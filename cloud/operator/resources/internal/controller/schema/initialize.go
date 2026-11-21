package schema

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	controllerruntime "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// A new database is initialized by the first DXP pod that starts on it. If
// that pod dies halfway, the database keeps a partial schema that DXP cannot
// detect or repair. The operator therefore initializes only a database that
// its probe found empty, lets exactly one pod do it, and when that pod fails
// before it is ready, empties the database again and retries.
const (
	AnnotationInitAttempts = "dxp.liferay.com/schema-init-attempts"
	AnnotationInitHolder   = "dxp.liferay.com/schema-init-holder"
	AnnotationInitState    = "dxp.liferay.com/schema-init"
	DecisionInitialize     = "initialize"
	InitStateFailed        = "failed"
	InitStateInitializing  = "initializing"
	InitStateResetting     = "resetting"

	maxInitAttempts = 3
)

func chooseHolder(
	pods []corev1.Pod,
	statefulSetName string,
	targetImage string,
) *corev1.Pod {
	for index := range pods {
		pod := &pods[index]

		if (pod.DeletionTimestamp == nil) &&
			(getDXPImage(statefulSetName, &pod.Spec) == targetImage) {

			return pod
		}
	}

	return nil
}

func (statefulSetReconciler *StatefulSetReconciler) deleteDatabaseJobs(
	context context.Context,
	statefulSet *appsv1.StatefulSet,
) error {
	return client.IgnoreNotFound(
		statefulSetReconciler.DeleteAllOf(
			context, &batchv1.Job{}, client.InNamespace(statefulSet.Namespace),
			client.MatchingLabels{labelStatefulSet: statefulSet.Name},
			client.PropagationPolicy(metav1.DeletePropagationBackground),
		),
	)
}

func (statefulSetReconciler *StatefulSetReconciler) finishReset(
	context context.Context,
	statefulSet *appsv1.StatefulSet,
) (controllerruntime.Result, error) {
	attempts, _ := strconv.Atoi(statefulSet.Annotations[AnnotationInitAttempts])

	state, done, error := statefulSetReconciler.runDatabaseJob(
		attempts, context, DatabaseJobReset, statefulSet,
	)

	if (error != nil) || !done {
		return controllerruntime.Result{RequeueAfter: upgradePollInterval}, error
	}

	if tables := state.tables; tables > 0 {
		return statefulSetReconciler.markInitFailed(
			context, fmt.Sprintf(
				"The reset left %d tables that the DXP user does not own. Empty the database by hand and remove %s.",
				tables, AnnotationInitState,
			),
			statefulSet,
		)
	}

	// Without a state the next reconcile probes the database again and
	// starts another attempt.

	return controllerruntime.Result{Requeue: true}, statefulSetReconciler.setAnnotations(
		context, statefulSet,
		map[string]string{
			AnnotationInitHolder: "",
			AnnotationInitState:  "",
		},
		event{
			corev1.EventTypeNormal,
			"The partially initialized database was emptied.", "DatabaseReset",
		},
	)
}

// getAdoptedSchemaVersion returns the image to record for a database that
// already has a schema. When Release_ is at a different schema than the
// target image, it records the schema itself, so that the upgrade and its
// guard start from what the database is at.
func (statefulSetReconciler *StatefulSetReconciler) getAdoptedSchemaVersion(
	context context.Context,
	portalSchemaVersion string,
	targetImage string,
) string {
	targetSchemaVersion, error := statefulSetReconciler.ReleaseCatalog.GetSchemaVersion(
		context, targetImage,
	)

	if (error != nil) || (targetSchemaVersion == portalSchemaVersion) {
		return targetImage
	}

	return SchemaVersionPrefix + portalSchemaVersion
}

func getContainerStatus(
	containerName string,
	pod *corev1.Pod,
) *corev1.ContainerStatus {
	for index := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[index].Name == containerName {
			return &pod.Status.ContainerStatuses[index]
		}
	}

	return nil
}

