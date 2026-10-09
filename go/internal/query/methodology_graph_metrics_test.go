// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"encoding/json"
	"strings"
	"testing"

	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type methodologyMetricPlan struct {
	operator               string
	args                   map[string]any
	hits, rows             int64
	cacheHits, cacheMisses int64
	ratio                  float64
	time                   int64
	children               []neo4j.ProfiledPlan
}

func (p methodologyMetricPlan) Operator() string               { return p.operator }
func (p methodologyMetricPlan) Arguments() map[string]any      { return p.args }
func (p methodologyMetricPlan) Identifiers() []string          { return nil }
func (p methodologyMetricPlan) DbHits() int64                  { return p.hits }
func (p methodologyMetricPlan) Records() int64                 { return p.rows }
func (p methodologyMetricPlan) Children() []neo4j.ProfiledPlan { return p.children }
func (p methodologyMetricPlan) PageCacheMisses() int64         { return p.cacheMisses }
func (p methodologyMetricPlan) PageCacheHits() int64           { return p.cacheHits }
func (p methodologyMetricPlan) PageCacheHitRatio() float64     { return p.ratio }
func (p methodologyMetricPlan) Time() int64                    { return p.time }

func TestMethodologyProfileMetricsRequireRawCounters(t *testing.T) {
	valid := methodologyMetricPlan{operator: "ProduceResults@neo4j", args: map[string]any{"DbHits": int64(0), "Rows": int64(0), "PageCacheHits": int64(0), "PageCacheMisses": int64(0)}, hits: 0, rows: 0}
	if err := methodologyValidateProfileCounters(valid, 0); err != nil {
		t.Fatalf("legitimate zero counters rejected: %v", err)
	}
	for _, tc := range []struct {
		name         string
		plan         methodologyMetricPlan
		capturedRows int
	}{
		{"missing db hits", methodologyMetricPlan{operator: valid.operator, args: map[string]any{"Rows": int64(0)}}, 0},
		{"missing cache hits", methodologyMetricPlan{operator: valid.operator, args: map[string]any{"DbHits": int64(0), "Rows": int64(0), "PageCacheMisses": int64(0)}}, 0},
		{"wrong type", methodologyMetricPlan{operator: valid.operator, args: map[string]any{"DbHits": "0", "Rows": int64(0)}}, 0},
		{"hydrated mismatch", methodologyMetricPlan{operator: valid.operator, args: map[string]any{"DbHits": int64(4), "Rows": int64(0)}}, 0},
		{"captured row mismatch", valid, 1},
		{"unnamed operator", methodologyMetricPlan{args: valid.args}, 0},
		{"missing child counter", methodologyMetricPlan{operator: valid.operator, args: valid.args, children: []neo4j.ProfiledPlan{methodologyMetricPlan{operator: "Filter@neo4j", args: map[string]any{"Rows": int64(0)}}}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := methodologyValidateProfileCounters(tc.plan, tc.capturedRows); err == nil || !strings.Contains(err.Error(), "PROFILE") {
				t.Fatalf("accepted invalid PROFILE: %v", err)
			}
		})
	}
}

func TestMethodologyPlanOmitsUnavailableOptionalMetrics(t *testing.T) {
	plan := methodologyMetricPlan{operator: "ProduceResults@neo4j", args: map[string]any{"DbHits": int64(0), "Rows": int64(0), "PageCacheHits": int64(0), "PageCacheMisses": int64(0)}}
	check := func(t *testing.T, plan methodologyMetricPlan, wantOptional bool) {
		t.Helper()
		encoded, err := json.Marshal(methodologyPlanTree(plan))
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"time_raw", "page_cache_hit_ratio"} {
			value, present := fields[key]
			if present != wantOptional || present && value != float64(0) {
				t.Fatalf("%s present=%v value=%v, want optional=%v", key, present, value, wantOptional)
			}
		}
	}
	check(t, plan, false)
	plan.args["Time"] = int64(0)
	plan.args["PageCacheHitRatio"] = float64(0)
	check(t, plan, true)
}
