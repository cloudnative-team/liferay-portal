package persistentvolumeclaim

import (
	"context"
	"slices"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	StateAccessModesUnsupported State = "AccessModesUnsupported"
	StateBound                  State = "Bound"
	StateCreated                State = "Created"
	StateNotBound               State = "NotBound"
	StateStorageClassNotFound   State = "StorageClassNotFound"
)

func (manager *Manager) Ensure(
	context context.Context,
	owner client.Object,
	spec Spec,
) (Result, error) {
	storageClassFound, error := manager.storageClassExists(context, spec.StorageClassName)

	if error != nil {
		return Result{}, error
	}

	if !storageClassFound {
		return Result{State: StateStorageClassNotFound}, nil
	}

	persistentVolumeClaim, error := manager.resolvePersistentVolumeClaim(context, spec)

	if error != nil {
		return Result{}, error
	}

	if persistentVolumeClaim == nil {
		if error := manager.createPersistentVolumeClaim(
			context,
			owner,
			spec,
		); error != nil {
			return Result{}, error
		}

		return Result{
			Phase: corev1.ClaimPending,
			State: StateCreated,
		}, nil
	}

	return Result{
		Phase: persistentVolumeClaim.Status.Phase,
		State: resolveState(persistentVolumeClaim, spec),
	}, nil
}

func (manager *Manager) createPersistentVolumeClaim(
	context context.Context,
	owner client.Object,
	spec Spec,
) error {
	persistentVolumeClaim := getManifest(spec)

	if error := controllerruntime.SetControllerReference(
		owner,
		persistentVolumeClaim,
		manager.Scheme(),
	); error != nil {
		return error
	}

	if error := manager.Create(
		context,
		persistentVolumeClaim,
	); error != nil && !errors.IsAlreadyExists(error) {
		return error
	}

	return nil
}

func (manager *Manager) resolvePersistentVolumeClaim(
	context context.Context,
	spec Spec,
) (*corev1.PersistentVolumeClaim, error) {
	persistentVolumeClaim := &corev1.PersistentVolumeClaim{}

	namespacedName := types.NamespacedName{
		Name:      spec.Name,
		Namespace: spec.Namespace,
	}

	if error := manager.Get(
		context,
		namespacedName,
		persistentVolumeClaim,
	); error != nil {
		if errors.IsNotFound(error) {
			return nil, nil
		}

		return nil, error
	}

	return persistentVolumeClaim, nil
}

func (manager *Manager) storageClassExists(
	context context.Context,
	storageClassName string,
) (bool, error) {
	storageClass := &storagev1.StorageClass{}

	error := manager.Get(
		context,
		types.NamespacedName{Name: storageClassName},
		storageClass,
	)

	if errors.IsNotFound(error) {
		return false, nil
	}

	if error != nil {
		return false, error
	}

	return true, nil
}

func getManifest(spec Spec) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: spec.AccessModes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: spec.Size,
				},
			},
			StorageClassName: &spec.StorageClassName,
		},
	}
}

func resolveState(
	persistentVolumeClaim *corev1.PersistentVolumeClaim,
	spec Spec,
) State {
	if persistentVolumeClaim.Status.Phase != corev1.ClaimBound {
		return StateNotBound
	}

	for _, accessMode := range spec.AccessModes {
		if !slices.Contains(persistentVolumeClaim.Status.AccessModes, accessMode) {
			return StateAccessModesUnsupported
		}
	}

	return StateBound
}

type Manager struct {
	client.Client
}

type Result struct {
	Phase corev1.PersistentVolumeClaimPhase
	State State
}

type Spec struct {
	AccessModes      []corev1.PersistentVolumeAccessMode
	Name             string
	Namespace        string
	Size             resource.Quantity
	StorageClassName string
}

type State string
