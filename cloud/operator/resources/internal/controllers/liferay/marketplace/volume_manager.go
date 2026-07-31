package marketplace

import (
	"context"

	licensingv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/licensing/v1alpha1"
	"github.com/liferay/liferay-portal/cloud/operator/internal/utils/persistentvolumeclaim"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

func (volumeManager *VolumeManager) CreateVolumeIfMissing(
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
) (metav1.Condition, error) {
	claimResult, error := volumeManager.GetOrCreateClaim()

	if error != nil {
		return metav1.Condition{}, error
	}

	volumeManager.setVolumeStatus(claimResult, liferayEnvironment)

	return newClaimReadyCondition(claimResult, volumeManager.Spec), nil
}

func (volumeManager *VolumeManager) GetMountCondition(
	statefulSet *appsv1.StatefulSet,
) metav1.Condition {
	return newMountCondition(volumeManager.Spec.Name, statefulSet)
}

func NewVolumeManager(
	context context.Context,
	kubernetesClient client.Client,
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
	statefulSet *appsv1.StatefulSet,
) *VolumeManager {
	return &VolumeManager{
		PersistentVolumeClaimManager: &persistentvolumeclaim.PersistentVolumeClaimManager{
			Client:  kubernetesClient,
			Context: context,
			Owner:   statefulSet,
			Spec:    newClaimSpec(liferayEnvironment),
		},
	}
}

func (volumeManager *VolumeManager) setVolumeStatus(
	claimResult persistentvolumeclaim.Result,
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
) {
	volumeStatus := &liferayEnvironment.Status.MarketplaceVolume

	volumeStatus.ClaimName = volumeManager.Spec.Name

	if claimResult.Phase != "" {
		volumeStatus.Phase = string(claimResult.Phase)
	}
}

type VolumeManager struct {
	*persistentvolumeclaim.PersistentVolumeClaimManager
}
