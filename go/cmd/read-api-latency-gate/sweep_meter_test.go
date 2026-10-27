// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMeter records the order of Reset/Read calls against the server's own
// request log so tests can prove WHEN the sweep meters, and returns fixed
// counters.
type fakeMeter struct {
	mu       sync.Mutex
	events   *[]string
	counters WorkCounters
	resetErr error
	readErr  error
}

type fakeSnapshotMeter struct {
	fakeMeter
	snapshot WorkSnapshot
	token    time.Time
	beginErr error
	endErr   error
}

func (m *fakeSnapshotMeter) BeginSnapshot(context.Context) (time.Time, error) {
	*m.events = append(*m.events, "begin")
	return m.token, m.beginErr
}

func (m *fakeSnapshotMeter) EndSnapshot(_ context.Context, token time.Time) (WorkSnapshot, error) {
	*m.events = append(*m.events, "end")
	if !token.Equal(m.token) {
		return WorkSnapshot{}, errors.New("wrong route-local snapshot token")
	}
	return m.snapshot, m.endErr
}

func TestSearchEntityWorkSnapshotSurvivesBudgetBreach(t *testing.T) {
	var events []string
	var mu sync.Mutex
	srv := meteredServer(&events, &mu, http.StatusOK)
	defer srv.Close()
	token := time.Date(2026, time.October, 26, 19, 0, 0, 0, time.UTC)
	meter := &fakeSnapshotMeter{
		fakeMeter: fakeMeter{events: &events, counters: WorkCounters{Calls: 2}},
		token:     token,
		snapshot: WorkSnapshot{
			Scope:      "database_interval",
			Counters:   WorkCounters{Calls: 200, Rows: 10, Blks: 300},
			Statements: []StatementWork{{DBID: 1, UserID: 2, QueryID: "-9223372036854775808", TopLevel: true, StatsSinceUnixMicro: token.UnixMicro(), Calls: 200, Rows: 10, SharedBlksHit: 300}},
		},
	}
	results, err := SweepRoutes(SweepOptions{
		BaseURL: srv.URL, MCPBaseURL: srv.URL, APIKey: "k",
		Routes:     []string{"MCP search_entity_content", "GET /other"},
		Operations: map[string]Operation{"MCP search_entity_content": {Method: http.MethodPost, Path: "/mcp/message", MCP: true}},
		Iterations: 2, Runs: 2, Timeout: 5 * time.Second, Meter: meter,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantEvents := strings.Repeat("req,", warmupRequests) + "begin," + strings.Repeat("req,", 4) + "end," + strings.Repeat("req,", warmupRequests) + "reset," + strings.Repeat("req,", 4) + "read"
	if got := strings.Join(events, ","); got != wantEvents {
		t.Fatalf("meter events = %s, want %s", got, wantEvents)
	}
	if got := results[0].Work.Calls; got != 50 {
		t.Fatalf("target calls per request = %g, want 50", got)
	}
	if results[1].StatementDiagnostics != nil {
		t.Fatal("ordinary route acquired targeted statement diagnostics")
	}
	path := filepath.Join(t.TempDir(), "work.json")
	if err := writeWorkReportFile(path, results); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Routes []struct {
			Route                string        `json:"route"`
			StatementDiagnostics *WorkSnapshot `json:"statement_diagnostics"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Routes) != 2 || report.Routes[0].StatementDiagnostics != nil || report.Routes[1].StatementDiagnostics == nil {
		t.Fatalf("report route diagnostics = %+v", report.Routes)
	}
	if got := report.Routes[1].StatementDiagnostics.Statements[0].QueryID; got != "-9223372036854775808" {
		t.Fatalf("queryid = %q, want exact signed decimal string", got)
	}
	budgets, err := ParseRouteWorkBudgets(strings.NewReader("default\t100\t1000\t100\nMCP search_entity_content\t40\t1000\t100\tlocal attribution proof\n"))
	if err != nil {
		t.Fatal(err)
	}
	var verdict strings.Builder
	if failures := reportWorkResults(&verdict, results, budgets); len(failures) != 1 || !strings.Contains(verdict.String(), "calls 50.0 > 40") {
		t.Fatalf("work verdict = %v, output %q", failures, verdict.String())
	}
}

func (m *fakeMeter) Reset(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.events = append(*m.events, "reset")
	return m.resetErr
}

func (m *fakeMeter) Read(context.Context) (WorkCounters, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.events = append(*m.events, "read")
	return m.counters, m.readErr
}

func meteredServer(events *[]string, mu *sync.Mutex, status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*events = append(*events, "req")
		mu.Unlock()
		w.WriteHeader(status)
	}))
}

// TestSweepRoutesMetersOnlyTheCountedRequests pins the metering window: the
// counters are reset AFTER the warmup requests and read AFTER the counted ones,
// so warmup cost never lands in the per-request average, and the average is the
// counters divided by the counted iterations.
func TestSweepRoutesMetersOnlyTheCountedRequests(t *testing.T) {
	var events []string
	var mu sync.Mutex
	srv := meteredServer(&events, &mu, http.StatusOK)
	defer srv.Close()
	meter := &fakeMeter{events: &events, counters: WorkCounters{Calls: 500, Rows: 1000, Blks: 40_000}}

	results, err := SweepRoutes(SweepOptions{
		BaseURL: srv.URL, APIKey: "k", Routes: []string{"GET /r"},
		Iterations: 10, Timeout: 5 * time.Second, Meter: meter,
	})
	if err != nil {
		t.Fatalf("SweepRoutes: %v", err)
	}

	want := strings.Repeat("req,", warmupRequests) + "reset," + strings.Repeat("req,", 10) + "read"
	if got := strings.Join(events, ","); got != want {
		t.Fatalf("event order = %s, want %s", got, want)
	}
	r := results[0]
	if !r.Metered {
		t.Fatal("Metered = false, want true")
	}
	if r.Work != (WorkPerRequest{Calls: 50, Rows: 100, Blks: 4000}) {
		t.Errorf("Work = %+v, want counters / 10 iterations", r.Work)
	}
}

func TestSweepRoutesFailsTheRunWhenTheMeterFails(t *testing.T) {
	for name, meter := range map[string]func(*[]string) *fakeMeter{
		"reset": func(e *[]string) *fakeMeter { return &fakeMeter{events: e, resetErr: errors.New("boom")} },
		"read":  func(e *[]string) *fakeMeter { return &fakeMeter{events: e, readErr: errors.New("boom")} },
	} {
		var events []string
		var mu sync.Mutex
		srv := meteredServer(&events, &mu, http.StatusOK)
		_, err := SweepRoutes(SweepOptions{
			BaseURL: srv.URL, APIKey: "k", Routes: []string{"GET /r"},
			Iterations: 2, Timeout: 5 * time.Second, Meter: meter(&events),
		})
		srv.Close()
		if err == nil {
			t.Errorf("%s error: SweepRoutes returned nil, want the meter error to abort the run instead of continuing on latency alone", name)
		}
	}
}

func TestSweepRoutesDoesNotMeterANotExercisedRoute(t *testing.T) {
	var events []string
	var mu sync.Mutex
	srv := meteredServer(&events, &mu, http.StatusBadRequest)
	defer srv.Close()
	meter := &fakeMeter{events: &events}

	results, err := SweepRoutes(SweepOptions{
		BaseURL: srv.URL, APIKey: "k", Routes: []string{"GET /r"},
		Iterations: 5, Timeout: 5 * time.Second, Meter: meter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Exercised || results[0].Metered {
		t.Errorf("result = %+v, want neither exercised nor metered", results[0])
	}
	for _, e := range events {
		if e == "reset" || e == "read" {
			t.Fatalf("meter was used for a not-exercised route: %v", events)
		}
	}
}

func TestSweepRoutesWithoutAMeterLeavesRoutesUnmetered(t *testing.T) {
	var events []string
	var mu sync.Mutex
	srv := meteredServer(&events, &mu, http.StatusOK)
	defer srv.Close()

	results, err := SweepRoutes(SweepOptions{
		BaseURL: srv.URL, APIKey: "k", Routes: []string{"GET /r"},
		Iterations: 2, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Metered {
		t.Error("Metered = true without a meter")
	}
}

func TestMeasureBackgroundCallRateDividesCallsByTheIdleWindow(t *testing.T) {
	var events []string
	meter := &fakeMeter{events: &events, counters: WorkCounters{Calls: 5}}

	rate, err := MeasureBackgroundCallRate(context.Background(), meter, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if rate != 100 { // 5 calls / 0.05s
		t.Errorf("rate = %v, want 100", rate)
	}
	if got := strings.Join(events, ","); got != "reset,read" {
		t.Errorf("event order = %s, want reset,read", got)
	}
}

func TestCheckMeterQuietRefusesANoisyStack(t *testing.T) {
	if err := CheckMeterQuiet(0.4); err != nil {
		t.Errorf("quiet stack refused: %v", err)
	}
	if err := CheckMeterQuiet(2.0); err != nil {
		t.Errorf("exactly the limit must pass: %v", err)
	}
	if err := CheckMeterQuiet(2.1); err == nil {
		t.Error("a stack above 2 statements/s while idle must fail the run")
	}
}

func TestSearchEntitySnapshotRequiresDetailedMeter(t *testing.T) {
	var events []string
	var mu sync.Mutex
	srv := meteredServer(&events, &mu, http.StatusOK)
	defer srv.Close()
	_, err := SweepRoutes(SweepOptions{
		BaseURL: srv.URL, MCPBaseURL: srv.URL, APIKey: "k",
		Routes:     []string{"MCP search_entity_content"},
		Operations: map[string]Operation{"MCP search_entity_content": {Method: http.MethodPost, Path: "/mcp/message", MCP: true}},
		Iterations: 1, Timeout: 5 * time.Second, Meter: &fakeMeter{events: &events},
	})
	if err == nil || !strings.Contains(err.Error(), "no statement snapshot support") {
		t.Fatalf("SweepRoutes error = %v, want unsupported meter error", err)
	}
	if strings.Contains(strings.Join(events, ","), "reset") {
		t.Fatalf("target silently fell back to legacy meter: %v", events)
	}
}

func TestValidateWorkSnapshotRejectsUntrustworthyRows(t *testing.T) {
	token := time.Date(2026, time.October, 26, 19, 0, 0, 0, time.UTC)
	valid := StatementWork{DBID: 1, UserID: 2, QueryID: "-9223372036854775808", StatsSinceUnixMicro: token.UnixMicro(), Calls: 1, Rows: 2, SharedBlksHit: 3}
	if got, err := validateWorkSnapshot(token, nil); err != nil || got.Counters != (WorkCounters{}) || got.Statements == nil {
		t.Fatalf("empty snapshot = %+v, %v", got, err)
	}
	if got, err := validateWorkSnapshot(token, []StatementWork{valid}); err != nil || got.Counters != (WorkCounters{Calls: 1, Rows: 2, Blks: 3}) {
		t.Fatalf("valid snapshot = %+v, %v", got, err)
	}
	for name, modify := range map[string]func(*StatementWork){
		"missing-query-id":      func(row *StatementWork) { row.QueryID = "" },
		"noncanonical-query-id": func(row *StatementWork) { row.QueryID = "+1" },
		"query-id-overflow":     func(row *StatementWork) { row.QueryID = "9223372036854775808" },
		"missing-dbid":          func(row *StatementWork) { row.DBID = 0 },
		"stale-entry-epoch":     func(row *StatementWork) { row.StatsSinceUnixMicro-- },
		"negative-calls":        func(row *StatementWork) { row.Calls = -1 },
		"negative-blocks":       func(row *StatementWork) { row.TempBlksWritten = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			row := valid
			modify(&row)
			if _, err := validateWorkSnapshot(token, []StatementWork{row}); err == nil {
				t.Fatalf("accepted invalid %+v", row)
			}
		})
	}
	if _, err := validateWorkSnapshot(token, []StatementWork{valid, valid}); err == nil {
		t.Fatal("accepted duplicate identity")
	}
	large := valid
	large.Calls = math.MaxInt64
	if _, err := validateWorkSnapshot(token, []StatementWork{large, {DBID: 3, UserID: 2, QueryID: "1", StatsSinceUnixMicro: token.UnixMicro(), Calls: 1}}); err == nil {
		t.Fatal("accepted call counter overflow")
	}
	large = valid
	large.SharedBlksHit = math.MaxInt64
	if _, err := validateWorkSnapshot(token, []StatementWork{large, {DBID: 3, UserID: 2, QueryID: "1", StatsSinceUnixMicro: token.UnixMicro(), TempBlksRead: 1}}); err == nil {
		t.Fatal("accepted block counter overflow")
	}
}

func TestValidateWorkSnapshotStatementCap(t *testing.T) {
	token := time.Date(2026, time.October, 26, 19, 0, 0, 0, time.UTC)
	rows := make([]StatementWork, 10_001)
	for i := range rows {
		rows[i] = StatementWork{
			DBID: 1, UserID: 2, QueryID: fmt.Sprintf("%d", i),
			StatsSinceUnixMicro: token.UnixMicro(), Calls: 1, Rows: 2, SharedBlksHit: 3,
		}
	}
	atLimit, err := validateWorkSnapshot(token, rows[:10_000])
	if err != nil {
		t.Fatalf("valid snapshot at the 10000-row limit rejected: %v", err)
	}
	if len(atLimit.Statements) != 10_000 || atLimit.Counters != (WorkCounters{Calls: 10_000, Rows: 20_000, Blks: 30_000}) {
		t.Fatalf("snapshot at the limit has wrong rows or totals: %+v", atLimit.Counters)
	}
	if _, err := validateWorkSnapshot(token, rows); err == nil || !strings.Contains(err.Error(), "exceeds 10000 rows") {
		t.Fatalf("valid snapshot beyond the limit: error = %v, want cap rejection", err)
	}
}

func TestSearchEntitySnapshotErrorsAbortTheGate(t *testing.T) {
	for name, setError := range map[string]func(*fakeSnapshotMeter){
		"begin": func(m *fakeSnapshotMeter) { m.beginErr = errors.New("reset failed") },
		"end":   func(m *fakeSnapshotMeter) { m.endErr = errors.New("capture failed") },
	} {
		t.Run(name, func(t *testing.T) {
			var events []string
			var mu sync.Mutex
			srv := meteredServer(&events, &mu, http.StatusOK)
			defer srv.Close()
			meter := &fakeSnapshotMeter{fakeMeter: fakeMeter{events: &events}, token: time.Date(2026, time.October, 26, 19, 0, 0, 0, time.UTC)}
			setError(meter)
			results, err := SweepRoutes(SweepOptions{
				BaseURL: srv.URL, MCPBaseURL: srv.URL, APIKey: "k",
				Routes:     []string{"MCP search_entity_content"},
				Operations: map[string]Operation{"MCP search_entity_content": {Method: http.MethodPost, Path: "/mcp/message", MCP: true}},
				Iterations: 1, Timeout: 5 * time.Second, Meter: meter,
			})
			if err == nil || len(results) != 0 {
				t.Fatalf("SweepRoutes = %+v, %v, want abort", results, err)
			}
		})
	}
}

func TestDecodeWorkSnapshotRequiresEveryNumericField(t *testing.T) {
	token := time.Date(2026, time.October, 26, 19, 0, 0, 0, time.UTC)
	complete := fmt.Sprintf(`[{"dbid":1,"userid":2,"queryid":"-3","toplevel":false,"stats_since_unix_micro":%d,"calls":1,"rows":2,"shared_blks_hit":3,"shared_blks_read":0,"local_blks_hit":0,"local_blks_read":0,"temp_blks_read":0,"temp_blks_written":0}]`, token.UnixMicro())
	if _, err := decodeWorkSnapshot(token, []byte(complete)); err != nil {
		t.Fatalf("complete row rejected: %v", err)
	}
	for name, missing := range map[string]string{
		"missing-calls":         `"calls":1,`,
		"missing-rows":          `"rows":2,`,
		"missing-block-counter": `"shared_blks_read":0,`,
		"missing-toplevel":      `"toplevel":false,`,
	} {
		t.Run(name, func(t *testing.T) {
			incomplete := strings.Replace(complete, missing, "", 1)
			if _, err := decodeWorkSnapshot(token, []byte(incomplete)); err == nil {
				t.Fatalf("accepted incomplete captured row: %s", incomplete)
			}
		})
	}
	if _, err := decodeWorkSnapshot(token, []byte(strings.Replace(complete, `"calls":1`, `"calls":null`, 1))); err == nil {
		t.Fatal("accepted null calls counter")
	}
}
