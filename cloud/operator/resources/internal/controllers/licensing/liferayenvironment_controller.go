// +kubebuilder:rbac:groups=licensing.liferay.com,resources=liferayenvironments,verbs=get;list;patch;update;watch
// +kubebuilder:rbac:groups=licensing.liferay.com,resources=liferayenvironments/finalizers,verbs=update
// +kubebuilder:rbac:groups=licensing.liferay.com,resources=liferayenvironments/status,verbs=get;patch;update
package licensing

import (
	"context"
	"crypto/rsa"
	"fmt"
	"time"

	licensingv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/licensing/v1alpha1"
	provisioning "github.com/liferay/liferay-portal/cloud/operator/internal/provisioning"
	corev1 "k8s.io/api/core/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	phaseDegraded = "Degraded"
	phasePending  = "Pending"
)

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create;get;list;patch;update;watch
func (liferayEnvironmentReconciler *LiferayEnvironmentReconciler) Reconcile(
	context context.Context,
	request controllerruntime.Request,
) (controllerruntime.Result, error) {
	liferayEnvironment := &licensingv1alpha1.LiferayEnvironment{}

	if error := liferayEnvironmentReconciler.Get(context, request.NamespacedName, liferayEnvironment); error != nil {
		return controllerruntime.Result{}, client.IgnoreNotFound(error)
	}

	environmentID, error := liferayEnvironmentReconciler.resolveEnvironmentID(context, liferayEnvironment.Namespace)

	if error != nil {
		return controllerruntime.Result{}, error
	}

	liferayEnvironment.Status.EnvironmentID = environmentID

	privateKey, error := liferayEnvironmentReconciler.getOrCreateIdentityKey(context, liferayEnvironment)

	if error != nil {
		return controllerruntime.Result{}, error
	}

	if liferayEnvironment.Status.ActivatedAt == nil {
		if error := liferayEnvironmentReconciler.activate(
			context,
			liferayEnvironment,
			privateKey,
		); error != nil {
			return controllerruntime.Result{}, error
		}
	}

	if liferayEnvironment.Status.Phase == "" {
		liferayEnvironment.Status.Phase = phasePending
	}

	return liferayEnvironmentReconciler.updateStatus(context, liferayEnvironment)
}

func (liferayEnvironmentReconciler *LiferayEnvironmentReconciler) SetupWithManager(
	manager controllerruntime.Manager,
) error {
	return controllerruntime.NewControllerManagedBy(
		manager,
	).For(
		&licensingv1alpha1.LiferayEnvironment{},
	).Named(
		"liferayenvironment",
	).Owns(
		&corev1.Secret{},
	).Complete(
		liferayEnvironmentReconciler,
	)
}

func (liferayEnvironmentReconciler *LiferayEnvironmentReconciler) activate(
	context context.Context,
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
	privateKey *rsa.PrivateKey,
) error {
	if liferayEnvironment.Spec.ActivationCodeSecretRef.Name == "" {
		setStatusCondition(liferayEnvironment, newActivationNotConfiguredCondition())

		liferayEnvironment.Status.Phase = phasePending

		return nil
	}

	activationRequest, error := liferayEnvironmentReconciler.newActivationRequest(
		context,
		liferayEnvironment,
		privateKey,
	)

	if error != nil {
		return error
	}

	if error := liferayEnvironmentReconciler.Provisioning.Activate(
		activationRequest,
		context,
		privateKey,
	); error != nil {
		setStatusCondition(liferayEnvironment, newActivationRejectedCondition(error))

		liferayEnvironment.Status.Phase = phaseDegraded

		return nil
	}

	activatedAt := metav1.Now()

	liferayEnvironment.Status.ActivatedAt = &activatedAt

	setStatusCondition(liferayEnvironment, newActivatedCondition())

	return nil
}

func (liferayEnvironmentReconciler *LiferayEnvironmentReconciler) newActivationRequest(
	context context.Context,
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
	privateKey *rsa.PrivateKey,
) (provisioning.ActivationRequest, error) {
	publicKey, error := encodePublicKeyBase64(privateKey)

	if error != nil {
		return provisioning.ActivationRequest{}, error
	}

	activationCode, error := liferayEnvironmentReconciler.readActivationCode(context, liferayEnvironment)

	if error != nil {
		return provisioning.ActivationRequest{}, error
	}

	return provisioning.ActivationRequest{
		ActivationCode:  activationCode,
		EnvironmentID:   liferayEnvironment.Status.EnvironmentID,
		EnvironmentName: liferayEnvironment.Spec.EnvironmentName,
		PublicKey:       publicKey,
	}, nil
}

func (liferayEnvironmentReconciler *LiferayEnvironmentReconciler) readActivationCode(
	context context.Context,
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
) (string, error) {
	reference := liferayEnvironment.Spec.ActivationCodeSecretRef

	key := types.NamespacedName{
		Name:      reference.Name,
		Namespace: liferayEnvironment.Namespace,
	}

	secret := &corev1.Secret{}

	if error := liferayEnvironmentReconciler.Get(context, key, secret); error != nil {
		return "", error
	}

	code, ok := secret.Data[reference.Key]

	if !ok {
		return "", fmt.Errorf(
			"activation code secret %q missing key %q",
			reference.Name, reference.Key)
	}

	return string(code), nil
}

func (liferayEnvironmentReconciler *LiferayEnvironmentReconciler) resolveEnvironmentID(
	context context.Context,
	namespaceName string,
) (string, error) {
	namespace := &corev1.Namespace{}

	if error := liferayEnvironmentReconciler.Get(context, types.NamespacedName{Name: namespaceName}, namespace); error != nil {
		return "", error
	}

	return string(namespace.UID), nil
}

func (liferayEnvironmentReconciler *LiferayEnvironmentReconciler) updateStatus(
	context context.Context,
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
) (controllerruntime.Result, error) {
	status := liferayEnvironmentReconciler.Status()

	if error := status.Update(context, liferayEnvironment); error != nil {
		if errors.IsConflict(error) {
			return controllerruntime.Result{RequeueAfter: time.Second}, nil
		}

		return controllerruntime.Result{}, error
	}

	return controllerruntime.Result{RequeueAfter: liferayEnvironmentReconciler.HeartbeatInterval}, nil
}

type LiferayEnvironmentReconciler struct {
	client.Client

	HeartbeatInterval time.Duration
	Provisioning      provisioning.Client
}
