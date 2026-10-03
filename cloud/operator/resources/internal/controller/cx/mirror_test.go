package cx

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"

	cxv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/cx/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	interceptor "sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	reconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const userAgentApplicationPID = "com.liferay.oauth2.provider.configuration.OAuth2ProviderApplicationUserAgentConfiguration~able-oauth-application-user-agent"

func TestReconcileDeletesExtInitMirrorNoLongerRequired(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "able", "able")

	addOAuth2Application(clientExtension, userAgentApplicationPID)

	clientExtensionReconciler := newReconciler(
		nil, t, clientExtension, newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace("able"),
		newExtInit("able-oauth-application-user-agent"),
	)

	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	if getConfigMap(clientExtensionReconciler, "able-liferay.com-lxc-ext-init-metadata", "able", t) == nil {
		t.Fatal("Expected the ext-init mirror")
	}

	updatedClientExtension := getClientExtension(clientExtension, clientExtensionReconciler, t)

	delete(updatedClientExtension.Spec.Configs, userAgentApplicationPID)

	if error := clientExtensionReconciler.Update(context.Background(), updatedClientExtension); error != nil {
		t.Fatal(error)
	}

	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	if getConfigMap(clientExtensionReconciler, "able-liferay.com-lxc-ext-init-metadata", "able", t) != nil {
		t.Error("Expected the ext-init mirror to be deleted once the configs no longer require ext-init")
	}

	if getConfigMap(clientExtensionReconciler, "liferay.com-lxc-dxp-metadata", "able", t) == nil {
		t.Error("Expected the dxp metadata mirror to remain")
	}
}

func TestReconcileDeletesMirrorsWhenRefused(t *testing.T) {
	testCases := map[string]struct {
		refuse     func(clientExtensionReconciler *ClientExtensionReconciler)
		wantReason string
	}{
		"the DXP namespace no longer permits it": {
			refuse: func(clientExtensionReconciler *ClientExtensionReconciler) {
				dxpNamespace := newDxpNamespace("")

				if error := clientExtensionReconciler.Update(context.Background(), dxpNamespace); error != nil {
					t.Fatal(error)
				}
			},
			wantReason: ReasonNamespaceNotPermitted,
		},
		"the virtual instance disappears": {
			refuse: func(clientExtensionReconciler *ClientExtensionReconciler) {
				if error := clientExtensionReconciler.Delete(
					context.Background(), newDxpMetadata("liferay-dev", "liferay.com"),
				); error != nil {
					t.Fatal(error)
				}
			},
			wantReason: ReasonUnknownVirtualInstance,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			clientExtension := newClientExtension("liferay-dev", "able", "able")

			addOAuth2Application(clientExtension, userAgentApplicationPID)

			clientExtensionReconciler := newReconciler(
				nil, t, clientExtension, newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace("able"),
				newExtInit("able-oauth-application-user-agent"),
			)

			reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

			testCase.refuse(clientExtensionReconciler)

			if _, reason := reconcileClientExtension(clientExtension, clientExtensionReconciler, t); reason != testCase.wantReason {
				t.Errorf("Delivered reason = %q, want %q", reason, testCase.wantReason)
			}

			for _, name := range []string{"able-liferay.com-lxc-ext-init-metadata", "liferay.com-lxc-dxp-metadata"} {
				if getConfigMap(clientExtensionReconciler, name, "able", t) != nil {
					t.Errorf("Expected the mirror %s to be deleted once the client extension is refused", name)
				}
			}
		})
	}
}

