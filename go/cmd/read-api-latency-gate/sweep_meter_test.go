// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
