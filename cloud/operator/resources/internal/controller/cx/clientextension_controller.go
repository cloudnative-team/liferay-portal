package cx

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	cxv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/cx/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	equality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	record "k8s.io/client-go/tools/record"
	controllerruntime "sigs.k8s.io/controller-runtime"
	builder "sigs.k8s.io/controller-runtime/pkg/builder"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	controllerutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	handler "sigs.k8s.io/controller-runtime/pkg/handler"
	predicate "sigs.k8s.io/controller-runtime/pkg/predicate"
	reconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ReasonDelivered              = "Delivered"
	ReasonDxpNamespaceNotFound   = "DxpNamespaceNotFound"
	ReasonNamespaceNotPermitted  = "NamespaceNotPermitted"
	ReasonServiceIDConflict      = "ServiceIDConflict"
	ReasonStepNotEvaluated       = "StepNotEvaluated"
	ReasonStepsComplete          = "StepsComplete"
	ReasonUnknownVirtualInstance = "UnknownVirtualInstance"
)

const unknownVirtualInstanceRequeueInterval = time.Minute

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=create;get;list;patch;update;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=cx.liferay.com,resources=clientextensions,verbs=get;list;watch
// +kubebuilder:rbac:groups=cx.liferay.com,resources=clientextensions/status,verbs=get;patch;update
func (clientExtensionReconciler *ClientExtensionReconciler) Reconcile(
	context context.Context,
	request controllerruntime.Request,
) (controllerruntime.Result, error) {
	var clientExtension cxv1alpha1.ClientExtension

	if error := clientExtensionReconciler.Get(
		context, request.NamespacedName, &clientExtension,
	); error != nil {
		return controllerruntime.Result{}, client.IgnoreNotFound(error)
	}

	dxpNamespace, reason, error := clientExtensionReconciler.resolveDxpNamespace(
		&clientExtension, context,
	)

	if error != nil {
		return controllerruntime.Result{}, error
	}

	extProvisionConfigMapNames := clientExtension.Status.ExtProvisionConfigMapNames

	if reason != "" {
		return controllerruntime.Result{}, clientExtensionReconciler.updateStatus(
			&clientExtension, metav1.ConditionFalse, context, extProvisionConfigMapNames,
			refusalMessage(&clientExtension, dxpNamespace, reason), reason,
		)
	}

	var dxpMetadata corev1.ConfigMap

	error = clientExtensionReconciler.Get(
		context,
		types.NamespacedName{
			Name:      DxpMetadataName(clientExtension.Spec.VirtualInstanceID),
			Namespace: dxpNamespace,
		},
		&dxpMetadata,
	)

	if apierrors.IsNotFound(error) {
		message, error := clientExtensionReconciler.unknownVirtualInstanceMessage(
			&clientExtension, context, dxpNamespace,
		)

		if error != nil {
			return controllerruntime.Result{}, error
		}

		return controllerruntime.Result{RequeueAfter: unknownVirtualInstanceRequeueInterval},
			clientExtensionReconciler.updateStatus(
				&clientExtension, metav1.ConditionFalse, context, extProvisionConfigMapNames,
				message, ReasonUnknownVirtualInstance,
			)
	}

	if error != nil {
		return controllerruntime.Result{}, error
	}

	payload, error := json.MarshalIndent(clientExtension.Spec.Configs, "", "\t")

	if error != nil {
		return controllerruntime.Result{}, error
	}

	conflictingConfigMap, error := clientExtensionReconciler.applyExtProvision(
		&clientExtension, context, dxpNamespace, string(payload),
	)

	if error != nil {
		return controllerruntime.Result{}, error
	}

	if conflictingConfigMap != nil {
		return controllerruntime.Result{}, clientExtensionReconciler.updateStatus(
			&clientExtension, metav1.ConditionFalse, context, nil,
			serviceIDConflictMessage(&clientExtension, conflictingConfigMap),
			ReasonServiceIDConflict,
		)
	}

	extProvisionName := ExtProvisionName(&clientExtension)

	return controllerruntime.Result{}, clientExtensionReconciler.updateStatus(
		&clientExtension, metav1.ConditionTrue, context, []string{extProvisionName},
		fmt.Sprintf(
			"Delivered to ConfigMap %q in namespace %q.", extProvisionName, dxpNamespace,
		),
		ReasonDelivered,
	)
}

