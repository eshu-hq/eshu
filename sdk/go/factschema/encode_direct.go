// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import "time"

func addStringValue(payload map[string]any, key string, value string) {
	if value != "" {
		payload[key] = value
	}
}

func addStringPtr(payload map[string]any, key string, value *string) {
	if value != nil {
		payload[key] = *value
	}
}

func addBoolPtr(payload map[string]any, key string, value *bool) {
	if value != nil {
		payload[key] = *value
	}
}

func addIntPtr(payload map[string]any, key string, value *int) {
	if value != nil {
		payload[key] = *value
	}
}

func addInt32Ptr(payload map[string]any, key string, value *int32) {
	if value != nil {
		payload[key] = *value
	}
}

func addInt64Ptr(payload map[string]any, key string, value *int64) {
	if value != nil {
		payload[key] = *value
	}
}

func addTimePtr(payload map[string]any, key string, value *time.Time) {
	if value != nil {
		payload[key] = value.UTC()
	}
}

func addStringSlice(payload map[string]any, key string, value []string) {
	if value != nil {
		payload[key] = value
	}
}

func addAnyMap(payload map[string]any, key string, value map[string]any) {
	if value != nil {
		payload[key] = value
	}
}

func addStringMapPtr(payload map[string]any, key string, value *map[string]string) {
	if value != nil {
		payload[key] = *value
	}
}

func addStringMap(payload map[string]any, key string, value map[string]string) {
	if value != nil {
		payload[key] = value
	}
}
