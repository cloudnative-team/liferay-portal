package persistentvolumeclaim

import (
	licensingv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/licensing/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
)

func GetPersistentVolumeClaimSpec(
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
	claimNameSuffix string,
) Spec {
	marketplaceVolumeSpec := liferayEnvironment.Spec.MarketplaceVolume

	return Spec{
		AccessModes: []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteMany,
		},
		Name:             ResolveClaimName(liferayEnvironment, claimNameSuffix),
		Namespace:        liferayEnvironment.Namespace,
		Size:             marketplaceVolumeSpec.Size,
		StorageClassName: marketplaceVolumeSpec.StorageClassName,
	}
}

func ResolveClaimName(liferayEnvironment *licensingv1alpha1.LiferayEnvironment, claimNameSuffix string) string {
	if liferayEnvironment.Spec.MarketplaceVolume.ClaimName != "" {
		return liferayEnvironment.Spec.MarketplaceVolume.ClaimName
	}

	return liferayEnvironment.Spec.WorkloadRef.Name + claimNameSuffix
}

type Spec struct {
	AccessModes      []corev1.PersistentVolumeAccessMode
	Name             string
	Namespace        string
	Size             resource.Quantity
	StorageClassName string
}
