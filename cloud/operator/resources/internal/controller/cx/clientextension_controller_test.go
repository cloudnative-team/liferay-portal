package cx

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	cxv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/cx/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	record "k8s.io/client-go/tools/record"
	controllerruntime "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	fake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	interceptor "sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestReconcileAcceptsAClientExtensionOnceConsentIsGranted(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "sample", "team-a")
	namespace := newLiferayNamespace("team-b")

	clientExtensionReconciler := newReconciler(nil, t, clientExtension, namespace)

	if phase, reason := reconcileClientExtension(clientExtension, clientExtensionReconciler, t); reason != ReasonNamespaceNotPermitted {
		t.Fatalf("Reconcile() = %s / %s, want %s / %s", phase, reason, cxv1alpha1.PhaseDegraded, ReasonNamespaceNotPermitted)
	}

	namespace.Annotations[cxv1alpha1.AnnotationAllowedClientExtensionNamespaces] = "team-a,team-b"

	if error := clientExtensionReconciler.Update(context.Background(), namespace); error != nil {
		t.Fatal(error)
	}

	phase, reason := reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	if (phase != cxv1alpha1.PhasePending) || (reason != ReasonNamespacePermitted) {
		t.Errorf("Reconcile() = %s / %s, want %s / %s", phase, reason, cxv1alpha1.PhasePending, ReasonNamespacePermitted)
	}
}

func TestReconcileRecordsRefusalOnce(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "sample", "team-a")
	recorder := record.NewFakeRecorder(10)

	clientExtensionReconciler := newReconciler(nil, t, clientExtension, newLiferayNamespace("team-b"))

	clientExtensionReconciler.Recorder = recorder

	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)
	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	if len(recorder.Events) != 1 {
		t.Fatalf("Expected one event for an unchanged refusal, got %d", len(recorder.Events))
	}

	if event := <-recorder.Events; !strings.HasPrefix(event, "Warning "+ReasonNamespaceNotPermitted) {
		t.Errorf("Expected a %s warning, got %q", ReasonNamespaceNotPermitted, event)
	}
}

func TestReconcileRequiresConsentAcrossNamespaces(t *testing.T) {
	testCases := map[string]struct {
		liferayNamespace string
		messages         []string
		objects          []client.Object
		wantPhase        string
		wantReason       string
		wantStatus       metav1.ConditionStatus
	}{
		"a listed namespace is permitted": {
			liferayNamespace: "liferay-dev",
			messages:         []string{`Namespace "liferay-dev" accepts client extensions from namespace "team-a".`},
			objects:          []client.Object{newLiferayNamespace(" team-a , team-b ")},
			wantPhase:        cxv1alpha1.PhasePending,
			wantReason:       ReasonNamespacePermitted,
			wantStatus:       metav1.ConditionUnknown,
		},
		"a missing Liferay namespace is refused": {
			liferayNamespace: "liferay-dev",
			messages:         []string{`Namespace "liferay-dev" does not exist`, "spec.liferayNamespace"},
			wantPhase:        cxv1alpha1.PhaseDegraded,
			wantReason:       ReasonLiferayNamespaceNotFound,
			wantStatus:       metav1.ConditionFalse,
		},
		"an unlisted namespace is refused": {
			liferayNamespace: "liferay-dev",
			messages: []string{
				`Namespace "liferay-dev" does not list "team-a"`,
				`"cx.liferay.com/allowed-client-extension-namespaces"`,
			},
			objects:    []client.Object{newLiferayNamespace("team-b")},
			wantPhase:  cxv1alpha1.PhaseDegraded,
			wantReason: ReasonNamespaceNotPermitted,
			wantStatus: metav1.ConditionFalse,
		},
		"liferay's own namespace needs no consent": {
			liferayNamespace: "team-a",
			wantPhase:        cxv1alpha1.PhasePending,
			wantReason:       ReasonNamespacePermitted,
			wantStatus:       metav1.ConditionUnknown,
		},
		"no liferayNamespace needs no consent": {
			wantPhase:  cxv1alpha1.PhasePending,
			wantReason: ReasonNamespacePermitted,
			wantStatus: metav1.ConditionUnknown,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			clientExtension := newClientExtension(testCase.liferayNamespace, "sample", "team-a")

			clientExtensionReconciler := newReconciler(nil, t, append(testCase.objects, clientExtension)...)

			reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

			var updatedClientExtension cxv1alpha1.ClientExtension

			if error := clientExtensionReconciler.Get(
				context.Background(), client.ObjectKeyFromObject(clientExtension), &updatedClientExtension,
			); error != nil {
				t.Fatal(error)
			}

			if updatedClientExtension.Status.Phase != testCase.wantPhase {
				t.Errorf("phase = %q, want %q", updatedClientExtension.Status.Phase, testCase.wantPhase)
			}

			for _, conditionType := range []string{cxv1alpha1.ConditionDelivered, cxv1alpha1.ConditionReady} {
				condition := meta.FindStatusCondition(updatedClientExtension.Status.Conditions, conditionType)

				if condition == nil {
					t.Fatalf("Expected a %s condition", conditionType)
				}

				if (condition.Reason != testCase.wantReason) || (condition.Status != testCase.wantStatus) {
					t.Errorf(
						"%s = %s / %s, want %s / %s",
						conditionType, condition.Status, condition.Reason, testCase.wantStatus, testCase.wantReason,
					)
				}

				for _, message := range testCase.messages {
					if !strings.Contains(condition.Message, message) {
						t.Errorf("Expected the %s message to contain %q, got %q", conditionType, message, condition.Message)
					}
				}
			}
		})
	}
}

