// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseSecurityAlertTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func securityAlertInt64(payload map[string]any, key string) int64 {
	raw := strings.TrimSpace(fmt.Sprint(payload[key]))
	if raw == "" || raw == "<nil>" {
		return 0
	}
	value, _ := strconv.ParseInt(raw, 10, 64)
	return value
}

func securityAlertMap(payload map[string]any, key string) map[string]any {
	raw, ok := payload[key].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func securityAlertStringMap(payload map[string]any, key string) map[string]string {
	raw, ok := payload[key].(map[string]string)
	if ok {
		return cloneSecurityAlertStringMap(raw)
	}
	anyMap, ok := payload[key].(map[string]any)
	if !ok || len(anyMap) == 0 {
		return nil
	}
	out := make(map[string]string, len(anyMap))
	for key, value := range anyMap {
		text := strings.TrimSpace(fmt.Sprint(value))
		if strings.TrimSpace(key) != "" && text != "" && text != "<nil>" {
			out[key] = text
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneSecurityAlertStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func securityAlertStringMapSlice(payload map[string]any, key string) []map[string]string {
	switch raw := payload[key].(type) {
	case []map[string]string:
		out := make([]map[string]string, 0, len(raw))
		for _, item := range raw {
			if cloned := cloneSecurityAlertStringMap(item); len(cloned) > 0 {
				out = append(out, cloned)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]map[string]string, 0, len(raw))
		for _, item := range raw {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			converted := make(map[string]string, len(row))
			for key, value := range row {
				text := strings.TrimSpace(fmt.Sprint(value))
				if strings.TrimSpace(key) != "" && text != "" && text != "<nil>" {
					converted[key] = text
				}
			}
			if len(converted) > 0 {
				out = append(out, converted)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}
