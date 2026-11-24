package schema

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	types "k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	record "k8s.io/client-go/tools/record"
	controllerruntime "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	fake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testDowngradeImage = "liferay/dxp:2026.q2.12"
	testName           = "liferay-default"
	testNamespace      = "liferay-test"
	testNewImage       = "liferay/dxp:2026.q3.10"
	testOldImage       = "liferay/dxp:2026.q1.14-lts"
	testReleaseNew     = "tables=1200\nportal=38.7.9\nfailed=0\nfailedModules="
	testReleaseOld     = "tables=1100\nportal=38.2.12\nfailed=0\nfailedModules="
)

func (fakeUpgradeChecker fakeUpgradeChecker) GetPendingUpgrades(
	container string,
	context context.Context,
	pod *corev1.Pod,
) (string, error) {
	return string(fakeUpgradeChecker), nil
}

func (fakeReleaseCatalog fakeReleaseCatalog) GetSchemaVersion(
	context context.Context,
	image string,
) (string, error) {
	schemaVersion, ok := fakeReleaseCatalog[image]

	if !ok {
		return "", fmt.Errorf("unknown image %s", image)
	}

	return schemaVersion, nil
}

func (fakeLogReader *fakeLogReader) GetUpgradeResult(
	container string,
	context context.Context,
	pod *corev1.Pod,
) (string, string, error) {
	return fakeLogReader.result, fakeLogReader.upgradeType, nil
}

func TestCompareVersions(t *testing.T) {
	testCases := map[string]struct {
		version1 string
		version2 string
		want     int
	}{
		"equal versions": {
			version1: "39.2.1",
			version2: "39.2.1",
			want:     0,
		},
		"missing part counts as zero": {
			version1: "39.2",
			version2: "39.2.0",
			want:     0,
		},
		"newer version": {
			version1: "38.7.9",
			version2: "38.2.12",
			want:     1,
		},
		"older version": {
			version1: "38.2.10",
			version2: "38.2.12",
			want:     -1,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			got, error := compareVersions(testCase.version1, testCase.version2)

			if (error != nil) || (got != testCase.want) {
				t.Fatalf(
					"compareVersions(%q, %q) = %d, %v, want %d",
					testCase.version1, testCase.version2, got, error,
					testCase.want,
				)
			}
		})
	}
}

func TestGetReleaseKey(t *testing.T) {
	for image, want := range map[string]string{
		"docker.io/liferay/dxp:2026.q3.10":     "dxp-2026.q3.10",
		"liferay/dxp:2026.Q1.14-LTS":           "dxp-2026.q1.14-lts",
		"liferay/dxp:2026.q2.12-slim":          "dxp-2026.q2.12",
		"registry.example.com:5000/custom:1.0": "",
	} {
		got, error := getReleaseKey(image)

		if (want == "") != (error != nil) || (got != want) {
			t.Fatalf("getReleaseKey(%q) = %q, %v; want %q", image, got, error, want)
		}
	}
}

func TestParseDatabaseState(t *testing.T) {
	state, error := parseDatabaseState(
		"tables=1200\nportal=38.7.9\nfailed=2\nfailedModules=com.liferay.a,com.liferay.b\n",
	)

	if (error != nil) || (state.tables != 1200) || (state.portal != "38.7.9") ||
		(state.failed != 2) || (state.failedModules != "com.liferay.a,com.liferay.b") {

		t.Fatalf("parseDatabaseState = %+v, %v", state, error)
	}

	if state, error := parseDatabaseState("tables=0\n"); (error != nil) || (state.tables != 0) {
		t.Fatalf("parseDatabaseState of a reset = %+v, %v", state, error)
	}

	if _, error := parseDatabaseState("ERROR: permission denied"); error == nil {
		t.Fatal("parseDatabaseState accepted a message without a table count")
	}
}

