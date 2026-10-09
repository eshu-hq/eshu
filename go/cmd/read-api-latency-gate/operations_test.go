// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/mcp"
)

func TestSweepOperationsUsesMethodPathAndBody(t *testing.T) {
	seen := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v0/code/search" || string(body) != `{"query":"seed"}` {
			t.Errorf("request = %s %s %s", r.Method, r.URL.Path, body)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		seen++
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	results, err := SweepRoutes(SweepOptions{BaseURL: server.URL, APIKey: "key", Routes: []string{"POST /api/v0/code/search"}, Operations: map[string]Operation{"POST /api/v0/code/search": {Method: http.MethodPost, Path: "/api/v0/code/search", Body: `{"query":"seed"}`}}, Iterations: 1, Timeout: time.Second})
	if err != nil || len(results) != 1 || !results[0].Exercised || seen != warmupRequests+1 {
		t.Fatalf("results=%+v calls=%d err=%v", results, seen, err)
	}
}

func TestPilotOperationsAreImplementedInventoryRoutes(t *testing.T) {
	inventory, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		t.Fatal(err)
	}
	operations, err := PilotOperations(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != 3 {
		t.Fatalf("pilot operations = %d, want 3", len(operations))
	}
	if got := operations["GET /api/v0/status/ingesters/{ingester}"].Path; got != "/api/v0/status/ingesters/repository" {
		t.Fatalf("parameterized path = %q", got)
	}
}

func TestConcurrentPilotSweepOverlapsRequestsWithoutMeter(t *testing.T) {
	var active, peak int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&active, 1)
		for {
			old := atomic.LoadInt32(&peak)
			if n <= old || atomic.CompareAndSwapInt32(&peak, old, n) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	results, err := SweepConcurrentOperations(SweepOptions{BaseURL: server.URL, APIKey: "key", Timeout: time.Second}, map[string]Operation{"POST /pilot": {Method: http.MethodPost, Path: "/pilot", Body: `{}`}}, 4, 8)
	if err != nil || len(results) != 1 || len(results[0].Samples) != 8 || peak < 2 || results[0].Metered || results[0].Requested != 8 || results[0].Succeeded != 8 || results[0].PeakInFlight < 2 || len(results[0].Statuses) != 8 {
		t.Fatalf("results=%+v peak=%d err=%v", results, peak, err)
	}
}

func TestSweepOperationsUsesRealMCPTransport(t *testing.T) {
	queryMux := http.NewServeMux()
	queryMux.HandleFunc("GET /api/v0/index-status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"version":"test","status":"ready","repository_count":0,"queue":{}}`)
	})
	server := httptest.NewServer(mcp.NewServer(queryMux, slog.Default()).Handler(http.NewServeMux()))
	defer server.Close()
	op := Operation{Method: http.MethodPost, Path: "/mcp/message", Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_index_status","arguments":{}}}`, MCP: true, Expect: "mcp_index_status"}
	results, err := SweepRoutes(SweepOptions{BaseURL: server.URL, MCPBaseURL: server.URL, Routes: []string{"MCP get_index_status"}, Operations: map[string]Operation{"MCP get_index_status": op}, Iterations: 1, Timeout: time.Second})
	if err != nil || len(results) != 1 || !results[0].Exercised || results[0].HardFailed {
		t.Fatalf("real transport: results=%+v err=%v", results, err)
	}
}

func TestSweepOperationsRejectsMCPErrorEnvelopes(t *testing.T) {
	for _, response := range []string{
		`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"missing tool"}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"query failed"}]}}`,
	} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, response) }))
			defer server.Close()
			results, err := SweepRoutes(SweepOptions{BaseURL: server.URL, MCPBaseURL: server.URL, APIKey: "key", Routes: []string{"MCP get_index_status"}, Operations: map[string]Operation{"MCP get_index_status": {Method: http.MethodPost, Path: "/mcp/message", Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_index_status","arguments":{}}}`, MCP: true}}, Iterations: 1, Timeout: time.Second})
			if err != nil || len(results) != 1 || !results[0].HardFailed {
				t.Fatalf("results=%+v err=%v", results, err)
			}
		})
	}
}

