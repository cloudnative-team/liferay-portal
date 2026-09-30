package cx

import (
	"slices"
	"testing"

	cxv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/cx/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDxpNamespaceDefaultsToClientExtensionNamespace(t *testing.T) {
	testCases := map[string]struct {
		dxpNamespace string
		want         string
	}{
		"an empty dxpNamespace is the client extension namespace": {
			want: "able",
		},
		"dxpNamespace is used when set": {
			dxpNamespace: "liferay-prod",
			want:         "liferay-prod",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			clientExtension := newClientExtension(testCase.dxpNamespace, "sample", "able")

			if got := DxpNamespace(clientExtension); got != testCase.want {
				t.Errorf("DxpNamespace() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestExtProvisionNameJoinsServiceIDAndVirtualInstance(t *testing.T) {
	clientExtension := newClientExtension("", "able", "able")

	if got, want := ExtProvisionName(clientExtension), "able-liferay.com-lxc-ext-provision-metadata"; got != want {
		t.Errorf("ExtProvisionName() = %q, want %q", got, want)
	}
}

func TestPermittedNamespacesReadsAllowedAnnotation(t *testing.T) {
	testCases := map[string]struct {
		annotations map[string]string
		want        []string
	}{
		"an empty annotation permits nothing": {
			annotations: map[string]string{
				cxv1alpha1.AnnotationAllowedClientExtensionNamespaces: "",
			},
		},
		"blank entries and spaces are ignored": {
			annotations: map[string]string{
				cxv1alpha1.AnnotationAllowedClientExtensionNamespaces: " able, ,baker ,",
			},
			want: []string{"able", "baker"},
		},
		"no annotation permits nothing": {},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Annotations: testCase.annotations, Name: "liferay-prod"},
			}

			if got := PermittedNamespaces(namespace); !slices.Equal(got, testCase.want) {
				t.Errorf("PermittedNamespaces() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestProjectNameDefaultsToServiceID(t *testing.T) {
	clientExtension := newClientExtension("", "able", "able")

	if got := ProjectName(clientExtension); got != "able" {
		t.Errorf("ProjectName() = %q, want %q", got, "able")
	}

	clientExtension.Spec.ProjectName = "able-project"

	if got := ProjectName(clientExtension); got != "able-project" {
		t.Errorf("ProjectName() = %q, want %q", got, "able-project")
	}
}
