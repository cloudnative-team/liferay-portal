package cx

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	cxv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/cx/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	types "k8s.io/apimachinery/pkg/types"
	record "k8s.io/client-go/tools/record"
	controllerruntime "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	fake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	interceptor "sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestReconcileAcceptsClientExtensionOnceConsentIsGranted(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "sample", "able")
	dxpNamespace := newDxpNamespace("baker")

	clientExtensionReconciler := newReconciler(
		nil, t, clientExtension, dxpNamespace, newDxpMetadata("liferay-dev", "liferay.com"),
	)

	if phase, reason := reconcileClientExtension(
		clientExtension, clientExtensionReconciler, t,
	); reason != ReasonNamespaceNotPermitted {
		t.Fatalf("Reconcile() = %s / %s, want %s / %s", phase, reason, cxv1alpha1.PhaseDegraded, ReasonNamespaceNotPermitted)
	}

	dxpNamespace.Annotations[cxv1alpha1.AnnotationAllowedClientExtensionNamespaces] = "able,baker"

	if error := clientExtensionReconciler.Update(context.Background(), dxpNamespace); error != nil {
		t.Fatal(error)
	}

	phase, reason := reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	if (phase != cxv1alpha1.PhaseReady) || (reason != ReasonDelivered) {
		t.Errorf("Reconcile() = %s / %s, want %s / %s", phase, reason, cxv1alpha1.PhaseReady, ReasonDelivered)
	}
}

func TestReconcileChecksConsentBeforeVirtualInstance(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "able", "able")

	clientExtensionReconciler := newReconciler(
		nil, t, clientExtension, newDxpMetadata("liferay-dev", "other.test"), newDxpNamespace("baker"),
	)

	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	delivered := getDelivered(clientExtension, clientExtensionReconciler, t)

	if delivered.Reason != ReasonNamespaceNotPermitted {
		t.Errorf("Delivered reason = %q, want %q", delivered.Reason, ReasonNamespaceNotPermitted)
	}

	if strings.Contains(delivered.Message, "other.test") {
		t.Errorf("Expected the refusal not to list virtual instances, got %q", delivered.Message)
	}
}

func TestReconcileDeliversOnceVirtualInstanceAppears(t *testing.T) {
	clientExtension := newClientExtension("", "able", "able")

	clientExtensionReconciler := newReconciler(nil, t, clientExtension)

	if _, reason := reconcileClientExtension(clientExtension, clientExtensionReconciler, t); reason != ReasonUnknownVirtualInstance {
		t.Fatalf("Delivered reason = %q, want %q", reason, ReasonUnknownVirtualInstance)
	}

	if error := clientExtensionReconciler.Create(
		context.Background(), newDxpMetadata("able", "liferay.com"),
	); error != nil {
		t.Fatal(error)
	}

	if phase, reason := reconcileClientExtension(clientExtension, clientExtensionReconciler, t); reason != ReasonDelivered {
		t.Errorf("Reconcile() = %s / %s, want %s / %s", phase, reason, cxv1alpha1.PhaseReady, ReasonDelivered)
	}

	if getExtProvision(clientExtensionReconciler, "able", t) == nil {
		t.Error("Expected the ext-provision ConfigMap once the virtual instance appears")
	}
}

