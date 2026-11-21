package schema

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	record "k8s.io/client-go/tools/record"
	controllerruntime "sigs.k8s.io/controller-runtime"
	builder "sigs.k8s.io/controller-runtime/pkg/builder"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	handler "sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	predicate "sigs.k8s.io/controller-runtime/pkg/predicate"
)

// The schema gate keeps one DXP version writing to the database at a time.
//
// A StatefulSet opts in with the annotation AnnotationGate=true and runs an
// init container that waits until the operator sets AnnotationDecision on
// its pod. The operator records the DXP image the database schema is at in
// AnnotationSchemaVersion on the StatefulSet. When the pod template moves to
// a different image, a Job reads Release_, and the operator checks in
// Liferay's release catalog that the new release does not have an older
// database schema than the one Release_ records. It then deletes every pod
// that still runs the old image, lets exactly one new pod (the holder) start
// with the database upgrade enabled, and holds the others until the holder is
// ready, logged a successful upgrade, and Release_ shows the new schema with
// no failed modules. A hotfix that keeps the image rolls as before.
//
// AnnotationSchemaVersion holds an image, or SchemaVersionPrefix and the
// Release_ schema of a database that the operator adopted at another
// version.
const (
	AnnotationDecision        = "dxp.liferay.com/schema-gate-decision"
	AnnotationGate            = "dxp.liferay.com/schema-gate"
	AnnotationGateImage       = "dxp.liferay.com/schema-gate-image"
	AnnotationGuard           = "dxp.liferay.com/schema-guard"
	AnnotationSchemaVersion   = "dxp.liferay.com/schema-version"
	AnnotationUpgradeCheck    = "dxp.liferay.com/schema-upgrade-check"
	AnnotationUpgradeHolder   = "dxp.liferay.com/schema-upgrade-holder"
	AnnotationUpgradeMessage  = "dxp.liferay.com/schema-upgrade-message"
	AnnotationUpgradeRestarts = "dxp.liferay.com/schema-upgrade-restarts"
	AnnotationUpgradeResult   = "dxp.liferay.com/schema-upgrade-result"
	AnnotationUpgradeTarget   = "dxp.liferay.com/schema-upgrade-target"
	DecisionStart             = "start"
	DecisionUpgrade           = "upgrade"
	GuardSkip                 = "skip"
	LabelComponent            = "component"
	LabelComponentLiferay     = "liferay"
	ResultFailed              = "failed"
	ResultRefused             = "refused"
	ResultSucceeded           = "succeeded"
	SchemaVersionPrefix       = "schema:"
	UpgradeCheckGogo          = "gogo"

	fieldOwner          = "liferay-dxp-operator"
	refusedPollInterval = 5 * time.Minute
	upgradePollInterval = 10 * time.Second
)

// +kubebuilder:rbac:groups="",resources=pods,verbs=delete;get;list;patch;watch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;update;watch
func (statefulSetReconciler *StatefulSetReconciler) Reconcile(
	context context.Context,
	request controllerruntime.Request,
) (controllerruntime.Result, error) {
	statefulSet := &appsv1.StatefulSet{}

	// The annotations are the state of the gate. A cached copy can lag
	// behind the operator's own last update and repeat a step.

	reader := statefulSetReconciler.APIReader

	if reader == nil {
		reader = statefulSetReconciler.Client
	}

	if error := reader.Get(context, request.NamespacedName, statefulSet); error != nil {
		return controllerruntime.Result{}, client.IgnoreNotFound(error)
	}

	if statefulSet.Annotations[AnnotationGate] != "true" {
		return controllerruntime.Result{}, nil
	}

	targetImage := getDXPImage(statefulSet.Name, &statefulSet.Spec.Template.Spec)

	pods, error := statefulSetReconciler.listPods(context, statefulSet)

	if error != nil {
		return controllerruntime.Result{}, error
	}

	schemaVersion := statefulSet.Annotations[AnnotationSchemaVersion]

	if schemaVersion == "" {
		schemaVersion = getReadyImage(pods, statefulSet.Name)

		if (schemaVersion == "") ||
			(statefulSet.Annotations[AnnotationInitState] != "") {

			return statefulSetReconciler.initialize(context, pods, statefulSet, targetImage)
		}

		logf.FromContext(context).Info(
			"Schema version was recorded from the running pods",
			"schemaVersion", schemaVersion, "statefulSet", statefulSet.Name,
		)

		if error := statefulSetReconciler.setAnnotations(
			context, statefulSet,
			map[string]string{AnnotationSchemaVersion: schemaVersion},
		); error != nil {
			return controllerruntime.Result{}, error
		}
	}

	if schemaVersion == targetImage {
		return statefulSetReconciler.startPods(context, pods, statefulSet, targetImage)
	}

	return statefulSetReconciler.upgrade(context, pods, schemaVersion, statefulSet, targetImage)
}