func TestParseUpgradeCheck(t *testing.T) {
	banner := "Trying 127.0.0.1...\r\nConnected to localhost.\r\nEscape character is '^]'.\r\nupgrade:check\r\n\r\n____________________________\r\nWelcome to Apache Felix Gogo\r\n\r\ng! "

	if pending, error := parseUpgradeCheck(banner + "\r\ng! upgrade-check-done\r\ng! Connection closed by foreign host.\n"); (error != nil) || (pending != "") {
		t.Fatalf("parseUpgradeCheck = %q, %v; want no pending upgrades", pending, error)
	}

	if _, error := parseUpgradeCheck(banner + "\r\ng! Connection closed by foreign host.\n"); error == nil {
		t.Fatal("parseUpgradeCheck accepted output without the marker")
	}

	pending, error := parseUpgradeCheck(
		banner + "There are upgrade processes available for com.liferay.c from 1.0.0 to 2.0.0\r\ng! upgrade-check-done\r\n",
	)

	if (error != nil) || !strings.Contains(pending, "com.liferay.c from 1.0.0 to 2.0.0") {
		t.Fatalf("parseUpgradeCheck = %q, %v", pending, error)
	}

	if _, error := parseUpgradeCheck("telnet: Unable to connect to remote host"); error == nil {
		t.Fatal("parseUpgradeCheck accepted output without a Gogo shell")
	}
}

func TestParseUpgradeResult(t *testing.T) {
	log := strings.Join(
		[]string{
			"2026-11-20 00:56:58.182 INFO  [main][VerifyProcess:50] Verifying",
			"2026-11-20 00:59:42.158 INFO  [main][UpgradeRecorder:187] Major upgrade finished with result failure",
			"2026-11-20 00:59:52.228 INFO [main] Server startup in [210704] milliseconds",
		},
		"\n",
	)

	result, upgradeType, error := parseUpgradeResult(bufio.NewScanner(strings.NewReader(log)))

	if (error != nil) || (result != "failure") || (upgradeType != "Major") {
		t.Fatalf("parseUpgradeResult = %q, %q, %v", result, upgradeType, error)
	}
}

func TestReconcileChoosesANewHolderWhenTheHolderIsGone(t *testing.T) {
	statefulSet := newStatefulSet(testNewImage, testOldImage)

	statefulSet.Annotations[AnnotationUpgradeHolder] = testName + "-1"
	statefulSet.Annotations[AnnotationUpgradeTarget] = testNewImage

	reconciler, kubeClient := newReconciler(
		t, statefulSet, newPod(testNewImage, 0, false),
	)

	reconcile(reconciler, t)

	if decision := getPod(kubeClient, 0, t).Annotations[AnnotationDecision]; decision != DecisionUpgrade {
		t.Fatalf("pod 0 decision = %q while holder pod 1 is gone", decision)
	}

	if holder := getStatefulSet(kubeClient, t).Annotations[AnnotationUpgradeHolder]; holder != testName+"-0" {
		t.Fatalf("holder = %q", holder)
	}
}

func TestReconcileFailsAnUpgradeThatReleaseDoesNotConfirm(t *testing.T) {
	testCases := map[string]struct {
		message string
		pending string
		release string
	}{
		"failed modules in Release_": {
			message: "2 modules in a failed state (com.liferay.a,com.liferay.b)",
			release: "tables=1200\nportal=38.7.9\nfailed=2\nfailedModules=com.liferay.a,com.liferay.b",
		},
		"old schema in Release_": {
			message: "\"Release_\" has schema 38.2.12 instead of 38.7.9",
			release: testReleaseOld,
		},
		"pending upgrades in upgrade:check": {
			message: "\"upgrade:check\" lists pending upgrades: com.liferay.c",
			pending: "com.liferay.c",
			release: testReleaseNew,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			statefulSet := newStatefulSet(testNewImage, testOldImage)

			statefulSet.Annotations[AnnotationUpgradeCheck] = UpgradeCheckGogo

			reconciler, kubeClient := newReconciler(
				t, statefulSet, newPod(testNewImage, 0, true),
				newPod(testNewImage, 1, false),
			)

			reconciler.UpgradeChecker = fakeUpgradeChecker(testCase.pending)

			reconcile(reconciler, t)

			completeStatefulSetJob(DatabaseJobPrecheck, kubeClient, testReleaseOld, t)

			reconcile(reconciler, t)

			completeStatefulSetJob(DatabaseJobPostcheck, kubeClient, testCase.release, t)

			reconcile(reconciler, t)

			statefulSet = getStatefulSet(kubeClient, t)

			if result := statefulSet.Annotations[AnnotationUpgradeResult]; result != ResultFailed {
				t.Fatalf("result = %q, want %q", result, ResultFailed)
			}

			if message := statefulSet.Annotations[AnnotationUpgradeMessage]; !strings.Contains(message, testCase.message) {
				t.Fatalf("message = %q, want it to contain %q", message, testCase.message)
			}

			if decision := getPod(kubeClient, 1, t).Annotations[AnnotationDecision]; decision != "" {
				t.Fatalf("pod 1 decision = %q, want none", decision)
			}
		})
	}
}