func TestReconcileDeliversExtProvisionConfigMap(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "able", "able")

	clientExtension.Spec.Domain = "able.example.com"
	clientExtension.Spec.ProjectName = "able-project"

	clientExtensionReconciler := newReconciler(
		nil, t, clientExtension, newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace("able"),
	)

	if phase, reason := reconcileClientExtension(clientExtension, clientExtensionReconciler, t); reason != ReasonDelivered {
		t.Fatalf("Reconcile() = %s / %s, want %s / %s", phase, reason, cxv1alpha1.PhaseReady, ReasonDelivered)
	}

	configMap := getExtProvision(clientExtensionReconciler, "liferay-dev", t)

	if configMap == nil {
		t.Fatal("Expected the ext-provision ConfigMap to be written")
	}

	wantLabels := map[string]string{
		LabelMetadataType:    MetadataTypeExtProvision,
		LabelOwnerName:       "able",
		LabelOwnerNamespace:  "able",
		LabelProjectName:     "able-project",
		LabelServiceID:       "able",
		LabelVirtualInstance: "liferay.com",
	}

	if !maps.Equal(configMap.Labels, wantLabels) {
		t.Errorf("labels = %v, want %v", configMap.Labels, wantLabels)
	}

	wantAnnotations := map[string]string{
		AnnotationDomains:    "able.example.com",
		AnnotationMainDomain: "able.example.com",
	}

	if !maps.Equal(configMap.Annotations, wantAnnotations) {
		t.Errorf("annotations = %v, want %v", configMap.Annotations, wantAnnotations)
	}

	var payload map[string]map[string]any

	if error := json.Unmarshal([]byte(configMap.Data["able.client-extension-config.json"]), &payload); error != nil {
		t.Fatalf("Expected a JSON payload under able.client-extension-config.json, got %v: %v", configMap.Data, error)
	}

	wantPayload := map[string]map[string]any{
		"com.liferay.client.extension.type.configuration.CETConfiguration~able": {"name": "Sample"},
	}

	if !reflect.DeepEqual(payload, wantPayload) {
		t.Errorf("payload = %v, want %v", payload, wantPayload)
	}

	updatedClientExtension := getClientExtension(clientExtension, clientExtensionReconciler, t)

	if names := updatedClientExtension.Status.ExtProvisionConfigMapNames; (len(names) != 1) || (names[0] != configMap.Name) {
		t.Errorf("extProvisionConfigMapNames = %v, want [%s]", names, configMap.Name)
	}
}

func TestReconcileExplainsWhenNoVirtualInstanceIsPublished(t *testing.T) {
	clientExtension := newClientExtension("", "able", "able")

	clientExtensionReconciler := newReconciler(nil, t, clientExtension)

	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	message := getDelivered(clientExtension, clientExtensionReconciler, t).Message

	if !strings.Contains(message, `There are currently no available DXP virtual instances in namespace "able".`) {
		t.Errorf("Expected the message to say no virtual instance is available, got %q", message)
	}
}

func TestReconcileKeepsDeliveredConfigMapWhenTheVirtualInstanceDisappears(t *testing.T) {
	clientExtension := newClientExtension("", "able", "able")

	dxpMetadata := newDxpMetadata("able", "liferay.com")

	clientExtensionReconciler := newReconciler(nil, t, clientExtension, dxpMetadata)

	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	before := getExtProvision(clientExtensionReconciler, "able", t)

	if error := clientExtensionReconciler.Delete(context.Background(), dxpMetadata); error != nil {
		t.Fatal(error)
	}

	if _, reason := reconcileClientExtension(clientExtension, clientExtensionReconciler, t); reason != ReasonUnknownVirtualInstance {
		t.Errorf("Delivered reason = %q, want %q", reason, ReasonUnknownVirtualInstance)
	}

	after := getExtProvision(clientExtensionReconciler, "able", t)

	if (after == nil) || (after.UID != before.UID) || (after.ResourceVersion != before.ResourceVersion) {
		t.Errorf("Expected the ext-provision ConfigMap to be untouched, got %v", after)
	}

	updatedClientExtension := getClientExtension(clientExtension, clientExtensionReconciler, t)

	if !slices.Equal(updatedClientExtension.Status.ExtProvisionConfigMapNames, []string{before.Name}) {
		t.Errorf("extProvisionConfigMapNames = %v, want [%s]", updatedClientExtension.Status.ExtProvisionConfigMapNames, before.Name)
	}
}

func TestReconcileLeavesUnchangedExtProvisionConfigMapAlone(t *testing.T) {
	clientExtension := newClientExtension("", "able", "able")

	clientExtensionReconciler := newReconciler(nil, t, clientExtension, newDxpMetadata("able", "liferay.com"))

	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	before := getExtProvision(clientExtensionReconciler, "able", t)

	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	if after := getExtProvision(clientExtensionReconciler, "able", t); after.ResourceVersion != before.ResourceVersion {
		t.Errorf("resourceVersion = %s, want %s so the agent does not reapply it", after.ResourceVersion, before.ResourceVersion)
	}
}

