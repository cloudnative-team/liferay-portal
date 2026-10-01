package cx

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	cxv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/cx/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	controllerutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	reconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (clientExtensionReconciler *ClientExtensionReconciler) applyMirror(
	clientExtension *cxv1alpha1.ClientExtension,
	context context.Context,
	controller bool,
	source *corev1.ConfigMap,
) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      source.Name,
			Namespace: clientExtension.Namespace,
		},
	}

	sourceReference := source.Namespace + "/" + source.Name

	_, error := controllerutil.CreateOrUpdate(
		context, clientExtensionReconciler.Client, configMap,
		func() error {
			if (configMap.ResourceVersion != "") && !isMirrorOf(configMap, sourceReference) {
				return mirrorConflictError(configMap, sourceReference)
			}

			if configMap.Annotations == nil {
				configMap.Annotations = map[string]string{}
			}

			configMap.Annotations[AnnotationSource] = sourceReference

			configMap.BinaryData = maps.Clone(source.BinaryData)
			configMap.Data = maps.Clone(source.Data)

			configMap.Labels = maps.Clone(source.Labels)

			if configMap.Labels == nil {
				configMap.Labels = map[string]string{}
			}

			configMap.Labels[LabelMirror] = "true"

			if controller {
				return controllerutil.SetControllerReference(
					clientExtension, configMap, clientExtensionReconciler.Scheme(),
				)
			}

			return controllerutil.SetOwnerReference(clientExtension, configMap, clientExtensionReconciler.Scheme())
		},
	)

	var alreadyOwnedError *controllerutil.AlreadyOwnedError

	if errors.As(error, &alreadyOwnedError) {
		return fmt.Errorf("configmap %q mirrors %q for another client extension", configMap.Name, sourceReference)
	}

	if apierrors.IsAlreadyExists(error) {
		var existingConfigMap corev1.ConfigMap

		if getError := clientExtensionReconciler.APIReader.Get(
			context, client.ObjectKeyFromObject(configMap), &existingConfigMap,
		); getError != nil {
			return getError
		}

		if !isMirrorOf(&existingConfigMap, sourceReference) {
			return mirrorConflictError(&existingConfigMap, sourceReference)
		}
	}

	return error
}

func (clientExtensionReconciler *ClientExtensionReconciler) deleteStaleMirrors(
	clientExtension *cxv1alpha1.ClientExtension,
	context context.Context,
	mirrorNames []string,
) error {
	var configMapList corev1.ConfigMapList

	if error := clientExtensionReconciler.List(
		context, &configMapList,
		client.InNamespace(clientExtension.Namespace), client.MatchingLabels{LabelMirror: "true"},
	); error != nil {
		return error
	}

	for index := range configMapList.Items {
		configMap := &configMapList.Items[index]

		if slices.Contains(mirrorNames, configMap.Name) || !ownsMirror(clientExtension, configMap) {
			continue
		}

		configMap.OwnerReferences = slices.DeleteFunc(
			configMap.OwnerReferences,
			func(ownerReference metav1.OwnerReference) bool {
				return clientExtension.UID == ownerReference.UID
			},
		)

		if len(configMap.OwnerReferences) > 0 {
			if error := clientExtensionReconciler.Update(context, configMap); error != nil {
				return error
			}

			continue
		}

		if error := clientExtensionReconciler.Delete(context, configMap); client.IgnoreNotFound(error) != nil {
			return error
		}
	}

	return nil
}

func isMirrorOf(configMap *corev1.ConfigMap, sourceReference string) bool {
	return (configMap.Labels[LabelMirror] == "true") && (configMap.Annotations[AnnotationSource] == sourceReference)
}

func mirrorConflictError(configMap *corev1.ConfigMap, sourceReference string) error {
	return fmt.Errorf("configmap %q exists and is not the operator's mirror of %q", configMap.Name, sourceReference)
}

func (clientExtensionReconciler *ClientExtensionReconciler) mirrorMetadata(
	clientExtension *cxv1alpha1.ClientExtension,
	context context.Context,
	dxpMetadata *corev1.ConfigMap,
	extInit *corev1.ConfigMap,
) error {
	if clientExtension.Namespace == dxpMetadata.Namespace {
		return clientExtensionReconciler.deleteStaleMirrors(clientExtension, context, nil)
	}

	if error := clientExtensionReconciler.applyMirror(clientExtension, context, false, dxpMetadata); error != nil {
		return error
	}

	mirrorNames := []string{dxpMetadata.Name}

	if extInit != nil {
		if error := clientExtensionReconciler.applyMirror(clientExtension, context, true, extInit); error != nil {
			return error
		}

		mirrorNames = append(mirrorNames, extInit.Name)
	}

	return clientExtensionReconciler.deleteStaleMirrors(clientExtension, context, mirrorNames)
}

func requestsForMirror(object client.Object) []reconcile.Request {
	var requests []reconcile.Request

	for _, ownerReference := range object.GetOwnerReferences() {
		if (cxv1alpha1.SchemeBuilder.GroupVersion.String() != ownerReference.APIVersion) ||
			(ownerReference.Kind != "ClientExtension") {

			continue
		}

		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKey{Name: ownerReference.Name, Namespace: object.GetNamespace()},
		})
	}

	return requests
}