// getIncompleteMessage returns why Release_ does not describe a complete
// schema, or "" when it does.
func getIncompleteMessage(state databaseState) string {
	if state.portal == "" {
		return fmt.Sprintf(
			"the database has %d tables and no portal row in \"Release_\", so its schema is incomplete. Empty the database or restore a backup, then remove %s.",
			state.tables, AnnotationInitState,
		)
	}

	if state.failed > 0 {
		return fmt.Sprintf(
			"\"Release_\" has %d modules in a failed state (%s). Restore a backup or fix them, then remove %s.",
			state.failed, state.failedModules, AnnotationInitState,
		)
	}

	return ""
}

func (statefulSetReconciler *StatefulSetReconciler) getJobMessage(
	context context.Context,
	job *batchv1.Job,
	succeeded bool,
) (string, error) {
	podList := &corev1.PodList{}

	if error := statefulSetReconciler.APIReader.List(
		context, podList, client.InNamespace(job.Namespace),
		client.MatchingLabels{"batch.kubernetes.io/controller-uid": string(job.UID)},
	); error != nil {
		return "", error
	}

	for _, pod := range podList.Items {
		if containerStatus := getContainerStatus("psql", &pod); (containerStatus != nil) &&
			(containerStatus.State.Terminated != nil) &&
			((containerStatus.State.Terminated.ExitCode == 0) == succeeded) {

			return containerStatus.State.Terminated.Message, nil
		}
	}

	return "", fmt.Errorf("database job %s has no finished pod", job.Name)
}

// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=create;delete;deletecollection;get;list;watch
func (statefulSetReconciler *StatefulSetReconciler) initialize(
	context context.Context,
	pods []corev1.Pod,
	statefulSet *appsv1.StatefulSet,
	targetImage string,
) (controllerruntime.Result, error) {
	switch statefulSet.Annotations[AnnotationInitState] {
	case InitStateFailed:
		return controllerruntime.Result{}, nil
	case InitStateInitializing:
		return statefulSetReconciler.watchInitHolder(context, pods, statefulSet, targetImage)
	case InitStateResetting:
		return statefulSetReconciler.finishReset(context, statefulSet)
	}

	attempts, _ := strconv.Atoi(statefulSet.Annotations[AnnotationInitAttempts])

	// The probe of each attempt has its own Job, so a reconcile that reads
	// an older state finds the probe it already ran instead of a new one.

	state, done, error := statefulSetReconciler.runDatabaseJob(
		attempts+1, context, DatabaseJobProbe, statefulSet,
	)

	if (error != nil) || !done {
		return controllerruntime.Result{RequeueAfter: upgradePollInterval}, error
	}

	logger := logf.FromContext(context)

	if state.tables > 0 {

		// A database with tables is adopted only when DXP recorded a
		// complete schema in Release_. Tables without a portal row are
		// what a first start leaves behind when it dies halfway, and
		// the operator does not empty a database it did not start on.

		if message := getIncompleteMessage(state); message != "" {
			return statefulSetReconciler.markInitFailed(context, message, statefulSet)
		}

		schemaVersion := statefulSetReconciler.getAdoptedSchemaVersion(
			context, state.portal, targetImage,
		)

		message := fmt.Sprintf(
			"The database has %d tables and \"Release_\" schema %s, so %s is recorded as its schema version.",
			state.tables, state.portal, schemaVersion,
		)

		logger.Info("Existing database was adopted", "message", message)

		if error := statefulSetReconciler.setAnnotations(
			context, statefulSet,
			map[string]string{
				AnnotationInitAttempts:  "",
				AnnotationSchemaVersion: schemaVersion,
			},
			event{corev1.EventTypeNormal, message, "DatabaseAdopted"},
		); error != nil {
			return controllerruntime.Result{}, error
		}

		if error := statefulSetReconciler.deleteDatabaseJobs(context, statefulSet); error != nil {
			return controllerruntime.Result{}, error
		}

		if schemaVersion != targetImage {

			// The next reconcile upgrades the database from the schema in
			// Release_ or refuses to.

			return controllerruntime.Result{Requeue: true}, nil
		}

		return statefulSetReconciler.startPods(context, pods, statefulSet, targetImage)
	}

	holder := chooseHolder(pods, statefulSet.Name, targetImage)

	if holder == nil {
		return controllerruntime.Result{RequeueAfter: upgradePollInterval}, nil
	}

	attempts++

	logger.Info(
		"Database initialization was started", "attempt", attempts,
		"pod", holder.Name,
	)

	if error := statefulSetReconciler.setAnnotations(
		context, statefulSet,
		map[string]string{
			AnnotationInitAttempts: strconv.Itoa(attempts),
			AnnotationInitHolder:   holder.Name + "/" + string(holder.UID),
			AnnotationInitState:    InitStateInitializing,
		},
		event{
			corev1.EventTypeNormal,
			fmt.Sprintf(
				"The database is empty. Pod %s initializes it with %s (attempt %d of %d).",
				holder.Name, targetImage, attempts, maxInitAttempts,
			),
			"InitializationStarted",
		},
	); error != nil {
		return controllerruntime.Result{}, error
	}

	return controllerruntime.Result{RequeueAfter: upgradePollInterval}, statefulSetReconciler.setDecision(
		context, DecisionInitialize, holder, statefulSet.Name,
	)
}

