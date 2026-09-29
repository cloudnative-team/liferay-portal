package cx

import (
	"encoding/json"
	"errors"
	"fmt"
)

func BuildPayload(configs []string) (string, error) {
	if len(configs) == 0 {
		return "", errors.New("spec.configs is empty, so there is no configuration to deliver")
	}

	merged := map[string]json.RawMessage{}

	for index, config := range configs {
		var entries map[string]json.RawMessage

		if error := json.Unmarshal([]byte(config), &entries); error != nil {
			return "", fmt.Errorf("spec.configs[%d] is not a JSON object: %w", index, error)
		}

		for pid, entry := range entries {
			if _, ok := merged[pid]; ok {
				return "", fmt.Errorf("spec.configs[%d] repeats the configuration %q", index, pid)
			}

			merged[pid] = entry
		}
	}

	encoded, error := json.MarshalIndent(merged, "", "\t")

	if error != nil {
		return "", error
	}

	return string(encoded), nil
}
