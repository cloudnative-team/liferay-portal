package marketplace

import (
	licensingv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/licensing/v1alpha1"
	"github.com/liferay/liferay-portal/cloud/operator/internal/persistentvolumeclaim"
	corev1 "k8s.io/api/core/v1"
)

const claimNameSuffix = "-marketplace"

func newClaimSpec(
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
) persistentvolumeclaim.Spec {
	marketplaceVolumeSpec := liferayEnvironment.Spec.MarketplaceVolume

	return persistentvolumeclaim.Spec{
		AccessModes: []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteMany,
		},
		Name:             resolveClaimName(liferayEnvironment),
		Namespace:        liferayEnvironment.Namespace,
		Size:             marketplaceVolumeSpec.Size,
		StorageClassName: marketplaceVolumeSpec.StorageClassName,
	}
}

func resolveClaimName(
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
) string {
	if liferayEnvironment.Spec.MarketplaceVolume.ClaimName != "" {
		return liferayEnvironment.Spec.MarketplaceVolume.ClaimName
	}

	return liferayEnvironment.Spec.WorkloadRef.Name + claimNameSuffix
}
