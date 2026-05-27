// Package v1 contains API Schema definitions for the lxc.liferay.com v1 API group.
//
// +kubebuilder:object:generate=true
// +groupName=lxc.liferay.com
package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is the group/version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "lxc.liferay.com", Version: "v1"}

	// SchemeBuilder collects functions that add the types to a Scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(&LiferayExtension{}, &LiferayExtensionList{})
}