func (clientExtensionReconciler *ClientExtensionReconciler) SetupWithManager(
	manager controllerruntime.Manager,
) error {
	return controllerruntime.NewControllerManagedBy(
		manager,
	).For(
		&cxv1alpha1.ClientExtension{},
		builder.WithPredicates(predicate.GenerationChangedPredicate{}),
	).Named(
		"clientextension",
	).Watches(
		&corev1.ConfigMap{},
		handler.EnqueueRequestsFromMapFunc(clientExtensionReconciler.requestsForConfigMap),
	).Watches(
		&corev1.Namespace{},
		handler.EnqueueRequestsFromMapFunc(clientExtensionReconciler.requestsForNamespace),
	).Complete(
		clientExtensionReconciler,
	)
}

func (clientExtensionReconciler *ClientExtensionReconciler) applyExtProvision(
	clientExtension *cxv1alpha1.ClientExtension,
	context context.Context,
	dxpNamespace string,
	payload string,
) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ExtProvisionName(clientExtension),
			Namespace: dxpNamespace,
		},
	}

	getError := clientExtensionReconciler.Get(
		context, client.ObjectKeyFromObject(configMap), configMap,
	)

	if (getError == nil) && !ownsExtProvision(clientExtension, configMap) {
		return configMap, nil
	}

	if (getError != nil) && !apierrors.IsNotFound(getError) {
		return nil, getError
	}

	_, error := controllerutil.CreateOrUpdate(
		context, clientExtensionReconciler.Client, configMap,
		func() error {
			if configMap.Annotations == nil {
				configMap.Annotations = map[string]string{}
			}

			if clientExtension.Spec.Domain == "" {
				delete(configMap.Annotations, AnnotationDomains)
				delete(configMap.Annotations, AnnotationMainDomain)
			} else {
				configMap.Annotations[AnnotationDomains] = clientExtension.Spec.Domain
				configMap.Annotations[AnnotationMainDomain] = clientExtension.Spec.Domain
			}

			configMap.Data = map[string]string{
				clientExtension.Spec.ServiceID + ".client-extension-config.json": payload,
			}

			if configMap.Labels == nil {
				configMap.Labels = map[string]string{}
			}

			configMap.Labels[LabelMetadataType] = MetadataTypeExtProvision
			configMap.Labels[LabelOwnerName] = clientExtension.Name
			configMap.Labels[LabelOwnerNamespace] = clientExtension.Namespace
			configMap.Labels[LabelProjectName] = ProjectName(clientExtension)
			configMap.Labels[LabelServiceID] = clientExtension.Spec.ServiceID
			configMap.Labels[LabelVirtualInstance] = clientExtension.Spec.VirtualInstanceID

			return nil
		},
	)

	return nil, error
}

func notReadyCondition(
	conditions []metav1.Condition,
	conditionTypes []string,
) *metav1.Condition {
	var notReady *metav1.Condition

	for _, conditionType := range conditionTypes {
		condition := meta.FindStatusCondition(conditions, conditionType)

		if condition == nil {
			condition = &metav1.Condition{
				Message: fmt.Sprintf("The %s step has not been evaluated.", conditionType),
				Reason:  ReasonStepNotEvaluated,
				Status:  metav1.ConditionUnknown,
			}
		}

		if condition.Status == metav1.ConditionFalse {
			return condition
		}

		if (condition.Status != metav1.ConditionTrue) && (notReady == nil) {
			notReady = condition
		}
	}

	return notReady
}