func TestReconcileIgnoresMirrorAsDxpMetadata(t *testing.T) {
	able := newClientExtension("liferay-dev", "able", "able")
	baker := newClientExtension("", "baker", "able")

	clientExtensionReconciler := newReconciler(
		nil, t, able, baker, newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace("able"),
	)

	reconcileClientExtension(able, clientExtensionReconciler, t)

	if getConfigMap(clientExtensionReconciler, "liferay.com-lxc-dxp-metadata", "able", t) == nil {
		t.Fatal("Expected the dxp metadata mirror")
	}

	if _, reason := reconcileClientExtension(baker, clientExtensionReconciler, t); reason != ReasonUnknownVirtualInstance {
		t.Errorf("Delivered reason = %q, want %q: a mirror does not make its namespace a DXP", reason, ReasonUnknownVirtualInstance)
	}

	if getConfigMap(clientExtensionReconciler, "baker-liferay.com-lxc-ext-provision-metadata", "able", t) != nil {
		t.Error("Expected no ext-provision ConfigMap beside a mirror")
	}
}

func TestReconcileKeepsMirrorClaimedAfterTheCacheRead(t *testing.T) {
	able := newClientExtension("liferay-dev", "able", "able")

	claimBehindTheCache := false

	clientExtensionReconciler := newReconciler(
		&interceptor.Funcs{
			List: func(
				context context.Context, client client.WithWatch, list client.ObjectList,
				options ...client.ListOption,
			) error {
				if error := client.List(context, list, options...); error != nil {
					return error
				}

				configMapList, ok := list.(*corev1.ConfigMapList)

				if !ok || !claimBehindTheCache {
					return nil
				}

				for _, configMap := range configMapList.Items {
					if (configMap.Name != "liferay.com-lxc-dxp-metadata") || (configMap.Namespace != "able") {
						continue
					}

					claimBehindTheCache = false

					configMap.OwnerReferences = append(
						configMap.OwnerReferences,
						metav1.OwnerReference{
							APIVersion: "cx.liferay.com/v1alpha1", Kind: "ClientExtension", Name: "baker", UID: "able-baker",
						},
					)

					if error := client.Update(context, &configMap); error != nil {
						return error
					}
				}

				return nil
			},
		},
		t, able, newDxpMetadata("liferay-dev", "liferay.com"), newDxpMetadata("liferay-dev", "other.test"),
		newDxpNamespace("able"),
	)

	reconcileClientExtension(able, clientExtensionReconciler, t)

	updatedClientExtension := getClientExtension(able, clientExtensionReconciler, t)

	updatedClientExtension.Spec.VirtualInstanceID = "other.test"

	if error := clientExtensionReconciler.Update(context.Background(), updatedClientExtension); error != nil {
		t.Fatal(error)
	}

	claimBehindTheCache = true

	if _, error := clientExtensionReconciler.Reconcile(
		context.Background(), controllerruntime.Request{NamespacedName: client.ObjectKeyFromObject(able)},
	); !apierrors.IsConflict(error) {
		t.Errorf("Reconcile() error = %v, want a conflict", error)
	}

	if getConfigMap(clientExtensionReconciler, "liferay.com-lxc-dxp-metadata", "able", t) == nil {
		t.Error("Expected the mirror another client extension claimed after the cache read to remain")
	}
}

func TestReconcileMirrorsDxpMetadataIntoClientExtensionNamespace(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "able", "able")

	dxpMetadata := newDxpMetadata("liferay-dev", "liferay.com")

	dxpMetadata.Data = map[string]string{"com.liferay.lxc.dxp.mainDomain": "liferay.example.com"}

	clientExtensionReconciler := newReconciler(nil, t, clientExtension, dxpMetadata, newDxpNamespace("able"))

	if phase, _ := reconcileClientExtension(clientExtension, clientExtensionReconciler, t); phase != cxv1alpha1.PhaseReady {
		t.Errorf("phase = %q, want %q", phase, cxv1alpha1.PhaseReady)
	}

	configMap := getConfigMap(clientExtensionReconciler, "liferay.com-lxc-dxp-metadata", "able", t)

	if configMap == nil {
		t.Fatal("Expected ConfigMap able/liferay.com-lxc-dxp-metadata")
	}

	if !maps.Equal(configMap.Data, dxpMetadata.Data) {
		t.Errorf("data = %v, want %v", configMap.Data, dxpMetadata.Data)
	}

	wantLabels := map[string]string{
		LabelMetadataType:    MetadataTypeDxp,
		LabelMirror:          "true",
		LabelVirtualInstance: "liferay.com",
	}

	if !maps.Equal(configMap.Labels, wantLabels) {
		t.Errorf("labels = %v, want %v", configMap.Labels, wantLabels)
	}

	if source := configMap.Annotations[AnnotationSource]; source != "liferay-dev/liferay.com-lxc-dxp-metadata" {
		t.Errorf("source = %q, want %q", source, "liferay-dev/liferay.com-lxc-dxp-metadata")
	}

	if (len(configMap.OwnerReferences) != 1) || (clientExtension.UID != configMap.OwnerReferences[0].UID) ||
		((configMap.OwnerReferences[0].Controller != nil) && *configMap.OwnerReferences[0].Controller) {

		t.Errorf("owner references = %v, want one non-controller reference to the client extension", configMap.OwnerReferences)
	}
}

