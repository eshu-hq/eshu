// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"slices"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
)

func indexExpectedPropertyKeys(expected []materializededges.ExpectedEdge) (map[string][]string, error) {
	indexed := make(map[string][]string)
	for _, edge := range expected {
		base := edge
		base.Properties = nil
		baseKey := base.Key()
		keys := make([]string, 0, len(edge.Properties))
		for key := range edge.Properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if prior, ok := indexed[baseKey]; ok && !slices.Equal(prior, keys) {
			return nil, fmt.Errorf("expected edge %s repeats one MERGE identity with inconsistent asserted property keys", expectedEdgeLabel(base))
		}
		indexed[baseKey] = keys
	}
	return indexed, nil
}
