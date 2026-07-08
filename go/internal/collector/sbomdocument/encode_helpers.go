// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomdocument

func mergeContractPayloadNoError(payload map[string]any, encode func() (map[string]any, error)) {
	encoded, err := encode()
	if err != nil {
		return
	}
	for key, value := range encoded {
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
}

func stringPtr(value string) *string {
	return &value
}

func intPtr(value int) *int {
	return &value
}
