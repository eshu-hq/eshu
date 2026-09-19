// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

type workReportRoute struct {
	Route string         `json:"route"`
	P95MS float64        `json:"p95_ms"`
	Work  workReportWork `json:"work"`
}

type workReportWork struct {
	Calls float64 `json:"calls"`
	Rows  float64 `json:"rows"`
	Blks  float64 `json:"blks"`
}

// WriteWorkReport writes the per-request work every metered route measured, as
// JSON sorted by route. scripts/refresh-read-api-work-budgets.sh renders the
// committed work budget table from GREEN runs' reports; budgets are never
// derived from a hand-picked number.
func WriteWorkReport(w io.Writer, results []RouteLatency) error {
	routes := make([]workReportRoute, 0, len(results))
	for _, r := range results {
		if !r.Exercised || !r.Metered {
			continue
		}
		routes = append(routes, workReportRoute{
			Route: r.Route,
			P95MS: float64(r.P95) / 1e6,
			Work:  workReportWork{Calls: r.Work.Calls, Rows: r.Work.Rows, Blks: r.Work.Blks},
		})
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Route < routes[j].Route })

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"routes": routes}); err != nil {
		return fmt.Errorf("encode work report: %w", err)
	}
	return nil
}