func TestReconcileMirrorsExtInitIntoClientExtensionNamespace(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "able", "able")

	addOAuth2Application(clientExtension, userAgentApplicationPID)

	extInit := newExtInit("able-oauth-application-user-agent")

	extInit.Data["able-oauth-application-user-agent.oauth2.user.agent.client.id"] = "able-id"

	clientExtensionReconciler := newReconciler(
		nil, t, clientExtension, extInit, newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace("able"),
	)

	if phase, _ := reconcileClientExtension(clientExtension, clientExtensionReconciler, t); phase != cxv1alpha1.PhaseReady {
		t.Errorf("phase = %q, want %q", phase, cxv1alpha1.PhaseReady)
	}

	configMap := getConfigMap(clientExtensionReconciler, "able-liferay.com-lxc-ext-init-metadata", "able", t)

	if configMap == nil {
		t.Fatal("Expected ConfigMap able/able-liferay.com-lxc-ext-init-metadata")
	}

	if !maps.Equal(configMap.Data, extInit.Data) {
		t.Errorf("data = %v, want %v", configMap.Data, extInit.Data)
	}

	wantLabels := map[string]string{
		LabelMetadataType:    MetadataTypeExtInit,
		LabelMirror:          "true",
		LabelServiceID:       "able",
		LabelVirtualInstance: "liferay.com",
	}

	if !maps.Equal(configMap.Labels, wantLabels) {
		t.Errorf("labels = %v, want %v", configMap.Labels, wantLabels)
	}

	if source := configMap.Annotations[AnnotationSource]; source != "liferay-dev/able-liferay.com-lxc-ext-init-metadata" {
		t.Errorf("source = %q, want %q", source, "liferay-dev/able-liferay.com-lxc-ext-init-metadata")
	}

	if controllerReference := metav1.GetControllerOf(configMap); (controllerReference == nil) || (clientExtension.UID != controllerReference.UID) {
		t.Errorf("controller = %v, want the client extension", controllerReference)
	}
}

func TestReconcileMirrorsExtInitWhileAnApplicationIsMissing(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "able", "able")

	addOAuth2Application(clientExtension, userAgentApplicationPID)

	extInit := newExtInit()

	extInit.Data["baker-oauth-application-user-agent.oauth2.token.uri"] = "/o/oauth2/token"

	clientExtensionReconciler := newReconciler(
		nil, t, clientExtension, extInit, newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace("able"),
	)

	if phase, _ := reconcileClientExtension(clientExtension, clientExtensionReconciler, t); phase != cxv1alpha1.PhasePending {
		t.Errorf("phase = %q, want %q", phase, cxv1alpha1.PhasePending)
	}

	configMap := getConfigMap(clientExtensionReconciler, "able-liferay.com-lxc-ext-init-metadata", "able", t)

	if (configMap == nil) || !maps.Equal(configMap.Data, extInit.Data) {
		t.Errorf("Expected ext-init to be mirrored as DXP has written it so far, got %v", configMap)
	}
}

