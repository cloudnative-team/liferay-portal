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

func (persistentVolumeClaimManager *PersistentVolumeClaimManager) GetOrCreateClaim() (Result, error) {
	storageClassFound, error := persistentVolumeClaimManager.storageClassExists()

	if error != nil {
		return Result{}, error
	}

	if !storageClassFound {
		return Result{State: StateStorageClassNotFound}, nil
	}

	persistentVolumeClaim, error := persistentVolumeClaimManager.getClaim()

	if error != nil {
		return Result{}, error
	}

	if persistentVolumeClaim == nil {
		return persistentVolumeClaimManager.createClaim()
	}

	return Result{
		Phase: persistentVolumeClaim.Status.Phase,
		State: resolveClaimState(persistentVolumeClaim, persistentVolumeClaimManager.Spec),
	}, nil
}

func (persistentVolumeClaimManager *PersistentVolumeClaimManager) createClaim() (Result, error) {
	persistentVolumeClaim, error := persistentVolumeClaimManager.newOwnedClaimManifest()

	if error != nil {
		return Result{}, error
	}

	if error := persistentVolumeClaimManager.Create(
		persistentVolumeClaimManager.Context,
		persistentVolumeClaim,
	); error != nil && !errors.IsAlreadyExists(error) {
		return Result{}, error
	}

	return Result{
		Phase: corev1.ClaimPending,
		State: StateCreated,
	}, nil
}

func (persistentVolumeClaimManager *PersistentVolumeClaimManager) getClaim() (*corev1.PersistentVolumeClaim, error) {
	persistentVolumeClaim := &corev1.PersistentVolumeClaim{}

	namespacedName := types.NamespacedName{
		Name:      persistentVolumeClaimManager.Spec.Name,
		Namespace: persistentVolumeClaimManager.Spec.Namespace,
	}

	if error := persistentVolumeClaimManager.Get(
		persistentVolumeClaimManager.Context,
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

func newClaimManifest(spec Spec) *corev1.PersistentVolumeClaim {
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

func (persistentVolumeClaimManager *PersistentVolumeClaimManager) newOwnedClaimManifest() (*corev1.PersistentVolumeClaim, error) {
	persistentVolumeClaim := newClaimManifest(persistentVolumeClaimManager.Spec)

	if error := controllerruntime.SetControllerReference(
		persistentVolumeClaimManager.Owner,
		persistentVolumeClaim,
		persistentVolumeClaimManager.Scheme(),
	); error != nil {
		return nil, error
	}

	return persistentVolumeClaim, nil
}

func resolveClaimState(
	persistentVolumeClaim *corev1.PersistentVolumeClaim,
	spec Spec,
) State {
	if persistentVolumeClaim.Status.Phase != corev1.ClaimBound {
		return StateNotBound
	}

	if !supportsAccessModes(persistentVolumeClaim, spec.AccessModes) {
		return StateAccessModesUnsupported
	}

	return StateBound
}

func (persistentVolumeClaimManager *PersistentVolumeClaimManager) storageClassExists() (bool, error) {
	storageClass := &storagev1.StorageClass{}

	error := persistentVolumeClaimManager.Get(
		persistentVolumeClaimManager.Context,
		types.NamespacedName{Name: persistentVolumeClaimManager.Spec.StorageClassName},
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

func supportsAccessModes(
	persistentVolumeClaim *corev1.PersistentVolumeClaim,
	accessModes []corev1.PersistentVolumeAccessMode,
) bool {
	for _, accessMode := range accessModes {
		if !slices.Contains(persistentVolumeClaim.Status.AccessModes, accessMode) {
			return false
		}
	}

	return true
}

type PersistentVolumeClaimManager struct {
	client.Client

	Context context.Context
	Owner   client.Object
	Spec    Spec
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
