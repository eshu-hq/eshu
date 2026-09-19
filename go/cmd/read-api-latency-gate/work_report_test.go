// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestWriteWorkReportEmitsOnlyMeteredRoutesSortedByRoute(t *testing.T) {
	results := []RouteLatency{
		{Route: "GET /b", Exercised: true, Metered: true, P95: 1500 * time.Microsecond, Work: WorkPerRequest{Calls: 2, Rows: 3, Blks: 4}},
		{Route: "GET /skipped", Exercised: false, Status: 400},
		{Route: "GET /unmetered", Exercised: true},
		{Route: "GET /a", Exercised: true, Metered: true, Work: WorkPerRequest{Calls: 1.5, Rows: 0, Blks: 1}},
	}

	var buf bytes.Buffer
	if err := WriteWorkReport(&buf, results); err != nil {
		t.Fatalf("WriteWorkReport: %v", err)
	}

	var got struct {
		Routes []struct {
			Route string  `json:"route"`
			P95MS float64 `json:"p95_ms"`
			Work  struct {
				Calls float64 `json:"calls"`
				Rows  float64 `json:"rows"`
				Blks  float64 `json:"blks"`
			} `json:"work"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("report is not valid JSON: %v\n%s", err, buf.String())
	}
	if len(got.Routes) != 2 || got.Routes[0].Route != "GET /a" || got.Routes[1].Route != "GET /b" {
		t.Fatalf("routes = %+v, want exactly the two metered routes, sorted", got.Routes)
	}
	if got.Routes[1].Work.Blks != 4 || got.Routes[1].P95MS != 1.5 {
		t.Errorf("GET /b = %+v, want blks 4 and p95_ms 1.5", got.Routes[1])
	}
}

func TestWriteWorkReportIsByteIdenticalAcrossRuns(t *testing.T) {
	results := []RouteLatency{
		{Route: "GET /b", Exercised: true, Metered: true, Work: WorkPerRequest{Calls: 2}},
		{Route: "GET /a", Exercised: true, Metered: true, Work: WorkPerRequest{Calls: 1}},
	}
	var first, second bytes.Buffer
	_ = WriteWorkReport(&first, results)
	_ = WriteWorkReport(&second, results)
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("WriteWorkReport is not deterministic")
	}
}
