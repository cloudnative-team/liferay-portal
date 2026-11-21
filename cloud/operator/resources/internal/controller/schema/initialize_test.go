package schema

import (
	"context"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	ptr "k8s.io/utils/ptr"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestInitializeAdoptsADatabaseWithTables(t *testing.T) {
	reconciler, kubeClient := newInitReconciler(t, newPod(testNewImage, 0, false))

	reconcile(reconciler, t)

	completeDatabaseJob(1, DatabaseJobProbe, kubeClient, testReleaseNew, t)

	reconcile(reconciler, t)

	statefulSet := getStatefulSet(kubeClient, t)

	if statefulSet.Annotations[AnnotationSchemaVersion] != testNewImage {
		t.Fatalf("schema version = %q", statefulSet.Annotations[AnnotationSchemaVersion])
	}

	if decision := getPod(kubeClient, 0, t).Annotations[AnnotationDecision]; decision != DecisionStart {
		t.Fatalf("pod 0 decision = %q", decision)
	}
}

func TestInitializeAdoptsAnOlderDatabaseForAnUpgrade(t *testing.T) {
	reconciler, kubeClient := newInitReconciler(t, newPod(testNewImage, 0, false))

	reconcile(reconciler, t)

	completeDatabaseJob(1, DatabaseJobProbe, kubeClient, testReleaseOld, t)

	reconcile(reconciler, t)

	if version := getStatefulSet(kubeClient, t).Annotations[AnnotationSchemaVersion]; version != SchemaVersionPrefix+"38.2.12" {
		t.Fatalf("schema version = %q", version)
	}

	// The next reconcile upgrades from the schema in Release_.

	reconcile(reconciler, t)

	completeStatefulSetJob(DatabaseJobPrecheck, kubeClient, testReleaseOld, t)

	reconcile(reconciler, t)

	if decision := getPod(kubeClient, 0, t).Annotations[AnnotationDecision]; decision != DecisionUpgrade {
		t.Fatalf("pod 0 decision = %q", decision)
	}
}

func TestInitializeRefusesAnIncompleteDatabase(t *testing.T) {
	for _, message := range []string{
		"tables=87\nportal=\nfailed=0\nfailedModules=",
		"tables=1200\nportal=38.7.9\nfailed=1\nfailedModules=com.liferay.a",
	} {
		reconciler, kubeClient := newInitReconciler(t, newPod(testNewImage, 0, false))

		reconcile(reconciler, t)

		completeDatabaseJob(1, DatabaseJobProbe, kubeClient, message, t)

		reconcile(reconciler, t)

		statefulSet := getStatefulSet(kubeClient, t)

		if statefulSet.Annotations[AnnotationInitState] != InitStateFailed {
			t.Fatalf("init state = %q for %q", statefulSet.Annotations[AnnotationInitState], message)
		}

		if statefulSet.Annotations[AnnotationSchemaVersion] != "" {
			t.Fatalf("schema version = %q for %q", statefulSet.Annotations[AnnotationSchemaVersion], message)
		}

		if decision := getPod(kubeClient, 0, t).Annotations[AnnotationDecision]; decision != "" {
			t.Fatalf("pod 0 decision = %q for %q", decision, message)
		}
	}
}

func TestInitializeResetsAndRetriesAfterTheHolderFails(t *testing.T) {
	reconciler, kubeClient := newInitReconciler(
		t, newPod(testNewImage, 0, false), newPod(testNewImage, 1, false),
	)

	reconcile(reconciler, t)

	completeDatabaseJob(1, DatabaseJobProbe, kubeClient, "tables=0", t)

	reconcile(reconciler, t)

	statefulSet := getStatefulSet(kubeClient, t)

	if statefulSet.Annotations[AnnotationInitState] != InitStateInitializing {
		t.Fatalf("init state = %q", statefulSet.Annotations[AnnotationInitState])
	}

	if decision := getPod(kubeClient, 0, t).Annotations[AnnotationDecision]; decision != DecisionInitialize {
		t.Fatalf("pod 0 decision = %q", decision)
	}

	if decision := getPod(kubeClient, 1, t).Annotations[AnnotationDecision]; decision != "" {
		t.Fatalf("pod 1 decision = %q during the initialization", decision)
	}

	// DXP in the holder is killed halfway through.

	pod := getPod(kubeClient, 0, t)

	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{Name: testName, RestartCount: 1},
	}

	if error := kubeClient.Status().Update(context.Background(), pod); error != nil {
		t.Fatal(error)
	}

	reconcile(reconciler, t)

	if error := kubeClient.Get(
		context.Background(),
		types.NamespacedName{Name: testName + "-0", Namespace: testNamespace},
		&corev1.Pod{},
	); error == nil {
		t.Fatal("the failed holder was not deleted")
	}

	if state := getStatefulSet(kubeClient, t).Annotations[AnnotationInitState]; state != InitStateResetting {
		t.Fatalf("init state = %q", state)
	}

	reconcile(reconciler, t)

	completeDatabaseJob(1, DatabaseJobReset, kubeClient, "tables=0", t)

	reconcile(reconciler, t)

	if state := getStatefulSet(kubeClient, t).Annotations[AnnotationInitState]; state != "" {
		t.Fatalf("init state = %q after the reset", state)
	}

	// A reconcile that still reads the resetting state reuses the finished
	// reset instead of emptying the database again.

	staleStatefulSet := getStatefulSet(kubeClient, t)

	staleStatefulSet.Annotations[AnnotationInitState] = InitStateResetting

	if _, error := reconciler.finishReset(context.Background(), staleStatefulSet); error != nil {
		t.Fatal(error)
	}

	jobList := &batchv1.JobList{}

	if error := kubeClient.List(
		context.Background(), jobList,
		client.MatchingLabels{labelDatabaseJob: DatabaseJobReset},
	); error != nil {
		t.Fatal(error)
	}

	if (len(jobList.Items) != 1) || (len(jobList.Items[0].Status.Conditions) == 0) {
		t.Fatalf("a stale reconcile created a new reset job: %v", jobList.Items)
	}

	// The StatefulSet controller recreates pod 0, the next probe finds the
	// database empty and the second attempt succeeds.

	createPod(kubeClient, newPod(testNewImage, 0, false), t)

	reconcile(reconciler, t)

	completeDatabaseJob(2, DatabaseJobProbe, kubeClient, "tables=0", t)

	reconcile(reconciler, t)

	statefulSet = getStatefulSet(kubeClient, t)

	if statefulSet.Annotations[AnnotationInitAttempts] != "2" {
		t.Fatalf("attempts = %q", statefulSet.Annotations[AnnotationInitAttempts])
	}

	setReady(kubeClient, 0, t)

	reconcile(reconciler, t)

	completeDatabaseJob(2, DatabaseJobVerify, kubeClient, testReleaseNew, t)

	reconcile(reconciler, t)

	statefulSet = getStatefulSet(kubeClient, t)

	if statefulSet.Annotations[AnnotationSchemaVersion] != testNewImage {
		t.Fatalf("schema version = %q", statefulSet.Annotations[AnnotationSchemaVersion])
	}

	if statefulSet.Annotations[AnnotationInitState] != "" {
		t.Fatalf("init state = %q", statefulSet.Annotations[AnnotationInitState])
	}

	if decision := getPod(kubeClient, 1, t).Annotations[AnnotationDecision]; decision != DecisionStart {
		t.Fatalf("pod 1 decision = %q", decision)
	}

	if error := kubeClient.List(context.Background(), jobList); error != nil {
		t.Fatal(error)
	}

	if len(jobList.Items) != 0 {
		t.Fatalf("%d database jobs after the initialization", len(jobList.Items))
	}
}

