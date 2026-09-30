package cx

import (
	"fmt"
	"strings"

	cxv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/cx/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

const (
	AnnotationDomains        = "ext.lxc.liferay.com/domains"
	AnnotationMainDomain     = "ext.lxc.liferay.com/mainDomain"
	AnnotationOwnerName      = "cx.liferay.com/owner-name"
	AnnotationOwnerNamespace = "cx.liferay.com/owner-namespace"
)

const (
	LabelMetadataType    = "lxc.liferay.com/metadataType"
	LabelProjectName     = "ext.lxc.liferay.com/projectName"
	LabelServiceID       = "ext.lxc.liferay.com/serviceId"
	LabelVirtualInstance = "dxp.lxc.liferay.com/virtualInstanceId"
)

const (
	MetadataTypeDxp          = "dxp"
	MetadataTypeExtProvision = "ext-provision"
)

func DxpMetadataName(virtualInstanceID string) string {
	return virtualInstanceID + "-lxc-dxp-metadata"
}

func DxpNamespace(clientExtension *cxv1alpha1.ClientExtension) string {
	if clientExtension.Spec.DxpNamespace != "" {
		return clientExtension.Spec.DxpNamespace
	}

	return clientExtension.Namespace
}

func ExtProvisionName(clientExtension *cxv1alpha1.ClientExtension) string {
	return fmt.Sprintf(
		"%s-%s-lxc-ext-provision-metadata",
		clientExtension.Spec.ServiceID, clientExtension.Spec.VirtualInstanceID,
	)
}

func PermittedNamespaces(namespace *corev1.Namespace) []string {
	var permittedNamespaces []string

	for _, name := range strings.Split(
		namespace.Annotations[cxv1alpha1.AnnotationAllowedClientExtensionNamespaces], ",",
	) {
		if name = strings.TrimSpace(name); name != "" {
			permittedNamespaces = append(permittedNamespaces, name)
		}
	}

	return permittedNamespaces
}

func ProjectName(clientExtension *cxv1alpha1.ClientExtension) string {
	if clientExtension.Spec.ProjectName != "" {
		return clientExtension.Spec.ProjectName
	}

	return clientExtension.Spec.ServiceID
}

func ownsExtProvision(clientExtension *cxv1alpha1.ClientExtension, configMap *corev1.ConfigMap) bool {
	return (configMap.Annotations[AnnotationOwnerName] == clientExtension.Name) &&
		(configMap.Annotations[AnnotationOwnerNamespace] == clientExtension.Namespace)
}
