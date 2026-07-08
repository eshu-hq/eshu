// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

func mergeContractPayload(payload map[string]any, encode func() (map[string]any, error)) error {
	encoded, err := encode()
	if err != nil {
		return err
	}
	for key, value := range encoded {
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
	return nil
}

func stringPtr(value string) *string {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func optionalStringPtrFromPayload(payload map[string]any, key string) *string {
	value, ok := payload[key].(string)
	if !ok {
		return nil
	}
	return &value
}
