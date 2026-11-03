package main

import (
	"maps"
	"slices"
	"testing"

	env "github.com/caarlos0/env/v11"
	corev1 "k8s.io/api/core/v1"
)

func TestNewManagerOptionsWatchesNamespaces(t *testing.T) {
	testCases := map[string]struct {
		wantNamespaces  []string
		watchNamespaces string
	}{
		"a list of namespaces is watched": {
			wantNamespaces:  []string{"able", "baker"},
			watchNamespaces: "able,baker",
		},
		"every namespace is watched by default": {},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("WATCH_NAMESPACES", testCase.watchNamespaces)

			config, error := env.ParseAs[config]()

			if error != nil {
				t.Fatal(error)
			}

			if !slices.Equal(config.WatchNamespaces, testCase.wantNamespaces) {
				t.Errorf("WatchNamespaces = %v, want %v", config.WatchNamespaces, testCase.wantNamespaces)
			}

			managerOptions, error := newManagerOptions(config)

			if error != nil {
				t.Fatalf("newManagerOptions() error = %v, want nil", error)
			}

			defaultNamespaces := slices.Sorted(maps.Keys(managerOptions.Cache.DefaultNamespaces))

			if !slices.Equal(defaultNamespaces, testCase.wantNamespaces) {
				t.Errorf("DefaultNamespaces = %v, want %v", defaultNamespaces, testCase.wantNamespaces)
			}

			if len(managerOptions.Cache.ByObject) != 1 {
				t.Errorf("ByObject = %v, want only the ConfigMap label filter", managerOptions.Cache.ByObject)
			}

			for object, byObject := range managerOptions.Cache.ByObject {
				if _, ok := object.(*corev1.ConfigMap); !ok || (byObject.Label == nil) {
					t.Errorf("ByObject[%T] = %v, want a ConfigMap label filter", object, byObject)
				}
			}

			if (managerOptions.Client.Cache == nil) || (len(managerOptions.Client.Cache.DisableFor) != 1) {
				t.Fatalf("Client.Cache = %v, want Namespaces read from the API server", managerOptions.Client.Cache)
			}

			if _, ok := managerOptions.Client.Cache.DisableFor[0].(*corev1.Namespace); !ok {
				t.Errorf("DisableFor = %T, want *corev1.Namespace", managerOptions.Client.Cache.DisableFor[0])
			}
		})
	}
}