func TestReconcileRecordsRefusalOnce(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "sample", "able")
	recorder := record.NewFakeRecorder(10)

	clientExtensionReconciler := newReconciler(nil, t, clientExtension, newDxpNamespace("baker"))

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

func TestReconcileRefusesExtProvisionConfigMapItDoesNotOwn(t *testing.T) {
	testCases := map[string]struct {
		labels      map[string]string
		wantMessage string
	}{
		"a ConfigMap another ClientExtension owns": {
			labels: map[string]string{
				LabelMetadataType:   MetadataTypeExtProvision,
				LabelOwnerName:      "able",
				LabelOwnerNamespace: "baker",
			},
			wantMessage: `already belongs to ClientExtension "able" in namespace "baker"`,
		},
		"a ConfigMap no ClientExtension owns": {
			wantMessage: "is not managed by any ClientExtension",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			clientExtension := newClientExtension("liferay-dev", "able", "able")

			clientExtension.Status.ExtProvisionConfigMapNames = []string{"able-liferay.com-lxc-ext-provision-metadata"}

			owned := newExtProvision(testCase.labels)

			clientExtensionReconciler := newReconciler(
				nil, t, clientExtension, owned,
				newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace("able,baker"),
			)

			phase, reason := reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

			if (phase != cxv1alpha1.PhaseDegraded) || (reason != ReasonServiceIDConflict) {
				t.Fatalf("Reconcile() = %s / %s, want %s / %s", phase, reason, cxv1alpha1.PhaseDegraded, ReasonServiceIDConflict)
			}

			updatedClientExtension := getClientExtension(clientExtension, clientExtensionReconciler, t)

			delivered := meta.FindStatusCondition(updatedClientExtension.Status.Conditions, cxv1alpha1.ConditionDelivered)

			for _, message := range []string{testCase.wantMessage, `serviceId "able"`} {
				if !strings.Contains(delivered.Message, message) {
					t.Errorf("Expected the message to contain %q, got %q", message, delivered.Message)
				}
			}

			configMap := getExtProvision(clientExtensionReconciler, "liferay-dev", t)

			if (configMap.Data["owner"] != "untouched") || !maps.Equal(configMap.Labels, owned.Labels) {
				t.Errorf("Expected the other owner's ConfigMap to be untouched, got %v / %v", configMap.Labels, configMap.Data)
			}

			if names := updatedClientExtension.Status.ExtProvisionConfigMapNames; len(names) != 0 {
				t.Errorf("extProvisionConfigMapNames = %v, want none", names)
			}
		})
	}
}

func TestReconcileRefusesUnknownVirtualInstance(t *testing.T) {
	clientExtension := newClientExtension("", "able", "able")

	ableMetadata := newDxpMetadata("able", "able.test")

	ableMetadata.Name = "zulu-lxc-dxp-metadata"

	clientExtensionReconciler := newReconciler(
		nil, t, ableMetadata, clientExtension, newDxpMetadata("able", "baker.test"),
	)

	result, error := clientExtensionReconciler.Reconcile(
		context.Background(),
		controllerruntime.Request{NamespacedName: client.ObjectKeyFromObject(clientExtension)},
	)

	if error != nil {
		t.Fatalf("Reconcile() error = %v, want nil", error)
	}

	if result.RequeueAfter <= 0 {
		t.Errorf("Reconcile() RequeueAfter = %v, want a retry", result.RequeueAfter)
	}

	updatedClientExtension := getClientExtension(clientExtension, clientExtensionReconciler, t)

	if updatedClientExtension.Status.Phase != cxv1alpha1.PhaseDegraded {
		t.Errorf("phase = %q, want %q", updatedClientExtension.Status.Phase, cxv1alpha1.PhaseDegraded)
	}

	delivered := getDelivered(clientExtension, clientExtensionReconciler, t)

	if (delivered.Status != metav1.ConditionFalse) || (delivered.Reason != ReasonUnknownVirtualInstance) {
		t.Fatalf("Delivered = %s / %s, want False / %s", delivered.Status, delivered.Reason, ReasonUnknownVirtualInstance)
	}

	for _, message := range []string{
		`Virtual instance "liferay.com" is unknown to DXP`,
		`ConfigMap "liferay.com-lxc-dxp-metadata" does not exist in namespace "able"`,
		`are "able.test", "baker.test"; check spec.virtualInstanceId.`,
		"delivered as soon as the virtual instance appears",
	} {
		if !strings.Contains(delivered.Message, message) {
			t.Errorf("Expected the message to contain %q, got %q", message, delivered.Message)
		}
	}

	if getExtProvision(clientExtensionReconciler, "able", t) != nil {
		t.Error("Expected nothing to be written for an unknown virtual instance")
	}
}