func TestSweepOperationsRejectsCountedClientError(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == warmupRequests+1 {
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	results, err := SweepRoutes(SweepOptions{BaseURL: server.URL, Routes: []string{"POST /pilot"}, Operations: map[string]Operation{"POST /pilot": {Method: http.MethodPost, Path: "/pilot", Body: `{}`}}, Iterations: 2, Timeout: time.Second})
	if err != nil || results[0].Exercised {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestPilotOperationsRejectWrongSuccessfulPayloads(t *testing.T) {
	cases := []struct {
		name string
		op   Operation
		body string
	}{
		{"ingester", Operation{Method: http.MethodGet, Path: "/ingester", Expect: "ingester_status"}, `{"ingester":"other","queue":{}}`},
		{"catalog", Operation{Method: http.MethodPost, Path: "/catalog", Expect: "relationships_catalog"}, `{"verbs":[],"verb_count":2,"total_edges":0,"layer_count":0}`},
		{"catalog_invented", Operation{Method: http.MethodPost, Path: "/catalog", Expect: "relationships_catalog"}, `{"verbs":[{"verb":"nonsense","layer":"made-up","count":0}],"verb_count":1,"layer_count":1,"total_edges":0}`},
		{"no_content", Operation{Method: http.MethodGet, Path: "/ingester", Expect: "ingester_status"}, ``},
		{"mcp", Operation{Method: http.MethodPost, Path: "/mcp/message", MCP: true, Expect: "mcp_index_status"}, `{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"repository_count":"bad"}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.name == "no_content" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			opts := SweepOptions{BaseURL: server.URL, MCPBaseURL: server.URL, Routes: []string{tc.name}, Operations: map[string]Operation{tc.name: tc.op}, Iterations: 1, Timeout: time.Second}
			results, err := SweepRoutes(opts)
			if err != nil || len(results) != 1 || !results[0].HardFailed {
				t.Fatalf("wrong successful payload passed: results=%+v err=%v", results, err)
			}
		})
	}
}

func TestConcurrentPilotRejectsOneWrongResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 3 {
			_, _ = io.WriteString(w, `{"ingester":"other","queue":{}}`)
			return
		}
		_, _ = io.WriteString(w, `{"ingester":"repository","runtime_family":"ingester","queue":{},"health":{}}`)
	}))
	defer server.Close()
	results, err := SweepConcurrentOperations(SweepOptions{BaseURL: server.URL, Timeout: time.Second}, map[string]Operation{"GET /pilot": {Method: http.MethodGet, Path: "/pilot", Expect: "ingester_status"}}, 2, 8)
	if err != nil || len(results) != 1 || !results[0].HardFailed || results[0].Succeeded != 7 || results[0].Requested != 8 || len(results[0].Statuses) != 8 {
		t.Fatalf("wrong concurrent response passed: results=%+v err=%v", results, err)
	}
}

func TestPilotErrorBodyRemainsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, strings.Repeat("x", hardFailedBodyCap+100))
	}))
	defer server.Close()
	results, err := SweepRoutes(SweepOptions{BaseURL: server.URL, Routes: []string{"GET /pilot"}, Operations: map[string]Operation{"GET /pilot": {Method: http.MethodGet, Path: "/pilot", Expect: "ingester_status"}}, Iterations: 1, Timeout: time.Second})
	if err != nil || len(results) != 1 || !results[0].HardFailed || len(results[0].HardFailedBody) != hardFailedBodyCap {
		t.Fatalf("pilot error body cap: results=%+v err=%v", results, err)
	}
}

func TestPilotWarmupWrongPayloadCannotPassCountedSweep(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"ingester":"other","runtime_family":"ingester","queue":{},"health":{}}`)
			return
		}
		_, _ = io.WriteString(w, `{"ingester":"repository","runtime_family":"ingester","queue":{},"health":{}}`)
	}))
	defer server.Close()
	results, err := SweepRoutes(SweepOptions{BaseURL: server.URL, Routes: []string{"GET /pilot"}, Operations: map[string]Operation{"GET /pilot": {Method: http.MethodGet, Path: "/pilot", Expect: "ingester_status"}}, Iterations: 1, Timeout: time.Second})
	if err != nil || len(results) != 1 || !results[0].HardFailed {
		t.Fatalf("warmup mismatch passed: results=%+v err=%v", results, err)
	}
}

func TestConcurrentReportRecordsVersionAndEveryOutcome(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.json")
	results := []RouteLatency{{Route: "GET /pilot", Method: http.MethodGet, Path: "/pilot", Requested: 3, Succeeded: 2, Workers: 2, PeakInFlight: 2, Wall: 100 * time.Millisecond, Samples: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}, Statuses: []int{200, 500, 200}}}
	if err := writeConcurrentReport(path, runOptions{concurrentWorkers: 2, concurrentRequests: 3}, results); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Version    int `json:"version"`
		Operations []struct {
			Requested int   `json:"requested"`
			Succeeded int   `json:"succeeded"`
			Statuses  []int `json:"statuses"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Version != 1 || len(report.Operations) != 1 || report.Operations[0].Requested != 3 || report.Operations[0].Succeeded != 2 || !slices.Equal(report.Operations[0].Statuses, []int{200, 500, 200}) {
		t.Fatalf("report missing load identity or outcomes: %+v", report)
	}
}
