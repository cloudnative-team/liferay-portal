package cx

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildPayloadIsStable(t *testing.T) {
	configs := []string{`{"b~baker": {"z": 1, "a": 2}}`, `{"a~able": {}}`}

	first, error := BuildPayload(configs)

	if error != nil {
		t.Fatal(error)
	}

	for range 10 {
		if again, _ := BuildPayload(configs); again != first {
			t.Fatalf("Expected the same payload on every build, got %s and %s", first, again)
		}
	}
}

func TestBuildPayloadMergesConfigsVerbatim(t *testing.T) {
	payload, error := BuildPayload([]string{
		`{"CETConfiguration~able": {"name": "Able"}}`,
		`{"OAuth2ProviderApplicationUserAgentConfiguration~able-oaua": {"expiration": 12345678901234567890}}`,
	})

	if error != nil {
		t.Fatalf("BuildPayload() error = %v, want nil", error)
	}

	var merged map[string]json.RawMessage

	if error := json.Unmarshal([]byte(payload), &merged); error != nil {
		t.Fatalf("Expected the payload to be a JSON object, got %v", error)
	}

	if len(merged) != 2 {
		t.Errorf("Expected 2 configurations, got %d in %s", len(merged), payload)
	}

	if !strings.Contains(payload, "12345678901234567890") {
		t.Errorf("Expected a large integer to survive verbatim, got %s", payload)
	}
}

func TestBuildPayloadRejectsConfigsItCannotDeliver(t *testing.T) {
	testCases := map[string]struct {
		configs     []string
		wantMessage string
	}{
		"a config that is not a JSON object": {
			configs:     []string{`{"CETConfiguration~able": {}}`, `["CETConfiguration~baker"]`},
			wantMessage: "spec.configs[1] is not a JSON object",
		},
		"a configuration repeated across configs": {
			configs:     []string{`{"CETConfiguration~able": {}}`, `{"CETConfiguration~able": {"name": "Other"}}`},
			wantMessage: `spec.configs[1] repeats the configuration "CETConfiguration~able"`,
		},
		"no configs": {
			wantMessage: "spec.configs is empty",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			_, error := BuildPayload(testCase.configs)

			if error == nil {
				t.Fatal("BuildPayload() error = nil, want an error")
			}

			if !strings.Contains(error.Error(), testCase.wantMessage) {
				t.Errorf("BuildPayload() error = %q, want it to contain %q", error.Error(), testCase.wantMessage)
			}
		})
	}
}