func TestReconcileMirrorsNothingIntoDxpNamespace(t *testing.T) {
	clientExtension := newClientExtension("", "able", "liferay-dev")

	addOAuth2Application(clientExtension, userAgentApplicationPID)

	clientExtensionReconciler := newReconciler(
		nil, t, clientExtension, newDxpMetadata("liferay-dev", "liferay.com"),
		newExtInit("able-oauth-application-user-agent"),
	)

	if phase, _ := reconcileClientExtension(clientExtension, clientExtensionReconciler, t); phase != cxv1alpha1.PhaseReady {
		t.Errorf("phase = %q, want %q", phase, cxv1alpha1.PhaseReady)
	}

	var configMapList corev1.ConfigMapList

	if error := clientExtensionReconciler.List(
		context.Background(), &configMapList, client.MatchingLabels{LabelMirror: "true"},
	); error != nil {
		t.Fatal(error)
	}

	if len(configMapList.Items) != 0 {
		t.Errorf("Expected no mirror where the workload can mount DXP's own ConfigMaps, got %d", len(configMapList.Items))
	}

	for _, name := range []string{"able-liferay.com-lxc-ext-init-metadata", "liferay.com-lxc-dxp-metadata"} {
		if configMap := getConfigMap(clientExtensionReconciler, name, "liferay-dev", t); (configMap == nil) || (len(configMap.OwnerReferences) != 0) {
			t.Errorf("Expected DXP's ConfigMap %s to be untouched, got %v", name, configMap)
		}
	}
}

func TestReconcileRefusesToRepointSharedMirror(t *testing.T) {
	able := newClientExtension("liferay-dev", "able", "able")
	baker := newClientExtension("liferay-dev", "baker", "able")

	uatNamespace := newDxpNamespace("able")

	uatNamespace.Name = "liferay-uat"

	clientExtensionReconciler := newReconciler(
		nil, t, able, baker,
		newDxpMetadata("liferay-dev", "liferay.com"), newDxpMetadata("liferay-uat", "liferay.com"),
		newDxpNamespace("able"), uatNamespace,
	)

	reconcileClientExtension(able, clientExtensionReconciler, t)
	reconcileClientExtension(baker, clientExtensionReconciler, t)

	updatedClientExtension := getClientExtension(able, clientExtensionReconciler, t)

	updatedClientExtension.Spec.DxpNamespace = "liferay-uat"

	if error := clientExtensionReconciler.Update(context.Background(), updatedClientExtension); error != nil {
		t.Fatal(error)
	}

	reconcileClientExtension(able, clientExtensionReconciler, t)

	if provisioned := getCondition(able, clientExtensionReconciler, cxv1alpha1.ConditionProvisioned, t); (provisioned == nil) || (provisioned.Reason != ReasonMirrorFailed) {
		t.Errorf("Provisioned = %v, want reason %s: baker still uses the mirror of liferay-dev", provisioned, ReasonMirrorFailed)
	}

	configMap := getConfigMap(clientExtensionReconciler, "liferay.com-lxc-dxp-metadata", "able", t)

	if source := configMap.Annotations[AnnotationSource]; source != "liferay-dev/liferay.com-lxc-dxp-metadata" {
		t.Errorf("source = %q, want the mirror baker uses left alone", source)
	}
}

