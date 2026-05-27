package controller

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	lxcv1 "github.com/liferay/liferay-portal/cloud/operator/api/v1"
)

// LiferayExtensionReconciler reconciles LiferayExtension resources.
//
// Phase 1 scope: log on every reconcile so we can verify the wiring end-to-end
// (CRD applied, CR created, controller observes it). Deployment logic lands in
// Phase 3 once the ConfigMap-join indexers from Phase 2 are in place.
type LiferayExtensionReconciler struct {
	client.Client
}

// Reconcile is the per-object reconciliation loop.
func (r *LiferayExtensionReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	cx := &lxcv1.LiferayExtension{}

	if err := r.Get(ctx, req.NamespacedName, cx); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log := logf.FromContext(ctx)
	log.Info(
		"LiferayExtension reconciled.",
		"name", cx.Name,
		"namespace", cx.Namespace,
		"image", cx.Spec.Image,
		"virtualInstanceId", cx.Spec.VirtualInstanceID,
		"serviceId", cx.Spec.ServiceID,
	)

	return ctrl.Result{}, nil
}

// SetupWithManager wires the controller into the manager.
func (r *LiferayExtensionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(
		mgr,
	).For(
		&lxcv1.LiferayExtension{},
	).Named(
		"LiferayExtensionController",
	).Complete(
		r,
	)
}