func (statefulSetReconciler *StatefulSetReconciler) SetupWithManager(
	manager controllerruntime.Manager,
) error {
	return controllerruntime.NewControllerManagedBy(
		manager,
	).For(
		&appsv1.StatefulSet{},
		builder.WithPredicates(
			predicate.Or(
				predicate.AnnotationChangedPredicate{},
				predicate.GenerationChangedPredicate{},
			),
		),
	).Named(
		"schemagate",
	).Owns(
		&batchv1.Job{},
	).Watches(
		&corev1.Pod{},
		handler.EnqueueRequestForOwner(
			manager.GetScheme(), manager.GetRESTMapper(), &appsv1.StatefulSet{},
			handler.OnlyControllerOwner(),
		),
	).Complete(
		statefulSetReconciler,
	)
}

// checkSchemaVersions compares the schema that Release_ records with the
// schema of the target release and returns why the upgrade is refused, or "".
func (statefulSetReconciler *StatefulSetReconciler) checkSchemaVersions(
	context context.Context,
	state databaseState,
	targetImage string,
) string {
	if message := getIncompleteMessage(state); message != "" {
		return "Not upgrading: " + message
	}

	targetDatabaseSchemaVersion, error := statefulSetReconciler.ReleaseCatalog.GetSchemaVersion(
		context, targetImage,
	)

	if error != nil {
		return fmt.Sprintf(
			"Unable to find the database schema version of %s: %s. Set %s=%s on the StatefulSet to upgrade without the check.",
			targetImage, error, AnnotationGuard, GuardSkip,
		)
	}

	comparison, error := compareVersions(targetDatabaseSchemaVersion, state.portal)

	if error != nil {
		return error.Error()
	}

	if comparison < 0 {
		return fmt.Sprintf(
			"%s has database schema %s, which is older than the schema %s in \"Release_\". DXP cannot move a database to an older schema; choose a release with schema %s or newer.",
			targetImage, targetDatabaseSchemaVersion, state.portal, state.portal,
		)
	}

	return ""
}

func getDXPImage(containerName string, podSpec *corev1.PodSpec) string {
	for _, container := range podSpec.Containers {
		if container.Name == containerName {
			return container.Image
		}
	}

	if len(podSpec.Containers) == 0 {
		return ""
	}

	return podSpec.Containers[0].Image
}

func getOrdinal(pod *corev1.Pod) int {
	index := strings.LastIndex(pod.Name, "-")

	ordinal, error := strconv.Atoi(pod.Name[index+1:])

	if error != nil {
		return -1
	}

	return ordinal
}

// getReadyImage returns the image of a ready pod. The operator was installed
// next to a running DXP, whose database is at that version.
func getReadyImage(pods []corev1.Pod, statefulSetName string) string {
	for _, pod := range pods {
		if isReady(&pod) {
			return getDXPImage(statefulSetName, &pod.Spec)
		}
	}

	return ""
}

func isReady(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}

	return false
}