func (statefulSetReconciler *StatefulSetReconciler) markInitFailed(
	context context.Context,
	message string,
	statefulSet *appsv1.StatefulSet,
) (controllerruntime.Result, error) {
	logf.FromContext(context).Info("Database initialization failed", "message", message)

	return controllerruntime.Result{}, statefulSetReconciler.setAnnotations(
		context, statefulSet,
		map[string]string{
			AnnotationInitState:      InitStateFailed,
			AnnotationUpgradeMessage: message,
		},
		event{corev1.EventTypeWarning, message, "InitializationFailed"},
	)
}

// retryDatabaseJob deletes a failed Job so that the next reconcile of the
// same attempt creates it again. A database Job fails when it cannot reach
// the database, for example while DXP holds every connection, which passes;
// the event tells what psql reported.
func (statefulSetReconciler *StatefulSetReconciler) retryDatabaseJob(
	conditionMessage string,
	context context.Context,
	job *batchv1.Job,
	statefulSet *appsv1.StatefulSet,
) error {
	message, error := statefulSetReconciler.getJobMessage(context, job, false)

	if (error != nil) || (message == "") {
		message = conditionMessage
	}

	if error := statefulSetReconciler.Delete(
		context, job,
		client.Preconditions{UID: &job.UID},
		client.PropagationPolicy(metav1.DeletePropagationBackground),
	); (error != nil) && !errors.IsNotFound(error) {
		return error
	}

	message = strings.TrimSuffix(strings.TrimSpace(message), ".")

	logf.FromContext(context).Info(
		"Database job failed and runs again", "job", job.Name,
		"message", message,
	)

	statefulSetReconciler.Recorder.Event(
		statefulSet, corev1.EventTypeWarning, "DatabaseJobFailed",
		fmt.Sprintf(
			"Database job %s failed and runs again. It reported: %s.",
			job.Name, message,
		),
	)

	return nil
}

// runDatabaseJob creates the Job of an attempt when it does not exist and
// returns the database state it reported once it succeeded. Finished Jobs
// stay until the initialization ends, so running this again for the same
// attempt only reads the result.
func (statefulSetReconciler *StatefulSetReconciler) runDatabaseJob(
	attempt int,
	context context.Context,
	kind string,
	statefulSet *appsv1.StatefulSet,
) (databaseState, bool, error) {
	job := newDatabaseJob(
		attempt, statefulSetReconciler.DatabaseClientImage, kind, statefulSet,
	)

	existingJob := &batchv1.Job{}

	error := statefulSetReconciler.APIReader.Get(
		context, client.ObjectKeyFromObject(job), existingJob,
	)

	if errors.IsNotFound(error) {
		if error := controllerruntime.SetControllerReference(
			statefulSet, job, statefulSetReconciler.Scheme(),
		); error != nil {
			return databaseState{}, false, error
		}

		logf.FromContext(context).Info("Database job was created", "job", job.Name)

		return databaseState{}, false, statefulSetReconciler.Create(context, job)
	}

	if error != nil {
		return databaseState{}, false, error
	}

	for _, condition := range existingJob.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}

		if condition.Type == batchv1.JobFailed {
			return databaseState{}, false, statefulSetReconciler.retryDatabaseJob(
				condition.Message, context, existingJob, statefulSet,
			)
		}

		if condition.Type != batchv1.JobComplete {
			continue
		}

		message, error := statefulSetReconciler.getJobMessage(context, existingJob, true)

		if error != nil {
			return databaseState{}, false, error
		}

		state, error := parseDatabaseState(message)

		if error != nil {
			return databaseState{}, false, fmt.Errorf("database job %s: %w", existingJob.Name, error)
		}

		return state, true, nil
	}

	return databaseState{}, false, nil
}