func TestReconcileKeepsAnUpgradingHolderWithAnOlderTemplate(t *testing.T) {
	reconciler, kubeClient := newReconciler(
		t, newUpgradingStatefulSet(), newRestartedHolder(0, "revision-old"),
	)

	reconcile(reconciler, t)

	getPod(kubeClient, 0, t)
}

func TestReconcileKeepsOtherPodsWaitingAfterAFailedUpgrade(t *testing.T) {
	reconciler, kubeClient := newReconciler(
		t, newStatefulSet(testNewImage, testOldImage),
		newPod(testNewImage, 0, true), newPod(testNewImage, 1, false),
	)

	reconciler.LogReader = &fakeLogReader{result: "failure", upgradeType: "Major"}

	reconcile(reconciler, t)

	completeStatefulSetJob(DatabaseJobPrecheck, kubeClient, testReleaseOld, t)

	reconcile(reconciler, t)

	statefulSet := getStatefulSet(kubeClient, t)

	if statefulSet.Annotations[AnnotationSchemaVersion] != testOldImage {
		t.Fatalf("schema version = %q after a failed upgrade", statefulSet.Annotations[AnnotationSchemaVersion])
	}

	if statefulSet.Annotations[AnnotationUpgradeResult] != ResultFailed {
		t.Fatalf("result = %q", statefulSet.Annotations[AnnotationUpgradeResult])
	}

	if decision := getPod(kubeClient, 1, t).Annotations[AnnotationDecision]; decision != "" {
		t.Fatalf("pod 1 decision = %q after a failed upgrade", decision)
	}
}

func TestReconcileRefusesAnOlderDatabaseSchema(t *testing.T) {
	reconciler, kubeClient := newReconciler(
		t, newStatefulSet(testDowngradeImage, testOldImage),
		newPod(testOldImage, 0, true), newPod(testDowngradeImage, 1, false),
	)

	reconcile(reconciler, t)

	completeStatefulSetJob(DatabaseJobPrecheck, kubeClient, testReleaseOld, t)

	reconcile(reconciler, t)

	if getPod(kubeClient, 0, t).DeletionTimestamp != nil {
		t.Fatal("pod 0 was deleted")
	}

	if decision := getPod(kubeClient, 1, t).Annotations[AnnotationDecision]; decision != "" {
		t.Fatalf("pod 1 decision = %q for a refused upgrade", decision)
	}

	statefulSet := getStatefulSet(kubeClient, t)

	if statefulSet.Annotations[AnnotationUpgradeResult] != ResultRefused {
		t.Fatalf("result = %q", statefulSet.Annotations[AnnotationUpgradeResult])
	}

	if !strings.Contains(statefulSet.Annotations[AnnotationUpgradeMessage], "older than the schema 38.2.12 in \"Release_\"") {
		t.Fatalf("message = %q", statefulSet.Annotations[AnnotationUpgradeMessage])
	}

	if statefulSet.Annotations[AnnotationUpgradeTarget] != "" {
		t.Fatalf("target = %q for a refused upgrade", statefulSet.Annotations[AnnotationUpgradeTarget])
	}

	// The guard can be skipped explicitly.

	statefulSet.Annotations[AnnotationGuard] = GuardSkip

	if error := kubeClient.Update(context.Background(), statefulSet); error != nil {
		t.Fatal(error)
	}

	reconcile(reconciler, t)

	if decision := getPod(kubeClient, 1, t).Annotations[AnnotationDecision]; decision != DecisionUpgrade {
		t.Fatalf("pod 1 decision = %q with the guard skipped", decision)
	}
}