func TestReconcileReleasesStaleDxpMetadataMirror(t *testing.T) {
	able := newClientExtension("liferay-dev", "able", "able")
	baker := newClientExtension("liferay-dev", "baker", "able")

	clientExtensionReconciler := newReconciler(
		nil, t, able, baker,
		newDxpMetadata("liferay-dev", "liferay.com"), newDxpMetadata("liferay-dev", "other.test"), newDxpNamespace("able"),
	)

	reconcileClientExtension(able, clientExtensionReconciler, t)
	reconcileClientExtension(baker, clientExtensionReconciler, t)

	moveToOtherVirtualInstance := func(clientExtension *cxv1alpha1.ClientExtension) {
		updatedClientExtension := getClientExtension(clientExtension, clientExtensionReconciler, t)

		updatedClientExtension.Spec.VirtualInstanceID = "other.test"

		if error := clientExtensionReconciler.Update(context.Background(), updatedClientExtension); error != nil {
			t.Fatal(error)
		}

		reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

		if getConfigMap(clientExtensionReconciler, "other.test-lxc-dxp-metadata", "able", t) == nil {
			t.Errorf("Expected %s to mirror the new virtual instance", clientExtension.Name)
		}
	}

	moveToOtherVirtualInstance(able)

	configMap := getConfigMap(clientExtensionReconciler, "liferay.com-lxc-dxp-metadata", "able", t)

	if (configMap == nil) || (len(configMap.OwnerReferences) != 1) || (baker.UID != configMap.OwnerReferences[0].UID) {
		t.Fatalf("Expected the old mirror to remain, owned by baker alone, got %v", configMap)
	}

	moveToOtherVirtualInstance(baker)

	if getConfigMap(clientExtensionReconciler, "liferay.com-lxc-dxp-metadata", "able", t) != nil {
		t.Error("Expected the old mirror to be deleted once no client extension uses it")
	}
}

func TestReconcileReplacesMirrorDataWhenSourceChanges(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "able", "able")

	addOAuth2Application(clientExtension, userAgentApplicationPID)

	extInit := newExtInit()

	extInit.Data = map[string]string{"able.oauth2.user.agent.client.id": "able-id", "able.oauth2.user.agent.scopes": "everything"}

	clientExtensionReconciler := newReconciler(
		nil, t, clientExtension, extInit, newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace("able"),
	)

	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	extInit.Data = map[string]string{"able.oauth2.user.agent.client.id": "able-new-id"}

	if error := clientExtensionReconciler.Update(context.Background(), extInit); error != nil {
		t.Fatal(error)
	}

	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	configMap := getConfigMap(clientExtensionReconciler, "able-liferay.com-lxc-ext-init-metadata", "able", t)

	if !maps.Equal(configMap.Data, extInit.Data) {
		t.Errorf("data = %v, want %v: a key DXP removed must not linger in the mirror", configMap.Data, extInit.Data)
	}
}

func TestReconcileRepointsItsOwnMirrorWhenDxpNamespaceChanges(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "able", "able")

	uatDxpMetadata := newDxpMetadata("liferay-uat", "liferay.com")

	uatDxpMetadata.Data = map[string]string{"com.liferay.lxc.dxp.mainDomain": "uat.example.com"}

	uatNamespace := newDxpNamespace("able")

	uatNamespace.Name = "liferay-uat"

	clientExtensionReconciler := newReconciler(
		nil, t, clientExtension, newDxpMetadata("liferay-dev", "liferay.com"), uatDxpMetadata,
		newDxpNamespace("able"), uatNamespace,
	)

	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	updatedClientExtension := getClientExtension(clientExtension, clientExtensionReconciler, t)

	updatedClientExtension.Spec.DxpNamespace = "liferay-uat"

	if error := clientExtensionReconciler.Update(context.Background(), updatedClientExtension); error != nil {
		t.Fatal(error)
	}

	if phase, _ := reconcileClientExtension(clientExtension, clientExtensionReconciler, t); phase != cxv1alpha1.PhaseReady {
		t.Errorf("phase = %q, want %q", phase, cxv1alpha1.PhaseReady)
	}

	configMap := getConfigMap(clientExtensionReconciler, "liferay.com-lxc-dxp-metadata", "able", t)

	if source := configMap.Annotations[AnnotationSource]; source != "liferay-uat/liferay.com-lxc-dxp-metadata" {
		t.Errorf("source = %q, want %q", source, "liferay-uat/liferay.com-lxc-dxp-metadata")
	}

	if !maps.Equal(configMap.Data, uatDxpMetadata.Data) {
		t.Errorf("data = %v, want %v", configMap.Data, uatDxpMetadata.Data)
	}
}

