package provisioning

import (
	"strings"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	testCases := map[string]struct {
		assertContains    string
		assertNotContains string
		payload         string
	}{
		"activationCode is redacted": {
			assertContains:    `"activationCode":"[REDACTED]"`,
			assertNotContains: "one-time-secret",
			payload: `{
				"activationCode": "one-time-secret",
				"environmentID": "env-1"
			}`,
		},
		"manifest request is not redacted": {
			assertContains:    `"dxpVersion": "2026.q3.0"`,
			assertNotContains: "[REDACTED]",
			payload: `{
				"dxpVersion": "2026.q3.0",
				"environmentID": "env-1"
			}`,
		},
		"manifest response redacts licenseXML": {
			assertContains:    `"licenseXML": "[REDACTED]"`,
			assertNotContains: "c2lnbmVkLWxpY2Vuc2U=",
			payload: `{
				"licenseXML": "c2lnbmVkLWxpY2Vuc2U=",
				"maxClusterNodes": 3
			}`,
		},
		"non-JSON payload is returned as is": {
			assertContains:    "not-a-json-token",
			assertNotContains: "[REDACTED]",
			payload:         "not-a-json-token",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			redacted := redactSensitive(testCase.payload)

			if !strings.Contains(redacted, testCase.assertContains) {
				t.Errorf(
					"redactSensitive = %q, want it to contain %q",
					redacted, testCase.assertContains,
				)
			}

			if strings.Contains(redacted, testCase.assertNotContains) {
				t.Errorf(
					"redactSensitive = %q, want it to not contain %q",
					redacted, testCase.assertNotContains,
				)
			}
		})
	}
}