func TestReconcileReplacesARestartedHolderWithAnOlderTemplate(t *testing.T) {
	reconciler, kubeClient := newReconciler(
		t, newUpgradingStatefulSet(), newRestartedHolder(1, "revision-old"),
	)

	reconcile(reconciler, t)

	pod := &corev1.Pod{}

	if error := kubeClient.Get(
		context.Background(),
		types.NamespacedName{Name: testName + "-0", Namespace: testNamespace},
		pod,
	); !errors.IsNotFound(error) {
		t.Fatalf("holder with the older template was not deleted: %v", error)
	}

	if restarts := getStatefulSet(kubeClient, t).Annotations[AnnotationUpgradeRestarts]; restarts != "" {
		t.Fatalf("restarts = %q after the holder was replaced", restarts)
	}
}

func TestReconcileReportsEachRestartOfTheHolder(t *testing.T) {
	reconciler, kubeClient := newReconciler(
		t, newUpgradingStatefulSet(), newRestartedHolder(2, "revision-new"),
	)

	recorder := record.NewFakeRecorder(10)

	reconciler.Recorder = recorder

	reconcile(reconciler, t)
	reconcile(reconciler, t)

	if count := countEvents("UpgradeHolderRestarted", recorder); count != 1 {
		t.Fatalf("%d UpgradeHolderRestarted events for 2 reconciles of the same restart count", count)
	}

	statefulSet := getStatefulSet(kubeClient, t)

	if restarts := statefulSet.Annotations[AnnotationUpgradeRestarts]; restarts != "2" {
		t.Fatalf("restarts = %q", restarts)
	}

	if message := statefulSet.Annotations[AnnotationUpgradeMessage]; !strings.Contains(message, "restarted 2 times") {
		t.Fatalf("message = %q", message)
	}

	if result := statefulSet.Annotations[AnnotationUpgradeResult]; result != "" {
		t.Fatalf("result = %q while the holder only restarted", result)
	}

	getPod(kubeClient, 0, t)
}

func TestReconcileRunsAFailedDatabaseJobAgain(t *testing.T) {
	statefulSet := newStatefulSet(testNewImage, testOldImage)

	reconciler, kubeClient := newReconciler(
		t, statefulSet, newPod(testNewImage, 0, true),
		newPod(testNewImage, 1, false),
	)

	recorder := record.NewFakeRecorder(10)

	reconciler.Recorder = recorder

	reconcile(reconciler, t)

	completeStatefulSetJob(DatabaseJobPrecheck, kubeClient, testReleaseOld, t)

	reconcile(reconciler, t)

	attempt := int(getStatefulSet(kubeClient, t).Generation)

	finishDatabaseJob(
		attempt, 2, DatabaseJobPostcheck, kubeClient,
		"FATAL:  remaining connection slots are reserved for roles with the SUPERUSER attribute",
		t,
	)

	reconcile(reconciler, t)

	if result := getStatefulSet(kubeClient, t).Annotations[AnnotationUpgradeResult]; result != "" {
		t.Fatalf("result = %q after a failed postcheck job", result)
	}

	found := false

	for len(recorder.Events) > 0 {
		event := <-recorder.Events

		if strings.Contains(event, "DatabaseJobFailed") &&
			strings.Contains(event, "remaining connection slots") {

			found = true
		}
	}

	if !found {
		t.Fatal("no DatabaseJobFailed event for the failed postcheck job")
	}

	reconcile(reconciler, t)

	completeStatefulSetJob(DatabaseJobPostcheck, kubeClient, testReleaseNew, t)

	reconcile(reconciler, t)

	if result := getStatefulSet(kubeClient, t).Annotations[AnnotationUpgradeResult]; result != ResultSucceeded {
		t.Fatalf("result = %q after the postcheck job ran again", result)
	}
}

func TestReconcileStartsPodsOnTheSchemaVersion(t *testing.T) {
	statefulSet := newStatefulSet(testOldImage, testOldImage)

	reconciler, kubeClient := newReconciler(
		t, statefulSet, newPod(testOldImage, 0, false),
		newPod(testOldImage, 1, false),
	)

	reconcile(reconciler, t)

	for _, ordinal := range []int{0, 1} {
		pod := getPod(kubeClient, ordinal, t)

		if decision := pod.Annotations[AnnotationDecision]; decision != DecisionStart {
			t.Fatalf("pod %d decision = %q, want %q", ordinal, decision, DecisionStart)
		}

		if image := pod.Annotations[AnnotationGateImage]; image != testOldImage {
			t.Fatalf("pod %d gate image = %q, want %q", ordinal, image, testOldImage)
		}
	}
}