func TestReconcileReportsMirrorConflict(t *testing.T) {
	testCases := map[string]struct {
		funcs  *interceptor.Funcs
		labels map[string]string
	}{
		"a ConfigMap the cache can see": {
			labels: map[string]string{LabelMetadataType: MetadataTypeDxp, LabelVirtualInstance: "liferay.com"},
		},
		"a ConfigMap the cache cannot see": {
			funcs: hideFromCacheInNamespace("liferay.com-lxc-dxp-metadata", "able"),
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			clientExtension := newClientExtension("liferay-dev", "able", "able")

			foreignConfigMap := &corev1.ConfigMap{
				Data: map[string]string{"owner": "untouched"},
				ObjectMeta: metav1.ObjectMeta{
					Labels:    testCase.labels,
					Name:      "liferay.com-lxc-dxp-metadata",
					Namespace: "able",
				},
			}

			clientExtensionReconciler := newReconciler(
				testCase.funcs, t, clientExtension, foreignConfigMap,
				newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace("able"),
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

			if phase := getClientExtension(clientExtension, clientExtensionReconciler, t).Status.Phase; phase != cxv1alpha1.PhaseDegraded {
				t.Errorf("phase = %q, want %q", phase, cxv1alpha1.PhaseDegraded)
			}

			provisioned := getCondition(clientExtension, clientExtensionReconciler, cxv1alpha1.ConditionProvisioned, t)

			if (provisioned == nil) || (provisioned.Status != metav1.ConditionFalse) || (provisioned.Reason != ReasonMirrorFailed) {
				t.Fatalf("Provisioned = %v, want False / %s", provisioned, ReasonMirrorFailed)
			}

			want := `configmap "liferay.com-lxc-dxp-metadata" exists and is not the operator's mirror of "liferay-dev/liferay.com-lxc-dxp-metadata"`

			if !strings.Contains(provisioned.Message, want) {
				t.Errorf("Provisioned message = %q, want it to contain %q", provisioned.Message, want)
			}

			configMap := getConfigMap(clientExtensionReconciler, "liferay.com-lxc-dxp-metadata", "able", t)

			if !maps.Equal(configMap.Data, foreignConfigMap.Data) || !maps.Equal(configMap.Labels, foreignConfigMap.Labels) {
				t.Errorf("Expected the foreign ConfigMap to be untouched, got %v / %v", configMap.Labels, configMap.Data)
			}
		})
	}
}

func TestReconcileReportsMirrorNotPermitted(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "able", "able")

	clientExtensionReconciler := newReconciler(
		&interceptor.Funcs{
			Create: func(
				context context.Context, client client.WithWatch, object client.Object,
				options ...client.CreateOption,
			) error {
				if _, ok := object.(*corev1.ConfigMap); ok && (object.GetNamespace() == "able") {
					return apierrors.NewForbidden(corev1.Resource("configmaps"), object.GetName(), errors.New("no RoleBinding"))
				}

				return client.Create(context, object, options...)
			},
		},
		t, clientExtension, newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace("able"),
	)

	reconcileClientExtension(clientExtension, clientExtensionReconciler, t)

	provisioned := getCondition(clientExtension, clientExtensionReconciler, cxv1alpha1.ConditionProvisioned, t)

	if (provisioned == nil) || (provisioned.Reason != ReasonMirrorFailed) {
		t.Fatalf("Provisioned = %v, want reason %s", provisioned, ReasonMirrorFailed)
	}

	if want := `Mirror DXP metadata forbidden: the DXP operator is not permitted to write ConfigMaps in namespace "able".`; provisioned.Message != want {
		t.Errorf("Provisioned message = %q, want %q", provisioned.Message, want)
	}
}