func TestReconcileRequiresConsentAcrossNamespaces(t *testing.T) {
	testCases := map[string]struct {
		dxpNamespace string
		messages     []string
		objects      []client.Object
		wantPhase    string
		wantReason   string
		wantStatus   metav1.ConditionStatus
	}{
		"a listed namespace is permitted": {
			dxpNamespace: "liferay-dev",
			messages:     []string{`in namespace "liferay-dev"`},
			objects: []client.Object{
				newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace(" able , baker "),
			},
			wantPhase:  cxv1alpha1.PhaseReady,
			wantReason: ReasonDelivered,
			wantStatus: metav1.ConditionTrue,
		},
		"a missing DXP namespace is refused": {
			dxpNamespace: "liferay-dev",
			messages:     []string{`Namespace "liferay-dev" does not exist`, "spec.dxpNamespace"},
			wantPhase:    cxv1alpha1.PhaseDegraded,
			wantReason:   ReasonDxpNamespaceNotFound,
			wantStatus:   metav1.ConditionFalse,
		},
		"an unlisted namespace is refused": {
			dxpNamespace: "liferay-dev",
			messages: []string{
				`Namespace "liferay-dev" does not list "able"`,
				`"cx.liferay.com/allowed-client-extension-namespaces"`,
			},
			objects:    []client.Object{newDxpNamespace("baker")},
			wantPhase:  cxv1alpha1.PhaseDegraded,
			wantReason: ReasonNamespaceNotPermitted,
			wantStatus: metav1.ConditionFalse,
		},
		"no dxpNamespace needs no consent": {
			messages:   []string{`in namespace "able"`},
			objects:    []client.Object{newDxpMetadata("able", "liferay.com")},
			wantPhase:  cxv1alpha1.PhaseReady,
			wantReason: ReasonDelivered,
			wantStatus: metav1.ConditionTrue,
		},
		"the DXP namespace itself needs no consent": {
			dxpNamespace: "able",
			messages:     []string{`in namespace "able"`},
			objects:      []client.Object{newDxpMetadata("able", "liferay.com")},
			wantPhase:    cxv1alpha1.PhaseReady,
			wantReason:   ReasonDelivered,
			wantStatus:   metav1.ConditionTrue,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			clientExtension := newClientExtension(testCase.dxpNamespace, "sample", "able")

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

			delivered := meta.FindStatusCondition(
				updatedClientExtension.Status.Conditions, cxv1alpha1.ConditionDelivered,
			)

			if delivered == nil {
				t.Fatal("Expected a Delivered condition")
			}

			if (delivered.Reason != testCase.wantReason) || (delivered.Status != testCase.wantStatus) {
				t.Errorf(
					"Delivered = %s / %s, want %s / %s",
					delivered.Status, delivered.Reason, testCase.wantStatus, testCase.wantReason,
				)
			}

			for _, message := range testCase.messages {
				if !strings.Contains(delivered.Message, message) {
					t.Errorf("Expected the Delivered message to contain %q, got %q", message, delivered.Message)
				}
			}
		})
	}
}

