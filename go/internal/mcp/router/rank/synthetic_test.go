// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// vocabulary is a fixed word list for the synthetic registry.
var vocabulary = strings.Fields(`alpha beta gamma delta zebra repository service workload
deployment environment symbol function class definition cloud resource package registry
pipeline commit artifact image drift owner contract schema queue lease graph content
dependency relationship evidence freshness generation status incident vulnerability
advisory license secret scope account tenant cluster namespace cost latency budget`)

// synthetic builds n deterministic tool definitions and catalog entries with a
// realistic shape: 2-4 name tokens, a 30-45 word description with a sentence
// shared by every ninth tool, 3 intents, and a schema with 2-4 properties.
func synthetic(n int) ([]toolcontract.ToolDefinition, map[string]catalog.Entry) {
	seed := uint32(2463534242)
	next := func(bound int) int {
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		return int(seed % uint32(bound))
	}
	words := func(count int) []string {
		out := make([]string, count)
		for i := range out {
			out[i] = vocabulary[next(len(vocabulary))]
		}
		return out
	}
	tools := make([]toolcontract.ToolDefinition, 0, n)
	entries := make(map[string]catalog.Entry, n)
	props := []string{"repo_id", "service_id", "workload_id", "environment", "limit", "offset", "query", "scope_id"}
	costs := []catalog.CostClass{catalog.CostLow, catalog.CostModerate, catalog.CostHigh}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%s_%03d", strings.Join(words(2+next(3)), "_"), i)
		desc := strings.Join(words(30+next(16)), " ") + "."
		if i%9 == 0 {
			desc += " Scoped tokens receive only granted repositories; an ungranted repository selector is rejected."
		}
		properties := map[string]any{}
		for j := 0; j < 2+next(3); j++ {
			properties[props[next(len(props))]] = map[string]any{"type": "string"}
		}
		tools = append(tools, toolcontract.ToolDefinition{
			Name:        name,
			Description: desc,
			InputSchema: map[string]any{"type": "object", "properties": properties},
		})
		entries[name] = catalog.Entry{
			Name:      name,
			Cost:      costs[next(len(costs))],
			Canonical: i%17 == 0,
			Intents:   []string{strings.Join(words(5), " "), strings.Join(words(4), " "), strings.Join(words(6), " ")},
		}
	}
	return tools, entries
}
