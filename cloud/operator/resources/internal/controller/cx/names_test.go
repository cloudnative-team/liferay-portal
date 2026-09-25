package cx

import (
	"slices"
	"testing"

	cxv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/cx/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLiferayNamespaceDefaultsToTheClientExtensionNamespace(t *testing.T) {
	testCases := map[string]struct {
		liferayNamespace string
		want             string
	}{
		"an empty liferayNamespace is the client extension namespace": {
			want: "team-a",
		},
		"liferayNamespace is used when set": {
			liferayNamespace: "liferay-prod",
			want:             "liferay-prod",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			clientExtension := newClientExtension(testCase.liferayNamespace, "sample", "team-a")

			if got := LiferayNamespace(clientExtension); got != testCase.want {
				t.Errorf("LiferayNamespace() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestPermittedNamespacesReadsTheConsentAnnotation(t *testing.T) {
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
				cxv1alpha1.AnnotationAllowedClientExtensionNamespaces: " team-a, ,team-b ,",
			},
			want: []string{"team-a", "team-b"},
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
