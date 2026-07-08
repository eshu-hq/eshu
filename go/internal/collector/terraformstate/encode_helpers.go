// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

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

func mergeContractPayloadNoError(payload map[string]any, encode func() (map[string]any, error)) {
	_ = mergeContractPayload(payload, encode)
}

func stringPtr(value string) *string {
	return &value
}

func optionalStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func payloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func typedCorrelationAnchors(anchors []any) []map[string]any {
	if len(anchors) == 0 {
		return nil
	}
	typed := make([]map[string]any, 0, len(anchors))
	for _, anchor := range anchors {
		mapped, ok := anchor.(map[string]any)
		if !ok {
			continue
		}
		typed = append(typed, mapped)
	}
	if len(typed) == 0 {
		return nil
	}
	return typed
}
