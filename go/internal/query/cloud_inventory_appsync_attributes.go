// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"
	"time"
)

const cloudInventoryAppSyncKeyLifecyclesAttributeKey = "api_key_lifecycles"

// cloudInventoryAppSyncKeyLifecycleAttributes keeps only expiry and deletion
// timestamps from the AppSync lifecycle summary. It drops identifiers,
// descriptions, and every other nested value even if malformed input contains
// them.
func cloudInventoryAppSyncKeyLifecycleAttributes(raw any) []map[string]string {
	elements, ok := raw.([]any)
	if !ok || len(elements) == 0 {
		return nil
	}
	type lifecycle struct {
		attributes map[string]string
		expires    time.Time
		deletes    time.Time
	}
	ordered := make([]lifecycle, 0, len(elements))
	for _, element := range elements {
		item, ok := element.(map[string]any)
		if !ok {
			continue
		}
		attributes := map[string]string{}
		var expires, deletes time.Time
		for _, key := range []string{"expires", "deletes"} {
			value, ok := item[key].(string)
			if !ok || strings.TrimSpace(value) == "" {
				continue
			}
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				continue
			}
			attributes[key] = parsed.UTC().Format(time.RFC3339)
			if key == "expires" {
				expires = parsed
			} else {
				deletes = parsed
			}
		}
		if len(attributes) == 0 {
			continue
		}
		ordered = append(ordered, lifecycle{attributes: attributes, expires: expires, deletes: deletes})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if !ordered[i].expires.Equal(ordered[j].expires) {
			return ordered[i].expires.Before(ordered[j].expires)
		}
		return ordered[i].deletes.Before(ordered[j].deletes)
	})
	if len(ordered) == 0 {
		return nil
	}
	result := make([]map[string]string, 0, len(ordered))
	for _, item := range ordered {
		result = append(result, item.attributes)
	}
	return result
}
