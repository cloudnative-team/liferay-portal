// +kubebuilder:rbac:groups=licensing.liferay.com,resources=liferayenvironments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=licensing.liferay.com,resources=liferayenvironments/finalizers,verbs=update
// +kubebuilder:rbac:groups=licensing.liferay.com,resources=liferayenvironments/status,verbs=get;update;patch

package licensing

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	licensingv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/licensing/v1alpha1"
)

type LiferayEnvironmentReconciler struct {
	client.Client

	HeartbeatInterval time.Duration
}

func (r *LiferayEnvironmentReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	lenv := &licensingv1alpha1.LiferayEnvironment{}

	if err := r.Get(ctx, req.NamespacedName, lenv); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if lenv.Status.Phase == "" {
		lenv.Status.Phase = "Pending"
	}

	meta.SetStatusCondition(
		&lenv.Status.Conditions,
		metav1.Condition{
			Message: "Reconcile is not implemented.",
			Reason:  "NotImplemented",
			Status:  metav1.ConditionFalse,
			Type:    "Ready",
		},
	)

	status := r.Status()

	if err := status.Update(ctx, lenv); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}

		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: r.HeartbeatInterval}, nil
}

func (r *LiferayEnvironmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(
		mgr,
	).For(
		&licensingv1alpha1.LiferayEnvironment{},
	).Named(
		"liferayenvironment",
	).Complete(
		r,
	)
}