func completeDatabaseJob(
	attempt int,
	kind string,
	kubeClient client.Client,
	message string,
	t *testing.T,
) {
	finishDatabaseJob(attempt, 0, kind, kubeClient, message, t)
}

func finishDatabaseJob(
	attempt int,
	exitCode int32,
	kind string,
	kubeClient client.Client,
	message string,
	t *testing.T,
) {
	job := &batchv1.Job{}

	if error := kubeClient.Get(
		context.Background(),
		types.NamespacedName{
			Name:      fmt.Sprintf("%s-schema-%s-%d", testName, kind, attempt),
			Namespace: testNamespace,
		},
		job,
	); error != nil {
		t.Fatalf("%s job: %v", kind, error)
	}

	if job.UID == "" {
		jobsCount++

		job.UID = types.UID(fmt.Sprintf("job-uid-%d", jobsCount))

		if error := kubeClient.Update(context.Background(), job); error != nil {
			t.Fatal(error)
		}
	}

	conditionType := batchv1.JobComplete

	if exitCode != 0 {
		conditionType = batchv1.JobFailed
	}

	job.Status.Conditions = []batchv1.JobCondition{
		{Status: corev1.ConditionTrue, Type: conditionType},
	}

	if error := kubeClient.Status().Update(context.Background(), job); error != nil {
		t.Fatal(error)
	}

	createPod(
		kubeClient, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"batch.kubernetes.io/controller-uid": string(job.UID),
				},
				Name:      job.Name + "-" + string(job.UID),
				Namespace: testNamespace,
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "psql",
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: exitCode,
								Message:  message + "\n",
							},
						},
					},
				},
			},
		},
		t,
	)
}

func newInitReconciler(
	t *testing.T,
	pods ...*corev1.Pod,
) (*StatefulSetReconciler, client.Client) {
	statefulSet := newStatefulSet(testNewImage, "")

	delete(statefulSet.Annotations, AnnotationSchemaVersion)

	statefulSet.Spec.Template.Spec.InitContainers = []corev1.Container{
		{
			Name:          "cloud-sql-proxy",
			RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways),
		},
	}

	objects := []client.Object{statefulSet}

	for _, pod := range pods {
		objects = append(objects, pod)
	}

	reconciler, kubeClient := newReconciler(t, objects...)

	reconciler.APIReader = kubeClient
	reconciler.DatabaseClientImage = "postgres:17-alpine"

	return reconciler, kubeClient
}

var _ = appsv1.StatefulSet{}

var jobsCount int
