package cx

import (
	"context"
	"fmt"
	"slices"

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
	handler "sigs.k8s.io/controller-runtime/pkg/handler"
	predicate "sigs.k8s.io/controller-runtime/pkg/predicate"
	reconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ReasonLiferayNamespaceNotFound = "LiferayNamespaceNotFound"
	ReasonNamespaceNotPermitted    = "NamespaceNotPermitted"
	ReasonNamespacePermitted       = "NamespacePermitted"
)

// +kubebuilder:rbac:groups=cx.liferay.com,resources=clientextensions,verbs=get;list;watch
// +kubebuilder:rbac:groups=cx.liferay.com,resources=clientextensions/status,verbs=get;patch;update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
func (clientExtensionReconciler *ClientExtensionReconciler) Reconcile(
	context context.Context,
	request controllerruntime.Request,
) (controllerruntime.Result, error) {
	var clientExtension cxv1alpha1.ClientExtension

	if error := clientExtensionReconciler.Get(context, request.NamespacedName, &clientExtension); error != nil {
		return controllerruntime.Result{}, client.IgnoreNotFound(error)
	}

	liferayNamespace, reason, error := clientExtensionReconciler.resolveLiferayNamespace(
		&clientExtension, context,
	)

	if error != nil {
		return controllerruntime.Result{}, error
	}

	if reason != "" {
		return controllerruntime.Result{}, clientExtensionReconciler.updateStatus(
			&clientExtension, metav1.ConditionFalse, context,
			refusalMessage(&clientExtension, liferayNamespace, reason),
			cxv1alpha1.PhaseDegraded, reason,
		)
	}

	return controllerruntime.Result{}, clientExtensionReconciler.updateStatus(
		&clientExtension, metav1.ConditionUnknown, context,
		fmt.Sprintf(
			"Namespace %q accepts client extensions from namespace %q.",
			liferayNamespace, clientExtension.Namespace,
		),
		cxv1alpha1.PhasePending, ReasonNamespacePermitted,
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
		&corev1.Namespace{},
		handler.EnqueueRequestsFromMapFunc(clientExtensionReconciler.requestsForNamespace),
	).Complete(
		clientExtensionReconciler,
	)
}

func refusalMessage(
	clientExtension *cxv1alpha1.ClientExtension, liferayNamespace string, reason string,
) string {
	if reason == ReasonLiferayNamespaceNotFound {
		return fmt.Sprintf(
			"Namespace %q does not exist, so there is no Liferay to deliver to. Set spec.liferayNamespace to the namespace Liferay runs in.",
			liferayNamespace,
		)
	}

	return fmt.Sprintf(
		"Namespace %q does not list %q in its %q annotation, so this client extension is not delivered there. Whoever manages Liferay grants access by adding %q to that annotation.",
		liferayNamespace, clientExtension.Namespace,
		cxv1alpha1.AnnotationAllowedClientExtensionNamespaces, clientExtension.Namespace,
	)
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

		if LiferayNamespace(clientExtension) != object.GetName() {
			continue
		}

		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(clientExtension),
		})
	}

	return requests
}

func (clientExtensionReconciler *ClientExtensionReconciler) resolveLiferayNamespace(
	clientExtension *cxv1alpha1.ClientExtension,
	context context.Context,
) (string, string, error) {
	liferayNamespace := LiferayNamespace(clientExtension)

	if liferayNamespace == clientExtension.Namespace {
		return liferayNamespace, "", nil
	}

	var namespace corev1.Namespace

	error := clientExtensionReconciler.Get(
		context, types.NamespacedName{Name: liferayNamespace}, &namespace,
	)

	if apierrors.IsNotFound(error) {
		return liferayNamespace, ReasonLiferayNamespaceNotFound, nil
	}

	if error != nil {
		return "", "", error
	}

	if !slices.Contains(PermittedNamespaces(&namespace), clientExtension.Namespace) {
		return liferayNamespace, ReasonNamespaceNotPermitted, nil
	}

	return liferayNamespace, "", nil
}

func (clientExtensionReconciler *ClientExtensionReconciler) updateStatus(
	clientExtension *cxv1alpha1.ClientExtension,
	conditionStatus metav1.ConditionStatus,
	context context.Context,
	message string,
	phase string,
	reason string,
) error {
	status := clientExtension.Status.DeepCopy()

	for _, conditionType := range []string{cxv1alpha1.ConditionDelivered, cxv1alpha1.ConditionReady} {
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Message:            message,
			ObservedGeneration: clientExtension.Generation,
			Reason:             reason,
			Status:             conditionStatus,
			Type:               conditionType,
		})
	}

	status.ObservedGeneration = clientExtension.Generation
	status.Phase = phase

	if equality.Semantic.DeepEqual(status, &clientExtension.Status) {
		return nil
	}

	clientExtension.Status = *status

	if error := clientExtensionReconciler.Status().Update(context, clientExtension); error != nil {
		return client.IgnoreNotFound(error)
	}

	if (phase == cxv1alpha1.PhaseDegraded) && (clientExtensionReconciler.Recorder != nil) {
		clientExtensionReconciler.Recorder.Event(clientExtension, corev1.EventTypeWarning, reason, message)
	}

	return nil
}

type ClientExtensionReconciler struct {
	client.Client

	Recorder record.EventRecorder
}