func refusalMessage(
	clientExtension *cxv1alpha1.ClientExtension, dxpNamespace string, reason string,
) string {
	if reason == ReasonDxpNamespaceNotFound {
		return fmt.Sprintf(
			"Namespace %q does not exist, so there is no DXP to deliver to. Set spec.dxpNamespace to the namespace DXP runs in.",
			dxpNamespace,
		)
	}

	return fmt.Sprintf(
		"Namespace %q does not list %q in its %q annotation, so this client extension is not delivered there. Whoever manages DXP grants access by adding %q to that annotation.",
		dxpNamespace, clientExtension.Namespace,
		cxv1alpha1.AnnotationAllowedClientExtensionNamespaces, clientExtension.Namespace,
	)
}

func (clientExtensionReconciler *ClientExtensionReconciler) requestsForConfigMap(
	context context.Context,
	object client.Object,
) []reconcile.Request {
	labels := object.GetLabels()

	metadataType := labels[LabelMetadataType]

	if (metadataType != MetadataTypeDxp) && (metadataType != MetadataTypeExtProvision) {
		return nil
	}

	var clientExtensionList cxv1alpha1.ClientExtensionList

	if error := clientExtensionReconciler.List(context, &clientExtensionList); error != nil {
		controllerruntime.LoggerFrom(context).Error(
			error, "Unable to list client extensions", "configMap", client.ObjectKeyFromObject(object),
		)

		return nil
	}

	var requests []reconcile.Request

	for index := range clientExtensionList.Items {
		clientExtension := &clientExtensionList.Items[index]

		if DxpNamespace(clientExtension) != object.GetNamespace() {
			continue
		}

		if clientExtension.Spec.VirtualInstanceID != labels[LabelVirtualInstance] {
			continue
		}

		if (metadataType == MetadataTypeExtProvision) &&
			(clientExtension.Spec.ServiceID != labels[LabelServiceID]) {

			continue
		}

		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(clientExtension),
		})
	}

	return requests
}

func (clientExtensionReconciler *ClientExtensionReconciler) requestsForNamespace(
	context context.Context,
	object client.Object,
) []reconcile.Request {
	var clientExtensionList cxv1alpha1.ClientExtensionList

	if error := clientExtensionReconciler.List(context, &clientExtensionList); error != nil {
		controllerruntime.LoggerFrom(context).Error(
			error, "Unable to list client extensions", "namespace", object.GetName(),
		)

		return nil
	}

	var requests []reconcile.Request

	for index := range clientExtensionList.Items {
		clientExtension := &clientExtensionList.Items[index]

		if clientExtension.Namespace == object.GetName() {
			continue
		}

		if DxpNamespace(clientExtension) != object.GetName() {
			continue
		}

		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(clientExtension),
		})
	}

	return requests
}

func (clientExtensionReconciler *ClientExtensionReconciler) resolveDxpNamespace(
	clientExtension *cxv1alpha1.ClientExtension,
	context context.Context,
) (string, string, error) {
	dxpNamespace := DxpNamespace(clientExtension)

	if dxpNamespace == clientExtension.Namespace {
		return dxpNamespace, "", nil
	}

	var namespace corev1.Namespace

	getError := clientExtensionReconciler.Get(
		context, types.NamespacedName{Name: dxpNamespace}, &namespace,
	)

	if apierrors.IsNotFound(getError) {
		return dxpNamespace, ReasonDxpNamespaceNotFound, nil
	}

	if getError != nil {
		return "", "", getError
	}

	if !slices.Contains(PermittedNamespaces(&namespace), clientExtension.Namespace) {
		return dxpNamespace, ReasonNamespaceNotPermitted, nil
	}

	return dxpNamespace, "", nil
}

func serviceIDConflictMessage(clientExtension *cxv1alpha1.ClientExtension, configMap *corev1.ConfigMap) string {
	owner := "is not managed by any ClientExtension"

	if ownerName := configMap.Labels[LabelOwnerName]; ownerName != "" {
		owner = fmt.Sprintf(
			"already belongs to ClientExtension %q in namespace %q",
			ownerName, configMap.Labels[LabelOwnerNamespace],
		)
	}

	return fmt.Sprintf(
		"Unable to deliver to ConfigMap %q in namespace %q: it %s. DXP identifies a client extension by its serviceId %q for a virtual instance, so it can be deployed only once.",
		configMap.Name, configMap.Namespace, owner, clientExtension.Spec.ServiceID,
	)
}