func TestReconcileUpgradesWithOneHolder(t *testing.T) {
	statefulSet := newStatefulSet(testNewImage, testOldImage)

	reconciler, kubeClient := newReconciler(
		t, statefulSet, newPod(testOldImage, 0, true),
		newPod(testNewImage, 1, false),
	)

	reconcile(reconciler, t)

	if decision := getPod(kubeClient, 1, t).Annotations[AnnotationDecision]; decision != "" {
		t.Fatalf("pod 1 decision = %q before the precheck", decision)
	}

	completeStatefulSetJob(DatabaseJobPrecheck, kubeClient, testReleaseOld, t)

	reconcile(reconciler, t)

	if error := kubeClient.Get(
		context.Background(),
		types.NamespacedName{Name: testName + "-0", Namespace: testNamespace},
		&corev1.Pod{},
	); error == nil {
		t.Fatal("pod 0 on the previous version was not deleted")
	}

	if decision := getPod(kubeClient, 1, t).Annotations[AnnotationDecision]; decision != DecisionUpgrade {
		t.Fatalf("pod 1 decision = %q, want %q", decision, DecisionUpgrade)
	}

	if holder := getStatefulSet(kubeClient, t).Annotations[AnnotationUpgradeHolder]; holder != testName+"-1" {
		t.Fatalf("holder = %q", holder)
	}

	// The StatefulSet controller recreates pod 0 on the new version. It
	// waits while pod 1 upgrades.

	createPod(kubeClient, newPod(testNewImage, 0, false), t)

	reconcile(reconciler, t)

	if decision := getPod(kubeClient, 0, t).Annotations[AnnotationDecision]; decision != "" {
		t.Fatalf("pod 0 decision = %q during the upgrade", decision)
	}

	setReady(kubeClient, 1, t)

	reconcile(reconciler, t)

	completeStatefulSetJob(DatabaseJobPostcheck, kubeClient, testReleaseNew, t)

	reconcile(reconciler, t)

	statefulSet = getStatefulSet(kubeClient, t)

	if version := statefulSet.Annotations[AnnotationSchemaVersion]; version != testNewImage {
		t.Fatalf("schema version = %q, want %q", version, testNewImage)
	}

	if statefulSet.Annotations[AnnotationUpgradeHolder] != "" ||
		statefulSet.Annotations[AnnotationUpgradeTarget] != "" {

		t.Fatalf("upgrade annotations were kept: %v", statefulSet.Annotations)
	}

	if decision := getPod(kubeClient, 0, t).Annotations[AnnotationDecision]; decision != DecisionStart {
		t.Fatalf("pod 0 decision = %q, want %q", decision, DecisionStart)
	}
}

func TestReconcileWaitsForATerminatingHolder(t *testing.T) {
	statefulSet := newStatefulSet(testNewImage, testOldImage)

	statefulSet.Annotations[AnnotationUpgradeHolder] = testName + "-1"
	statefulSet.Annotations[AnnotationUpgradeTarget] = testNewImage

	holder := newPod(testNewImage, 1, false)

	holder.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	holder.Finalizers = []string{"test"}

	reconciler, kubeClient := newReconciler(
		t, statefulSet, newPod(testNewImage, 0, false), holder,
	)

	reconcile(reconciler, t)

	if decision := getPod(kubeClient, 0, t).Annotations[AnnotationDecision]; decision != "" {
		t.Fatalf("pod 0 decision = %q while holder pod 1 terminates", decision)
	}
}

func completeStatefulSetJob(
	kind string,
	kubeClient client.Client,
	message string,
	t *testing.T,
) {
	completeDatabaseJob(
		int(getStatefulSet(kubeClient, t).Generation), kind, kubeClient, message,
		t,
	)
}

func countEvents(reason string, recorder *record.FakeRecorder) int {
	count := 0

	for len(recorder.Events) > 0 {
		if strings.Contains(<-recorder.Events, reason) {
			count++
		}
	}

	return count
}

func createPod(kubeClient client.Client, pod *corev1.Pod, t *testing.T) {
	if error := kubeClient.Create(context.Background(), pod); error != nil {
		t.Fatal(error)
	}
}

