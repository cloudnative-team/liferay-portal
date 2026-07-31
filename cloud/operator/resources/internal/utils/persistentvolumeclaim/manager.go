package persistentvolumeclaim

import (
	"context"
	"slices"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"
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

func (pvcManager *PersistentVolumeClaimManager) Ensure() (Result, error) {
	storageClassFound, error := pvcManager.storageClassExists(pvcManager.Spec.StorageClassName)

	if error != nil {
		return Result{}, error
	}

	if !storageClassFound {
		return Result{State: StateStorageClassNotFound}, nil
	}

	persistentVolumeClaim, error := pvcManager.resolvePersistentVolumeClaim()

	if error != nil {
		return Result{}, error
	}

	if persistentVolumeClaim == nil {
		if error := pvcManager.createPersistentVolumeClaim(); error != nil {
			return Result{}, error
		}

		return Result{
			Phase: corev1.ClaimPending,
			State: StateCreated,
		}, nil
	}

	return Result{
		Phase: persistentVolumeClaim.Status.Phase,
		State: resolveState(persistentVolumeClaim, pvcManager.Spec),
	}, nil
}

func (pvcManager *PersistentVolumeClaimManager) createPersistentVolumeClaim() error {
	persistentVolumeClaim := getManifest(pvcManager.Spec)

	if error := controllerruntime.SetControllerReference(
		pvcManager.Owner,
		persistentVolumeClaim,
		pvcManager.Scheme(),
	); error != nil {
		return error
	}

	if error := pvcManager.Create(
		pvcManager.Context,
		persistentVolumeClaim,
	); error != nil && !errors.IsAlreadyExists(error) {
		return error
	}

	return nil
}

func (pvcManager *PersistentVolumeClaimManager) resolvePersistentVolumeClaim() (*corev1.PersistentVolumeClaim, error) {
	persistentVolumeClaim := &corev1.PersistentVolumeClaim{}

	namespacedName := types.NamespacedName{
		Name:      pvcManager.Spec.Name,
		Namespace: pvcManager.Spec.Namespace,
	}

	if error := pvcManager.Get(
		pvcManager.Context,
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

func (pvcManager *PersistentVolumeClaimManager) storageClassExists(storageClassName string) (bool, error) {
	storageClass := &storagev1.StorageClass{}

	error := pvcManager.Get(
		pvcManager.Context,
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

type PersistentVolumeClaimManager struct {
	client.Client

	Context context.Context
	Spec    Spec
	Owner   client.Object
}

type Result struct {
	Phase corev1.PersistentVolumeClaimPhase
	State State
}

type State string