func TestReconcileRetriesWhenTheLiferayNamespaceIsUnreadable(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "sample", "team-a")

	reconciler := newReconciler(
		&interceptor.Funcs{
			Get: func(
				context context.Context, client client.WithWatch, key client.ObjectKey,
				object client.Object, options ...client.GetOption,
			) error {
				if _, ok := object.(*corev1.Namespace); ok {
					return errors.New("namespaces is forbidden")
				}

				return client.Get(context, key, object, options...)
			},
		},
		t, clientExtension,
	)

	_, error := reconciler.Reconcile(
		context.Background(),
		controllerruntime.Request{NamespacedName: client.ObjectKeyFromObject(clientExtension)},
	)

	if error == nil {
		t.Fatal("Reconcile() error = nil, want the error so the request is retried")
	}

	var updated cxv1alpha1.ClientExtension

	if getError := reconciler.Get(
		context.Background(), client.ObjectKeyFromObject(clientExtension), &updated,
	); getError != nil {
		t.Fatal(getError)
	}

	if updated.Status.Phase != "" {
		t.Errorf("Expected the status to be left alone, got phase %q", updated.Status.Phase)
	}
}

func TestRequestsForNamespaceRequeuesClientExtensionsDeliveringToIt(t *testing.T) {
	reconciler := newReconciler(
		nil, t,
		newClientExtension("", "beside", "liferay-dev"),
		newClientExtension("liferay-dev", "elsewhere", "team-a"),
		newClientExtension("liferay-uat", "other-liferay", "team-a"),
		newClientExtension("team-b", "own-namespace", "team-b"),
		newClientExtension("liferay-dev", "second", "team-b"),
	)

	var names []string

	for _, request := range reconciler.requestsForNamespace(context.Background(), newLiferayNamespace("")) {
		names = append(names, request.Namespace+"/"+request.Name)
	}

	slices.Sort(names)

	if want := []string{"team-a/elsewhere", "team-b/second"}; !slices.Equal(names, want) {
		t.Errorf("requestsForNamespace() = %v, want %v", names, want)
	}
}

func newClientExtension(liferayNamespace string, name string, namespace string) *cxv1alpha1.ClientExtension {
	return &cxv1alpha1.ClientExtension{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: cxv1alpha1.ClientExtensionSpec{
			LiferayNamespace:  liferayNamespace,
			ServiceID:         name,
			VirtualInstanceID: "liferay.com",
		},
	}
}

func newLiferayNamespace(permittedNamespaces string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				cxv1alpha1.AnnotationAllowedClientExtensionNamespaces: permittedNamespaces,
			},
			Name: "liferay-dev",
		},
	}
}

func newReconciler(
	funcs *interceptor.Funcs,
	t *testing.T,
	objects ...client.Object,
) *ClientExtensionReconciler {
	t.Helper()

	scheme := runtime.NewScheme()

	if error := corev1.AddToScheme(scheme); error != nil {
		t.Fatal(error)
	}

	if error := cxv1alpha1.AddToScheme(scheme); error != nil {
		t.Fatal(error)
	}

	clientBuilder := fake.NewClientBuilder().WithObjects(
		objects...,
	).WithScheme(
		scheme,
	).WithStatusSubresource(
		&cxv1alpha1.ClientExtension{},
	)

	if funcs != nil {
		clientBuilder.WithInterceptorFuncs(*funcs)
	}

	return &ClientExtensionReconciler{Client: clientBuilder.Build()}
}

func reconcileClientExtension(
	clientExtension *cxv1alpha1.ClientExtension,
	clientExtensionReconciler *ClientExtensionReconciler,
	t *testing.T,
) (string, string) {
	t.Helper()

	if _, error := clientExtensionReconciler.Reconcile(
		context.Background(),
		controllerruntime.Request{NamespacedName: client.ObjectKeyFromObject(clientExtension)},
	); error != nil {
		t.Fatalf("Reconcile() error = %v, want nil", error)
	}

	var updated cxv1alpha1.ClientExtension

	if error := clientExtensionReconciler.Get(
		context.Background(), client.ObjectKeyFromObject(clientExtension), &updated,
	); error != nil {
		t.Fatal(error)
	}

	delivered := meta.FindStatusCondition(updated.Status.Conditions, cxv1alpha1.ConditionDelivered)

	if delivered == nil {
		t.Fatal("Expected a Delivered condition")
	}

	return updated.Status.Phase, delivered.Reason
}