func summarize(
	conditionTypes []string,
	generation int64,
	status *cxv1alpha1.ClientExtensionStatus,
) {
	ready := metav1.Condition{
		Message:            "Every step is complete.",
		ObservedGeneration: generation,
		Reason:             ReasonStepsComplete,
		Status:             metav1.ConditionTrue,
		Type:               cxv1alpha1.ConditionReady,
	}

	status.Phase = cxv1alpha1.PhaseReady

	if notReady := notReadyCondition(status.Conditions, conditionTypes); notReady != nil {
		ready.Message = notReady.Message
		ready.Reason = notReady.Reason
		ready.Status = notReady.Status

		status.Phase = cxv1alpha1.PhasePending

		if notReady.Status == metav1.ConditionFalse {
			status.Phase = cxv1alpha1.PhaseDegraded
		}
	}

	meta.SetStatusCondition(&status.Conditions, ready)
}

func (clientExtensionReconciler *ClientExtensionReconciler) unknownVirtualInstanceMessage(
	clientExtension *cxv1alpha1.ClientExtension,
	context context.Context,
	dxpNamespace string,
) (string, error) {
	var configMapList corev1.ConfigMapList

	if error := clientExtensionReconciler.List(
		context, &configMapList,
		client.InNamespace(dxpNamespace),
		client.MatchingLabels{LabelMetadataType: MetadataTypeDxp},
	); error != nil {
		return "", error
	}

	var virtualInstanceIDs []string

	for _, configMap := range configMapList.Items {
		if virtualInstanceID := configMap.Labels[LabelVirtualInstance]; virtualInstanceID != "" {
			virtualInstanceIDs = append(virtualInstanceIDs, fmt.Sprintf("%q", virtualInstanceID))
		}
	}

	slices.Sort(virtualInstanceIDs)

	published := fmt.Sprintf("There are currently no available DXP virtual instances in namespace %q.", dxpNamespace)

	if len(virtualInstanceIDs) > 0 {
		published = fmt.Sprintf(
			"The available DXP virtual instances are %s; check spec.virtualInstanceId.",
			strings.Join(virtualInstanceIDs, ", "),
		)
	}

	message := fmt.Sprintf(
		"Virtual instance %q is unknown to DXP: ConfigMap %q does not exist in namespace %q. %s The client extension is delivered as soon as the virtual instance appears.",
		clientExtension.Spec.VirtualInstanceID,
		DxpMetadataName(clientExtension.Spec.VirtualInstanceID), dxpNamespace, published,
	)

	return message, nil
}

func (clientExtensionReconciler *ClientExtensionReconciler) updateStatus(
	clientExtension *cxv1alpha1.ClientExtension,
	conditionStatus metav1.ConditionStatus,
	context context.Context,
	extProvisionConfigMapNames []string,
	message string,
	reason string,
) error {
	status := clientExtension.Status.DeepCopy()

	status.ExtProvisionConfigMapNames = extProvisionConfigMapNames

	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Message:            message,
		ObservedGeneration: clientExtension.Generation,
		Reason:             reason,
		Status:             conditionStatus,
		Type:               cxv1alpha1.ConditionDelivered,
	})

	summarize(stepConditionTypes, clientExtension.Generation, status)

	status.ObservedGeneration = clientExtension.Generation

	if equality.Semantic.DeepEqual(status, &clientExtension.Status) {
		return nil
	}

	clientExtension.Status = *status

	if error := clientExtensionReconciler.Status().Update(context, clientExtension); error != nil {
		return client.IgnoreNotFound(error)
	}

	if (status.Phase == cxv1alpha1.PhaseDegraded) && (clientExtensionReconciler.Recorder != nil) {
		clientExtensionReconciler.Recorder.Event(clientExtension, corev1.EventTypeWarning, reason, message)
	}

	return nil
}

type ClientExtensionReconciler struct {
	client.Client

	Recorder record.EventRecorder
}

var stepConditionTypes = []string{cxv1alpha1.ConditionDelivered}