func TestReconcileRetriesWhenTheDxpNamespaceIsUnreadable(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "sample", "able")

	clientExtensionReconciler := newReconciler(
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

	_, reconcileError := clientExtensionReconciler.Reconcile(
		context.Background(),
		controllerruntime.Request{NamespacedName: client.ObjectKeyFromObject(clientExtension)},
	)

	if reconcileError == nil {
		t.Fatal("Reconcile() error = nil, want the error so the request is retried")
	}

	var updatedClientExtension cxv1alpha1.ClientExtension

	if error := clientExtensionReconciler.Get(
		context.Background(), client.ObjectKeyFromObject(clientExtension), &updatedClientExtension,
	); error != nil {
		t.Fatal(error)
	}

	if updatedClientExtension.Status.Phase != "" {
		t.Errorf("Expected the status to be left alone, got phase %q", updatedClientExtension.Status.Phase)
	}
}

func TestReconcileUpdatesItsOwnExtProvisionConfigMapInPlace(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "able", "able")

	owned := newExtProvision(map[string]string{
		LabelMetadataType:   MetadataTypeExtProvision,
		LabelOwnerName:      "able",
		LabelOwnerNamespace: "able",
	})

	owned.Annotations = map[string]string{
		AnnotationDomains:    "old.example.com",
		AnnotationMainDomain: "old.example.com",
	}
	owned.UID = "able-uid"

	clientExtensionReconciler := newReconciler(
		nil, t, clientExtension, owned,
		newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace("able"),
	)

	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	configMap := getExtProvision(clientExtensionReconciler, "liferay-dev", t)

	if configMap.UID != owned.UID {
		t.Errorf("uid = %q, want %q: recreating the ConfigMap would issue new OAuth2 credentials", configMap.UID, owned.UID)
	}

	if !strings.Contains(configMap.Data["able.client-extension-config.json"], "CETConfiguration~able") {
		t.Errorf("Expected the current payload, got %v", configMap.Data)
	}

	if _, ok := configMap.Data["owner"]; ok {
		t.Errorf("Expected the old payload to be replaced, got %v", configMap.Data)
	}

	if _, ok := configMap.Annotations[AnnotationDomains]; ok {
		t.Errorf("Expected the domain annotations to be removed with the domain, got %v", configMap.Annotations)
	}
}

func TestRequestsForConfigMapRequeuesClientExtensionsThatDependOnIt(t *testing.T) {
	otherInstance := newClientExtension("liferay-dev", "other-instance", "baker")

	otherInstance.Spec.VirtualInstanceID = "other.test"

	clientExtensionReconciler := newReconciler(
		nil, t,
		newClientExtension("", "beside", "liferay-dev"),
		newClientExtension("liferay-dev", "elsewhere", "able"),
		newClientExtension("liferay-uat", "other-liferay", "able"),
		otherInstance,
	)

	extProvision := newExtProvision(map[string]string{
		LabelMetadataType:    MetadataTypeExtProvision,
		LabelServiceID:       "elsewhere",
		LabelVirtualInstance: "liferay.com",
	})

	unlabelled := newDxpMetadata("liferay-dev", "liferay.com")

	delete(unlabelled.Labels, LabelMetadataType)

	testCases := map[string]struct {
		object client.Object
		want   []string
	}{
		"a ConfigMap DXP did not publish requeues nothing": {
			object: unlabelled,
		},
		"a dxp metadata ConfigMap requeues every client extension on its virtual instance": {
			object: newDxpMetadata("liferay-dev", "liferay.com"),
			want:   []string{"able/elsewhere", "liferay-dev/beside"},
		},
		"an ext-provision ConfigMap requeues only its serviceId": {
			object: extProvision,
			want:   []string{"able/elsewhere"},
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			var names []string

			for _, request := range clientExtensionReconciler.requestsForConfigMap(context.Background(), testCase.object) {
				names = append(names, request.Namespace+"/"+request.Name)
			}

			slices.Sort(names)

			if !slices.Equal(names, testCase.want) {
				t.Errorf("requestsForConfigMap() = %v, want %v", names, testCase.want)
			}
		})
	}
}

