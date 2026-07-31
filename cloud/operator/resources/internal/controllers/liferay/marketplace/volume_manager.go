package marketplace

import (
	"context"

	licensingv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/licensing/v1alpha1"
	persistentvolumeclaim "github.com/liferay/liferay-portal/cloud/operator/internal/controllers/persistentvolumeclaim"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

func (volumeManager *VolumeManager) Reconcile(
	context context.Context,
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
	statefulSet *appsv1.StatefulSet,
) ([]metav1.Condition, error) {
	readyCondition, error := volumeManager.reconcileClaim(
		context,
		liferayEnvironment,
		statefulSet,
	)

	if error != nil {
		return nil, error
	}

	return []metav1.Condition{
		readyCondition,
		mountCondition(liferayEnvironment, statefulSet),
	}, nil
}

func (volumeManager *VolumeManager) reconcileClaim(
	context context.Context,
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
	statefulSet *appsv1.StatefulSet,
) (metav1.Condition, error) {
	persistentVolumeClaimSpec := getPersistentVolumeClaimSpec(liferayEnvironment)

	liferayEnvironment.Status.MarketplaceVolume.ClaimName = persistentVolumeClaimSpec.Name

	persistentVolumeClaimManager := &persistentvolumeclaim.Manager{
		Client: volumeManager.Client,
	}

	persistentVolumeClaimResult, error := persistentVolumeClaimManager.Ensure(
		context,
		statefulSet,
		persistentVolumeClaimSpec,
	)

	if error != nil {
		return metav1.Condition{}, error
	}

	if persistentVolumeClaimResult.Phase != "" {
		liferayEnvironment.Status.MarketplaceVolume.Phase = string(persistentVolumeClaimResult.Phase)
	}

	return claimReadyCondition(persistentVolumeClaimResult, persistentVolumeClaimSpec), nil
}

type VolumeManager struct {
	client.Client
}
