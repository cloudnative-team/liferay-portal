package schema

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	record "k8s.io/client-go/tools/record"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	interceptor "sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestSetAnnotationsRecordsEventsOnlyAfterTheUpdate(t *testing.T) {
	_, kubeClient := newReconciler(t, newStatefulSet(testNewImage, ""))

	conflict := true
	recorder := record.NewFakeRecorder(10)

	reconciler := &StatefulSetReconciler{
		Client: interceptor.NewClient(
			kubeClient.(client.WithWatch),
			interceptor.Funcs{
				Update: func(
					context context.Context, kubeClient client.WithWatch,
					object client.Object, options ...client.UpdateOption,
				) error {
					if conflict {
						return apierrors.NewConflict(
							schema.GroupResource{Group: "apps", Resource: "statefulsets"},
							object.GetName(), nil,
						)
					}

					return kubeClient.Update(context, object, options...)
				},
			},
		),
		Recorder: recorder,
	}

	update := func() error {
		statefulSet := &appsv1.StatefulSet{}

		if error := kubeClient.Get(
			context.Background(),
			client.ObjectKey{Name: testName, Namespace: testNamespace},
			statefulSet,
		); error != nil {
			t.Fatal(error)
		}

		return reconciler.setAnnotations(
			context.Background(), statefulSet,
			map[string]string{AnnotationInitState: InitStateResetting},
			event{corev1.EventTypeWarning, "Interrupted.", "InitializationInterrupted"},
		)
	}

	if error := update(); !apierrors.IsConflict(error) {
		t.Fatalf("error = %v", error)
	}

	if len(recorder.Events) != 0 {
		t.Fatalf("event recorded after a conflict: %s", <-recorder.Events)
	}

	conflict = false

	if error := update(); error != nil {
		t.Fatal(error)
	}

	if len(recorder.Events) != 1 {
		t.Fatalf("events = %d", len(recorder.Events))
	}
}