func TestRequestsForNamespaceRequeuesClientExtensions(t *testing.T) {
	clientExtensionReconciler := newReconciler(
		nil, t,
		newClientExtension("", "beside", "liferay-dev"),
		newClientExtension("liferay-dev", "elsewhere", "able"),
		newClientExtension("liferay-uat", "other-liferay", "able"),
		newClientExtension("baker", "own-namespace", "baker"),
		newClientExtension("liferay-dev", "second", "baker"),
	)

	var names []string

	for _, request := range clientExtensionReconciler.requestsForNamespace(context.Background(), newDxpNamespace("")) {
		names = append(names, request.Namespace+"/"+request.Name)
	}

	slices.Sort(names)

	if want := []string{"able/elsewhere", "baker/second"}; !slices.Equal(names, want) {
		t.Errorf("requestsForNamespace() = %v, want %v", names, want)
	}
}

func TestSummarizeDerivesReadyFromTheSteps(t *testing.T) {
	testCases := map[string]struct {
		conditions  []metav1.Condition
		wantMessage string
		wantPhase   string
		wantReason  string
		wantStatus  metav1.ConditionStatus
	}{
		"a false step is Degraded even after an unknown one": {
			conditions: []metav1.Condition{
				{Message: "Delivery is pending.", Reason: "DeliveryPending", Status: metav1.ConditionUnknown, Type: cxv1alpha1.ConditionDelivered},
				{Message: "Provisioning failed.", Reason: "ProvisioningFailed", Status: metav1.ConditionFalse, Type: cxv1alpha1.ConditionProvisioned},
			},
			wantMessage: "Provisioning failed.",
			wantPhase:   cxv1alpha1.PhaseDegraded,
			wantReason:  "ProvisioningFailed",
			wantStatus:  metav1.ConditionFalse,
		},
		"a missing step is Pending": {
			conditions: []metav1.Condition{
				{Message: "Delivered.", Reason: "Delivered", Status: metav1.ConditionTrue, Type: cxv1alpha1.ConditionDelivered},
			},
			wantMessage: "The Provisioned step has not been evaluated.",
			wantPhase:   cxv1alpha1.PhasePending,
			wantReason:  ReasonStepNotEvaluated,
			wantStatus:  metav1.ConditionUnknown,
		},
		"every true step is Ready": {
			conditions: []metav1.Condition{
				{Message: "Delivered.", Reason: "Delivered", Status: metav1.ConditionTrue, Type: cxv1alpha1.ConditionDelivered},
				{Message: "Provisioned.", Reason: "Provisioned", Status: metav1.ConditionTrue, Type: cxv1alpha1.ConditionProvisioned},
			},
			wantMessage: "Every step is complete.",
			wantPhase:   cxv1alpha1.PhaseReady,
			wantReason:  ReasonStepsComplete,
			wantStatus:  metav1.ConditionTrue,
		},
		"the first unknown step is Pending": {
			conditions: []metav1.Condition{
				{Message: "Delivery is pending.", Reason: "DeliveryPending", Status: metav1.ConditionUnknown, Type: cxv1alpha1.ConditionDelivered},
				{Message: "Provisioning is pending.", Reason: "ProvisioningPending", Status: metav1.ConditionUnknown, Type: cxv1alpha1.ConditionProvisioned},
			},
			wantMessage: "Delivery is pending.",
			wantPhase:   cxv1alpha1.PhasePending,
			wantReason:  "DeliveryPending",
			wantStatus:  metav1.ConditionUnknown,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			status := cxv1alpha1.ClientExtensionStatus{Conditions: testCase.conditions}

			summarize(
				[]string{cxv1alpha1.ConditionDelivered, cxv1alpha1.ConditionProvisioned}, 3, &status,
			)

			if status.Phase != testCase.wantPhase {
				t.Errorf("phase = %q, want %q", status.Phase, testCase.wantPhase)
			}

			ready := meta.FindStatusCondition(status.Conditions, cxv1alpha1.ConditionReady)

			if ready == nil {
				t.Fatal("Expected a Ready condition")
			}

			if (ready.Message != testCase.wantMessage) || (ready.Reason != testCase.wantReason) || (ready.Status != testCase.wantStatus) {
				t.Errorf(
					"Ready = %s / %s / %q, want %s / %s / %q",
					ready.Status, ready.Reason, ready.Message,
					testCase.wantStatus, testCase.wantReason, testCase.wantMessage,
				)
			}

			if ready.ObservedGeneration != 3 {
				t.Errorf("Ready observedGeneration = %d, want 3", ready.ObservedGeneration)
			}
		})
	}
}

