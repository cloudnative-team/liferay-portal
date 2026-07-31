package licensing

import (
	licensingv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/licensing/v1alpha1"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const conditionTypeActivated = "Activated"

func newActivatedCondition() metav1.Condition {
	return newCondition(
		conditionTypeActivated,
		"",
		"Activated",
		metav1.ConditionTrue,
	)
}

func newActivationNotConfiguredCondition() metav1.Condition {
	return newCondition(
		conditionTypeActivated,
		"The activation code secret reference is not configured.",
		"ActivationCodeNotConfigured",
		metav1.ConditionFalse,
	)
}

func newActivationRejectedCondition(activationError error) metav1.Condition {
	return newCondition(
		conditionTypeActivated,
		activationError.Error(),
		"ActivationRejected",
		metav1.ConditionFalse,
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

func setStatusCondition(
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
	condition metav1.Condition,
) {
	meta.SetStatusCondition(&liferayEnvironment.Status.Conditions, condition)
}
