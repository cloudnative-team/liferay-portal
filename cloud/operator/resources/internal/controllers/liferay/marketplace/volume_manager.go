package marketplace

import (
	"context"

	licensingv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/licensing/v1alpha1"
	"github.com/liferay/liferay-portal/cloud/operator/internal/utils/persistentvolumeclaim"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

func (volumeManager *MarketplaceVolumeManager) Reconcile(
	context context.Context,
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
	statefulSet *appsv1.StatefulSet,
) ([]metav1.Condition, error) {
	readyCondition, error := volumeManager.reconcileClaim(
		liferayEnvironment,
	)

	if error != nil {
		return nil, error
	}

	return []metav1.Condition{
		readyCondition,
		mountCondition(liferayEnvironment, statefulSet),
	}, nil
}

func (volumeManager *MarketplaceVolumeManager) reconcileClaim(liferayEnvironment *licensingv1alpha1.LiferayEnvironment) (metav1.Condition, error) {
	persistentVolumeClaimSpec := persistentvolumeclaim.GetPersistentVolumeClaimSpec(liferayEnvironment, "-marketplace")

	liferayEnvironment.Status.MarketplaceVolume.ClaimName = persistentVolumeClaimSpec.Name

	persistentVolumeClaimResult, error := volumeManager.PersistentVolumeClaimManager.Ensure()

	if error != nil {
		return metav1.Condition{}, error
	}

	if persistentVolumeClaimResult.Phase != "" {
		liferayEnvironment.Status.MarketplaceVolume.Phase = string(persistentVolumeClaimResult.Phase)
	}

	return claimReadyCondition(persistentVolumeClaimResult, persistentVolumeClaimSpec), nil
}

type MarketplaceVolumeManager struct {
	client.Client

	*persistentvolumeclaim.PersistentVolumeClaimManager
}