func getClientExtension(
	clientExtension *cxv1alpha1.ClientExtension,
	clientExtensionReconciler *ClientExtensionReconciler,
	t *testing.T,
) *cxv1alpha1.ClientExtension {
	t.Helper()

	var updatedClientExtension cxv1alpha1.ClientExtension

	if error := clientExtensionReconciler.Get(
		context.Background(), client.ObjectKeyFromObject(clientExtension), &updatedClientExtension,
	); error != nil {
		t.Fatal(error)
	}

	return &updatedClientExtension
}

func getDelivered(
	clientExtension *cxv1alpha1.ClientExtension,
	clientExtensionReconciler *ClientExtensionReconciler,
	t *testing.T,
) *metav1.Condition {
	t.Helper()

	delivered := meta.FindStatusCondition(
		getClientExtension(clientExtension, clientExtensionReconciler, t).Status.Conditions,
		cxv1alpha1.ConditionDelivered,
	)

	if delivered == nil {
		t.Fatal("Expected a Delivered condition")
	}

	return delivered
}

func getExtProvision(
	clientExtensionReconciler *ClientExtensionReconciler,
	namespace string,
	t *testing.T,
) *corev1.ConfigMap {
	t.Helper()

	var configMap corev1.ConfigMap

	error := clientExtensionReconciler.Get(
		context.Background(),
		types.NamespacedName{Name: "able-liferay.com-lxc-ext-provision-metadata", Namespace: namespace},
		&configMap,
	)

	if apierrors.IsNotFound(error) {
		return nil
	}

	if error != nil {
		t.Fatal(error)
	}

	return &configMap
}

func newClientExtension(dxpNamespace string, name string, namespace string) *cxv1alpha1.ClientExtension {
	return &cxv1alpha1.ClientExtension{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: cxv1alpha1.ClientExtensionSpec{
			Configs: map[string]cxv1alpha1.Configuration{
				"com.liferay.client.extension.type.configuration.CETConfiguration~" + name: {
					JSON: apiextensionsv1.JSON{Raw: []byte(`{"name": "Sample"}`)},
				},
			},
			DxpNamespace:      dxpNamespace,
			ServiceID:         name,
			VirtualInstanceID: "liferay.com",
		},
	}
}

func newDxpMetadata(namespace string, virtualInstanceID string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				LabelMetadataType:    MetadataTypeDxp,
				LabelVirtualInstance: virtualInstanceID,
			},
			Name:      DxpMetadataName(virtualInstanceID),
			Namespace: namespace,
		},
	}
}

func newDxpNamespace(permittedNamespaces string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				cxv1alpha1.AnnotationAllowedClientExtensionNamespaces: permittedNamespaces,
			},
			Name: "liferay-dev",
		},
	}
}

func newExtProvision(labels map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		Data: map[string]string{"owner": "untouched"},
		ObjectMeta: metav1.ObjectMeta{
			Labels:    labels,
			Name:      "able-liferay.com-lxc-ext-provision-metadata",
			Namespace: "liferay-dev",
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

	var updatedClientExtension cxv1alpha1.ClientExtension

	if error := clientExtensionReconciler.Get(
		context.Background(), client.ObjectKeyFromObject(clientExtension), &updatedClientExtension,
	); error != nil {
		t.Fatal(error)
	}

	delivered := meta.FindStatusCondition(updatedClientExtension.Status.Conditions, cxv1alpha1.ConditionDelivered)

	if delivered == nil {
		t.Fatal("Expected a Delivered condition")
	}

	return updatedClientExtension.Status.Phase, delivered.Reason
}