func getPod(kubeClient client.Client, ordinal int, t *testing.T) *corev1.Pod {
	pod := &corev1.Pod{}

	if error := kubeClient.Get(
		context.Background(),
		types.NamespacedName{
			Name:      testName + "-" + string(rune('0'+ordinal)),
			Namespace: testNamespace,
		},
		pod,
	); error != nil {
		t.Fatal(error)
	}

	return pod
}

func getStatefulSet(kubeClient client.Client, t *testing.T) *appsv1.StatefulSet {
	statefulSet := &appsv1.StatefulSet{}

	if error := kubeClient.Get(
		context.Background(),
		types.NamespacedName{Name: testName, Namespace: testNamespace},
		statefulSet,
	); error != nil {
		t.Fatal(error)
	}

	return statefulSet
}

func newPod(image string, ordinal int, ready bool) *corev1.Pod {
	status := corev1.ConditionFalse

	if ready {
		status = corev1.ConditionTrue
	}

	controller := true

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels:    map[string]string{"app": testName},
			Name:      testName + "-" + string(rune('0'+ordinal)),
			Namespace: testNamespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Controller: &controller,
					Kind:       "StatefulSet",
					Name:       testName,
					UID:        "statefulset-uid",
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Image: image, Name: testName}},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Status: status, Type: corev1.PodReady},
			},
		},
	}
}

func newReconciler(
	t *testing.T,
	objects ...client.Object,
) (*StatefulSetReconciler, client.Client) {
	scheme := runtime.NewScheme()

	if error := clientgoscheme.AddToScheme(scheme); error != nil {
		t.Fatal(error)
	}

	kubeClient := fake.NewClientBuilder().WithScheme(
		scheme,
	).WithObjects(
		objects...,
	).Build()

	return &StatefulSetReconciler{
		APIReader: kubeClient,
		Client:    kubeClient,
		LogReader: &fakeLogReader{result: "success", upgradeType: "major"},
		Recorder:  record.NewFakeRecorder(10),
		ReleaseCatalog: fakeReleaseCatalog{
			testDowngradeImage: "38.2.10",
			testNewImage:       "38.7.9",
			testOldImage:       "38.2.12",
		},
	}, kubeClient
}

func newRestartedHolder(restarts int32, revision string) *corev1.Pod {
	pod := newPod(testNewImage, 0, false)

	pod.Labels[appsv1.ControllerRevisionHashLabelKey] = revision
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			LastTerminationState: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 0, Reason: "Completed",
				},
			},
			Name:         testName,
			RestartCount: restarts,
		},
	}
	pod.UID = "holder-uid"

	return pod
}

func newStatefulSet(image string, schemaVersion string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				AnnotationGate:          "true",
				AnnotationSchemaVersion: schemaVersion,
			},
			Name:      testName,
			Namespace: testNamespace,
			UID:       "statefulset-uid",
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": testName},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Image: image, Name: testName}},
				},
			},
		},
	}
}

func newUpgradingStatefulSet() *appsv1.StatefulSet {
	statefulSet := newStatefulSet(testNewImage, testOldImage)

	statefulSet.Annotations[AnnotationUpgradeHolder] = testName + "-0"
	statefulSet.Annotations[AnnotationUpgradeTarget] = testNewImage
	statefulSet.Status.UpdateRevision = "revision-new"

	return statefulSet
}

func reconcile(reconciler *StatefulSetReconciler, t *testing.T) {
	if _, error := reconciler.Reconcile(
		context.Background(),
		controllerruntime.Request{
			NamespacedName: types.NamespacedName{
				Name: testName, Namespace: testNamespace,
			},
		},
	); error != nil {
		t.Fatal(error)
	}
}

func setReady(kubeClient client.Client, ordinal int, t *testing.T) {
	pod := getPod(kubeClient, ordinal, t)

	pod.Status.Conditions[0].Status = corev1.ConditionTrue

	if error := kubeClient.Status().Update(context.Background(), pod); error != nil {
		t.Fatal(error)
	}
}

type fakeLogReader struct {
	result      string
	upgradeType string
}

type fakeReleaseCatalog map[string]string

type fakeUpgradeChecker string