func (statefulSetReconciler *StatefulSetReconciler) listPods(
	context context.Context,
	statefulSet *appsv1.StatefulSet,
) ([]corev1.Pod, error) {
	selector, error := metav1.LabelSelectorAsSelector(statefulSet.Spec.Selector)

	if error != nil {
		return nil, error
	}

	podList := &corev1.PodList{}

	if error := statefulSetReconciler.List(
		context, podList, client.InNamespace(statefulSet.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); error != nil {
		return nil, error
	}

	pods := []corev1.Pod{}

	for _, pod := range podList.Items {
		if metav1.IsControlledBy(&pod, statefulSet) {
			pods = append(pods, pod)
		}
	}

	slices.SortFunc(
		pods,
		func(pod1 corev1.Pod, pod2 corev1.Pod) int {
			return getOrdinal(&pod1) - getOrdinal(&pod2)
		},
	)

	return pods, nil
}

// setAnnotations records the events only after the update succeeds, so that
// a conflict, which the next reconcile retries, does not record them twice.
func (statefulSetReconciler *StatefulSetReconciler) setAnnotations(
	context context.Context,
	statefulSet *appsv1.StatefulSet,
	values map[string]string,
	events ...event,
) error {
	if statefulSet.Annotations == nil {
		statefulSet.Annotations = map[string]string{}
	}

	changed := false

	for key, value := range values {
		if statefulSet.Annotations[key] == value {
			continue
		}

		changed = true

		if value == "" {
			delete(statefulSet.Annotations, key)
		} else {
			statefulSet.Annotations[key] = value
		}
	}

	if !changed {
		return nil
	}

	if error := statefulSetReconciler.Update(
		context, statefulSet, client.FieldOwner(fieldOwner),
	); error != nil {
		return error
	}

	for _, event := range events {
		statefulSetReconciler.Recorder.Event(
			statefulSet, event.eventType, event.reason, event.message,
		)
	}

	return nil
}

func (statefulSetReconciler *StatefulSetReconciler) setDecision(
	context context.Context,
	decision string,
	pod *corev1.Pod,
	statefulSetName string,
) error {
	image := getDXPImage(statefulSetName, &pod.Spec)

	if (pod.Annotations[AnnotationDecision] == decision) &&
		(pod.Annotations[AnnotationGateImage] == image) {

		return nil
	}

	patch := client.MergeFrom(pod.DeepCopy())

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	pod.Annotations[AnnotationDecision] = decision

	// The gate clears the OSGi state on the pod's volume when it was written
	// by a different image.

	pod.Annotations[AnnotationGateImage] = image

	if error := statefulSetReconciler.Patch(
		context, pod, patch, client.FieldOwner(fieldOwner),
	); error != nil {
		return client.IgnoreNotFound(error)
	}

	logf.FromContext(context).Info(
		"Schema gate was opened", "decision", decision, "pod", pod.Name,
	)

	return nil
}

func (statefulSetReconciler *StatefulSetReconciler) startPods(
	context context.Context,
	pods []corev1.Pod,
	statefulSet *appsv1.StatefulSet,
	targetImage string,
) (controllerruntime.Result, error) {
	if error := statefulSetReconciler.setAnnotations(
		context, statefulSet,
		map[string]string{
			AnnotationUpgradeHolder: "",
			AnnotationUpgradeTarget: "",
		},
	); error != nil {
		return controllerruntime.Result{}, error
	}

	for _, pod := range pods {
		if (pod.DeletionTimestamp != nil) ||
			(getDXPImage(statefulSet.Name, &pod.Spec) != targetImage) {

			continue
		}

		if error := statefulSetReconciler.setDecision(
			context, DecisionStart, &pod, statefulSet.Name,
		); error != nil {
			return controllerruntime.Result{}, error
		}
	}

	return controllerruntime.Result{}, nil
}

func (statefulSetReconciler *StatefulSetReconciler) upgrade(
	context context.Context,
	pods []corev1.Pod,
	schemaVersion string,
	statefulSet *appsv1.StatefulSet,
	targetImage string,
) (controllerruntime.Result, error) {
	logger := logf.FromContext(context)

	if statefulSet.Annotations[AnnotationUpgradeTarget] != targetImage {

		// The pods on the previous version keep running until the check
		// passes. A pod that already runs the new image waits in the gate.

		message := ""

		if statefulSet.Annotations[AnnotationGuard] != GuardSkip {

			// Each pod template generation gets its own check, so a stale
			// reconcile reuses it.

			state, done, error := statefulSetReconciler.runDatabaseJob(
				int(statefulSet.Generation), context, DatabaseJobPrecheck,
				statefulSet,
			)

			if (error != nil) || !done {
				return controllerruntime.Result{RequeueAfter: upgradePollInterval}, error
			}

			message = statefulSetReconciler.checkSchemaVersions(
				context, state, targetImage,
			)
		}

		if message != "" {
			if statefulSet.Annotations[AnnotationUpgradeMessage] != message {
				logger.Info(
					"Database upgrade was refused", "message", message,
					"statefulSet", statefulSet.Name,
				)
			}

			if error := statefulSetReconciler.setAnnotations(
				context, statefulSet,
				map[string]string{
					AnnotationUpgradeMessage: message,
					AnnotationUpgradeResult:  ResultRefused,
				},
				event{corev1.EventTypeWarning, message, "UpgradeRefused"},
			); error != nil {
				return controllerruntime.Result{}, error
			}

			return controllerruntime.Result{RequeueAfter: refusedPollInterval}, nil
		}

		logger.Info(
			"Database upgrade was started", "from", schemaVersion,
			"statefulSet", statefulSet.Name, "to", targetImage,
		)

		if error := statefulSetReconciler.setAnnotations(
			context, statefulSet,
			map[string]string{
				AnnotationUpgradeHolder:   "",
				AnnotationUpgradeMessage:  "",
				AnnotationUpgradeRestarts: "",
				AnnotationUpgradeResult:   "",
				AnnotationUpgradeTarget:   targetImage,
			},
			event{
				corev1.EventTypeNormal,
				fmt.Sprintf(
					"Upgrading the database schema from %s to %s.",
					schemaVersion, targetImage,
				),
				"UpgradeStarted",
			},
		); error != nil {
			return controllerruntime.Result{}, error
		}
	}

	if statefulSet.Annotations[AnnotationUpgradeResult] == ResultFailed {

		// A failed upgrade needs a person: restore the database and move the
		// StatefulSet to an image again. The holder stays for diagnosis and
		// the other pods stay in the gate.

		return controllerruntime.Result{RequeueAfter: refusedPollInterval}, nil
	}

	holderName := statefulSet.Annotations[AnnotationUpgradeHolder]

	var holder *corev1.Pod

	holderPresent := false

	for index := range pods {
		pod := &pods[index]

		if pod.Name == holderName {
			holderPresent = true
		}

		if pod.DeletionTimestamp != nil {
			continue
		}

		if getDXPImage(statefulSet.Name, &pod.Spec) != targetImage {

			// Recreate: no pod keeps the old version on the database while
			// the holder upgrades it.

			logger.Info(
				"Pod on the previous version was deleted", "image",
				getDXPImage(statefulSet.Name, &pod.Spec), "pod", pod.Name,
			)

			if error := statefulSetReconciler.Delete(
				context, pod,
			); (error != nil) && !errors.IsNotFound(error) {
				return controllerruntime.Result{}, error
			}

			continue
		}

		if (pod.Name == holderName) || ((holder == nil) && (holderName == "")) {
			holder = pod
		}
	}

	if (holder == nil) && !holderPresent {

		// The holder is gone, for example after both pods were deleted. With
		// OrderedReady the StatefulSet creates a higher ordinal only after
		// the lower ones are ready, so waiting for the old holder would hold
		// the gate forever. A terminating holder is still waited for, so
		// that two pods never upgrade at the same time.

		holder = chooseHolder(pods, statefulSet.Name, targetImage)
	}

	if holder == nil {
		return controllerruntime.Result{RequeueAfter: upgradePollInterval}, nil
	}

	if holderName != holder.Name {
		logger.Info("Upgrade holder was chosen", "pod", holder.Name)

		if error := statefulSetReconciler.setAnnotations(
			context, statefulSet,
			map[string]string{AnnotationUpgradeHolder: holder.Name},
		); error != nil {
			return controllerruntime.Result{}, error
		}
	}

	if error := statefulSetReconciler.setDecision(
		context, DecisionUpgrade, holder, statefulSet.Name,
	); error != nil {
		return controllerruntime.Result{}, error
	}

	if !isReady(holder) {
		return statefulSetReconciler.watchUpgradeHolder(context, holder, statefulSet)
	}

	// DXP becomes ready even when its upgrade failed, so readiness alone does
	// not finish the upgrade.

	result, upgradeType, error := statefulSetReconciler.LogReader.GetUpgradeResult(
		statefulSet.Name, context, holder,
	)

	if error != nil {
		return controllerruntime.Result{}, error
	}

	if (result != "") && (result != "success") && (result != "warning") {
		message := fmt.Sprintf(
			"Pod %s logged a %s upgrade with result %s. Restore the database before you deploy again; the other pods wait in the gate.",
			holder.Name, upgradeType, result,
		)

		logger.Info(
			"Database upgrade failed", "message", message,
			"statefulSet", statefulSet.Name,
		)

		if error := statefulSetReconciler.setAnnotations(
			context, statefulSet,
			map[string]string{
				AnnotationUpgradeMessage: message,
				AnnotationUpgradeResult:  ResultFailed,
			},
			event{corev1.EventTypeWarning, message, "UpgradeFailed"},
		); error != nil {
			return controllerruntime.Result{}, error
		}

		return controllerruntime.Result{RequeueAfter: refusedPollInterval}, nil
	}

	state, done, error := statefulSetReconciler.runDatabaseJob(
		int(statefulSet.Generation), context, DatabaseJobPostcheck, statefulSet,
	)

	if (error != nil) || !done {
		return controllerruntime.Result{RequeueAfter: upgradePollInterval}, error
	}

	failure := ""

	if message := getIncompleteMessage(state); message != "" {
		failure = fmt.Sprintf("Pod %s is ready, but %s", holder.Name, message)
	} else if targetSchemaVersion, error := statefulSetReconciler.ReleaseCatalog.GetSchemaVersion(
		context, targetImage,
	); (error == nil) && (targetSchemaVersion != state.portal) {
		failure = fmt.Sprintf(
			"Pod %s is ready, but \"Release_\" has schema %s instead of %s of %s.",
			holder.Name, state.portal, targetSchemaVersion, targetImage,
		)
	} else if (statefulSet.Annotations[AnnotationUpgradeCheck] == UpgradeCheckGogo) &&
		(statefulSetReconciler.UpgradeChecker != nil) {

		pending, error := statefulSetReconciler.UpgradeChecker.GetPendingUpgrades(
			statefulSet.Name, context, holder,
		)

		if error != nil {
			return controllerruntime.Result{}, error
		}

		if pending != "" {
			failure = fmt.Sprintf(
				"Pod %s is ready, but \"upgrade:check\" lists pending upgrades: %s.",
				holder.Name, pending,
			)
		}
	}

	if failure != "" {
		failure += " The other pods wait in the gate."

		logger.Info(
			"Database upgrade failed", "message", failure,
			"statefulSet", statefulSet.Name,
		)

		if error := statefulSetReconciler.setAnnotations(
			context, statefulSet,
			map[string]string{
				AnnotationUpgradeMessage: failure,
				AnnotationUpgradeResult:  ResultFailed,
			},
			event{corev1.EventTypeWarning, failure, "UpgradeFailed"},
		); error != nil {
			return controllerruntime.Result{}, error
		}

		return controllerruntime.Result{RequeueAfter: refusedPollInterval}, nil
	}

	message := fmt.Sprintf(
		"Pod %s upgraded the database schema to %s (\"Release_\" schema %s).",
		holder.Name, targetImage, state.portal,
	)

	if result == "" {
		message = fmt.Sprintf(
			"Pod %s started %s without logging an upgrade.", holder.Name,
			targetImage,
		)
	} else if result == "warning" {
		message = fmt.Sprintf(
			"Pod %s upgraded the database schema to %s with warnings.",
			holder.Name, targetImage,
		)
	}

	logger.Info(
		"Database upgrade was completed", "message", message,
		"statefulSet", statefulSet.Name,
	)

	if error := statefulSetReconciler.setAnnotations(
		context, statefulSet,
		map[string]string{
			AnnotationSchemaVersion:  targetImage,
			AnnotationUpgradeHolder:  "",
			AnnotationUpgradeMessage: message,
			AnnotationUpgradeResult:  ResultSucceeded,
			AnnotationUpgradeTarget:  "",
		},
		event{corev1.EventTypeNormal, message, "UpgradeCompleted"},
	); error != nil {
		return controllerruntime.Result{}, error
	}

	if error := statefulSetReconciler.deleteDatabaseJobs(context, statefulSet); error != nil {
		return controllerruntime.Result{}, error
	}

	return statefulSetReconciler.startPods(context, pods, statefulSet, targetImage)
}

// watchUpgradeHolder reports each restart of a holder that is not ready yet,
// which otherwise goes unnoticed: a release that cannot start, for example
// because of its configuration, never logs an upgrade result. When the pod
// template changed since the holder was created, for example to fix that
// configuration, the StatefulSet does not replace a pod that is not ready, so
// a holder that restarted is deleted and comes back with the new template. A
// holder that has not restarted is never deleted, so a running upgrade is
// not interrupted.
func (statefulSetReconciler *StatefulSetReconciler) watchUpgradeHolder(
	context context.Context,
	holder *corev1.Pod,
	statefulSet *appsv1.StatefulSet,
) (controllerruntime.Result, error) {
	containerStatus := getContainerStatus(statefulSet.Name, holder)

	if (containerStatus == nil) || (containerStatus.RestartCount == 0) {
		return controllerruntime.Result{RequeueAfter: upgradePollInterval}, nil
	}

	restarts := strconv.Itoa(int(containerStatus.RestartCount))

	if statefulSet.Annotations[AnnotationUpgradeRestarts] != restarts {
		lastState := ""

		if terminated := containerStatus.LastTerminationState.Terminated; terminated != nil {
			lastState = fmt.Sprintf(
				" The last run ended with %s (exit code %d).", terminated.Reason,
				terminated.ExitCode,
			)
		}

		message := fmt.Sprintf(
			"Upgrade holder %s restarted %s times before it became ready.%s Check its log; the other pods wait in the gate.",
			holder.Name, restarts, lastState,
		)

		logf.FromContext(context).Info(
			"Upgrade holder restarted", "message", message,
			"statefulSet", statefulSet.Name,
		)

		if error := statefulSetReconciler.setAnnotations(
			context, statefulSet,
			map[string]string{
				AnnotationUpgradeMessage:  message,
				AnnotationUpgradeRestarts: restarts,
			},
			event{corev1.EventTypeWarning, message, "UpgradeHolderRestarted"},
		); error != nil {
			return controllerruntime.Result{}, error
		}
	}

	updateRevision := statefulSet.Status.UpdateRevision

	if (updateRevision != "") &&
		(holder.Labels[appsv1.ControllerRevisionHashLabelKey] != updateRevision) {

		logf.FromContext(context).Info(
			"Upgrade holder with an older pod template was deleted",
			"pod", holder.Name, "revision",
			holder.Labels[appsv1.ControllerRevisionHashLabelKey],
			"updateRevision", updateRevision,
		)

		if error := statefulSetReconciler.Delete(
			context, holder, client.Preconditions{UID: &holder.UID},
		); (error != nil) && !errors.IsNotFound(error) {
			return controllerruntime.Result{}, error
		}

		// The new pod counts its restarts from zero.

		if error := statefulSetReconciler.setAnnotations(
			context, statefulSet,
			map[string]string{AnnotationUpgradeRestarts: ""},
		); error != nil {
			return controllerruntime.Result{}, error
		}
	}

	return controllerruntime.Result{RequeueAfter: upgradePollInterval}, nil
}

type LogReader interface {
	GetUpgradeResult(
		container string, context context.Context, pod *corev1.Pod,
	) (result string, upgradeType string, error error)
}

type ReleaseCatalog interface {
	GetSchemaVersion(context context.Context, image string) (string, error)
}

type StatefulSetReconciler struct {
	client.Client

	APIReader           client.Reader
	DatabaseClientImage string
	LogReader           LogReader
	Recorder            record.EventRecorder
	ReleaseCatalog      ReleaseCatalog
	UpgradeChecker      UpgradeChecker
}

// UpgradeChecker lists the upgrades that DXP in a pod still has pending,
// as the Gogo shell command upgrade:check prints them.
type UpgradeChecker interface {
	GetPendingUpgrades(
		container string, context context.Context, pod *corev1.Pod,
	) (string, error)
}

type event struct {
	eventType string
	message   string
	reason    string
}