func TestReconcileRetriesTransientMirrorError(t *testing.T) {
	clientExtension := newClientExtension("liferay-dev", "able", "able")

	clientExtensionReconciler := newReconciler(
		&interceptor.Funcs{
			Create: func(
				context context.Context, client client.WithWatch, object client.Object,
				options ...client.CreateOption,
			) error {
				if _, ok := object.(*corev1.ConfigMap); ok && (object.GetNamespace() == "able") {
					return apierrors.NewServiceUnavailable("etcd is unavailable")
				}

				return client.Create(context, object, options...)
			},
		},
		t, clientExtension, newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace("able"),
	)

	if _, error := clientExtensionReconciler.Reconcile(
		context.Background(), controllerruntime.Request{NamespacedName: client.ObjectKeyFromObject(clientExtension)},
	); !apierrors.IsServiceUnavailable(error) {
		t.Errorf("Reconcile() error = %v, want it returned for a retry with backoff", error)
	}

	if provisioned := getCondition(clientExtension, clientExtensionReconciler, cxv1alpha1.ConditionProvisioned, t); (provisioned != nil) && (provisioned.Reason == ReasonMirrorFailed) {
		t.Errorf("Provisioned = %v, want no %s for a transient error", provisioned, ReasonMirrorFailed)
	}
}

func TestReconcileSharesDxpMetadataMirrorBetweenClientExtensions(t *testing.T) {
	able := newClientExtension("liferay-dev", "able", "able")
	baker := newClientExtension("liferay-dev", "baker", "able")

	clientExtensionReconciler := newReconciler(
		nil, t, able, baker, newDxpMetadata("liferay-dev", "liferay.com"), newDxpNamespace("able"),
	)

	reconcileClientExtension(able, clientExtensionReconciler, t)
	reconcileClientExtension(baker, clientExtensionReconciler, t)

	configMap := getConfigMap(clientExtensionReconciler, "liferay.com-lxc-dxp-metadata", "able", t)

	var ownerUIDs []types.UID

	for _, ownerReference := range configMap.OwnerReferences {
		ownerUIDs = append(ownerUIDs, ownerReference.UID)
	}

	if wantOwnerUIDs := []types.UID{able.UID, baker.UID}; !reflect.DeepEqual(ownerUIDs, wantOwnerUIDs) {
		t.Errorf("owners = %v, want %v", ownerUIDs, wantOwnerUIDs)
	}
}

func TestRequestsForConfigMapMapsMirrorToItsOwners(t *testing.T) {
	clientExtensionReconciler := newReconciler(nil, t, newClientExtension("able", "charlie", "liferay-dev"))

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				LabelMetadataType:    MetadataTypeDxp,
				LabelMirror:          "true",
				LabelVirtualInstance: "liferay.com",
			},
			Name:      "liferay.com-lxc-dxp-metadata",
			Namespace: "able",
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: "cx.liferay.com/v1alpha1", Kind: "ClientExtension", Name: "able", UID: "able-able"},
				{APIVersion: "apps/v1", Kind: "Deployment", Name: "delta", UID: "able-delta"},
				{APIVersion: "cx.liferay.com/v1alpha1", Kind: "ClientExtension", Name: "baker", UID: "able-baker"},
				{APIVersion: "cx.liferay.com/v1beta1", Kind: "ClientExtension", Name: "charlie", UID: "able-charlie"},
				{APIVersion: "other.liferay.com/v1alpha1", Kind: "ClientExtension", Name: "dog", UID: "able-dog"},
			},
		},
	}

	want := []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: "able", Namespace: "able"}},
		{NamespacedName: types.NamespacedName{Name: "baker", Namespace: "able"}},
		{NamespacedName: types.NamespacedName{Name: "charlie", Namespace: "able"}},
	}

	if got := clientExtensionReconciler.requestsForConfigMap(context.Background(), configMap); !reflect.DeepEqual(got, want) {
		t.Errorf("requestsForConfigMap() = %v, want %v: a mirror maps to its owners, not to the client extensions delivering to its namespace", got, want)
	}
}

func hideFromCacheInNamespace(name string, namespace string) *interceptor.Funcs {
	return &interceptor.Funcs{
		Get: func(
			context context.Context, client client.WithWatch, key client.ObjectKey,
			object client.Object, options ...client.GetOption,
		) error {
			if _, ok := object.(*corev1.ConfigMap); ok && (key.Name == name) && (key.Namespace == namespace) {
				return apierrors.NewNotFound(corev1.Resource("configmaps"), key.Name)
			}

			return client.Get(context, key, object, options...)
		},
	}
}
