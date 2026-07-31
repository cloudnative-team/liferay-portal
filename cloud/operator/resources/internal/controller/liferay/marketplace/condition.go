package marketplace

import (
	"fmt"

	licensingv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/licensing/v1alpha1"
	persistentvolumeclaim "github.com/liferay/liferay-portal/cloud/operator/internal/controller/persistentvolumeclaim"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	conditionTypeVolumeMounted = "MarketplaceVolumeMounted"
	conditionTypeVolumeReady   = "MarketplaceVolumeReady"
)

func claimReadyCondition(
	persistentVolumeClaimResult persistentvolumeclaim.Result,
	persistentVolumeClaimSpec persistentvolumeclaim.Spec,
) metav1.Condition {
	claimName := persistentVolumeClaimSpec.Name

	storageClassName := persistentVolumeClaimSpec.StorageClassName

	switch persistentVolumeClaimResult.State {
	case persistentvolumeclaim.StateAccessModesUnsupported:
		return newCondition(
			conditionTypeVolumeReady,
			fmt.Sprintf(
				"The persistent volume claim %q is bound but does not support ReadWriteMany. The storage class %q is not ReadWriteMany capable.",
				claimName, storageClassName),
			"ClaimNotReadWriteMany",
			metav1.ConditionFalse,
		)
	case persistentvolumeclaim.StateBound:
		return newCondition(
			conditionTypeVolumeReady,
			fmt.Sprintf(
				"The persistent volume claim %q is bound and supports ReadWriteMany.",
				claimName),
			"ClaimBound",
			metav1.ConditionTrue,
		)
	case persistentvolumeclaim.StateCreated:
		return newCondition(
			conditionTypeVolumeReady,
			fmt.Sprintf(
				"The persistent volume claim %q was created and is waiting to be bound.",
				claimName),
			"ClaimCreated",
			metav1.ConditionFalse,
		)
	case persistentvolumeclaim.StateNotBound:
		return newCondition(
			conditionTypeVolumeReady,
			fmt.Sprintf(
				"The persistent volume claim %q is not bound. Its phase is %q.",
				claimName, persistentVolumeClaimResult.Phase),
			"ClaimNotBound",
			metav1.ConditionFalse,
		)
	case persistentvolumeclaim.StateStorageClassNotFound:
		return newCondition(
			conditionTypeVolumeReady,
			fmt.Sprintf(
				"The storage class %q was not found. A ReadWriteMany capable storage class must exist before marketplace artifacts can be provisioned.",
				storageClassName),
			"StorageClassNotFound",
			metav1.ConditionFalse,
		)
	}

	return newCondition(
		conditionTypeVolumeReady,
		fmt.Sprintf(
			"The persistent volume claim %q is in the unknown state %q.",
			claimName, persistentVolumeClaimResult.State),
		"ClaimStateUnknown",
		metav1.ConditionUnknown,
	)
}

func mountCondition(
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
	statefulSet *appsv1.StatefulSet,
) metav1.Condition {
	claimName := resolveClaimName(liferayEnvironment)

	podSpec := &statefulSet.Spec.Template.Spec

	volume := persistentvolumeclaim.ResolveVolume(claimName, podSpec)

	if volume == nil {
		return newCondition(
			conditionTypeVolumeMounted,
			fmt.Sprintf(
				"The stateful set %q does not reference the persistent volume claim %q.",
				statefulSet.Name, claimName),
			"ClaimNotReferenced",
			metav1.ConditionFalse,
		)
	}

	if !persistentvolumeclaim.MountedReadOnly(podSpec, volume) {
		return newCondition(
			conditionTypeVolumeMounted,
			fmt.Sprintf(
				"The stateful set %q does not mount the volume %q read only.",
				statefulSet.Name, volume.Name),
			"ClaimNotReadOnly",
			metav1.ConditionFalse,
		)
	}

	return newCondition(
		conditionTypeVolumeMounted,
		fmt.Sprintf(
			"The stateful set %q mounts the volume %q read only.",
			statefulSet.Name, volume.Name),
		"ClaimMounted",
		metav1.ConditionTrue,
	)
}

func newCondition(
	conditionType string,
	message string,
	reason string,
	status metav1.ConditionStatus,
) metav1.Condition {
	return metav1.Condition{
		Message: message,
		Reason:  reason,
		Status:  status,
		Type:    conditionType,
	}
}