func (statefulSetReconciler *StatefulSetReconciler) watchInitHolder(
	context context.Context,
	pods []corev1.Pod,
	statefulSet *appsv1.StatefulSet,
	targetImage string,
) (controllerruntime.Result, error) {
	holderName, holderUID, _ := strings.Cut(
		statefulSet.Annotations[AnnotationInitHolder], "/",
	)

	var holder *corev1.Pod

	for index := range pods {
		if pods[index].Name == holderName {
			holder = &pods[index]
		}
	}

	failure := ""

	if (holder == nil) || (string(holder.UID) != holderUID) {
		failure = fmt.Sprintf("Pod %s was removed before it was ready.", holderName)
	} else if holder.DeletionTimestamp != nil {
		failure = fmt.Sprintf("Pod %s is being deleted before it was ready.", holderName)
	} else if containerStatus := getContainerStatus(statefulSet.Name, holder); (containerStatus != nil) &&
		((containerStatus.RestartCount > 0) || (containerStatus.State.Terminated != nil)) {

		failure = fmt.Sprintf("DXP in pod %s stopped before it was ready.", holderName)
	}

	if failure == "" {
		if !isReady(holder) {
			return controllerruntime.Result{RequeueAfter: upgradePollInterval}, nil
		}

		attempts, _ := strconv.Atoi(statefulSet.Annotations[AnnotationInitAttempts])

		state, done, error := statefulSetReconciler.runDatabaseJob(
			attempts, context, DatabaseJobVerify, statefulSet,
		)

		if (error != nil) || !done {
			return controllerruntime.Result{RequeueAfter: upgradePollInterval}, error
		}

		if message := getIncompleteMessage(state); message != "" {
			return statefulSetReconciler.markInitFailed(
				context, fmt.Sprintf("Pod %s is ready, but %s", holderName, message),
				statefulSet,
			)
		}

		message := fmt.Sprintf(
			"Pod %s initialized the database with %s (\"Release_\" schema %s).",
			holderName, targetImage, state.portal,
		)

		logf.FromContext(context).Info("Database initialization was completed", "message", message)

		if error := statefulSetReconciler.setAnnotations(
			context, statefulSet,
			map[string]string{
				AnnotationInitAttempts:   "",
				AnnotationInitHolder:     "",
				AnnotationInitState:      "",
				AnnotationSchemaVersion:  targetImage,
				AnnotationUpgradeMessage: message,
			},
			event{corev1.EventTypeNormal, message, "InitializationCompleted"},
		); error != nil {
			return controllerruntime.Result{}, error
		}

		if error := statefulSetReconciler.deleteDatabaseJobs(context, statefulSet); error != nil {
			return controllerruntime.Result{}, error
		}

		return statefulSetReconciler.startPods(context, pods, statefulSet, targetImage)
	}

	attempts, _ := strconv.Atoi(statefulSet.Annotations[AnnotationInitAttempts])

	if attempts >= maxInitAttempts {
		return statefulSetReconciler.markInitFailed(
			context, fmt.Sprintf(
				"%s The database stays partially initialized after %d attempts.",
				failure, attempts,
			),
			statefulSet,
		)
	}

	logf.FromContext(context).Info(
		"Database initialization was interrupted", "message", failure,
	)

	// The pod's replacement runs the gate again and waits there while the
	// database is emptied.

	if (holder != nil) && (holder.DeletionTimestamp == nil) {
		if error := statefulSetReconciler.Delete(context, holder); client.IgnoreNotFound(error) != nil {
			return controllerruntime.Result{}, error
		}
	}

	if error := statefulSetReconciler.setAnnotations(
		context, statefulSet,
		map[string]string{AnnotationInitState: InitStateResetting},
		event{
			corev1.EventTypeWarning,
			failure + " The database is emptied before the next attempt.",
			"InitializationInterrupted",
		},
	); error != nil {
		return controllerruntime.Result{}, error
	}

	return controllerruntime.Result{RequeueAfter: time.Second}, nil
}
