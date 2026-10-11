// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiagnosticBoundariesOutsideConcurrentSamples(t *testing.T) {
	var completed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(time.Millisecond)
		completed.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	phases := []string{}
	var hookTime time.Duration
	opts := SweepOptions{
		BaseURL: server.URL, Timeout: time.Second,
		DiagnosticBoundary: func(phase string) {
			start := time.Now()
			defer func() { hookTime += time.Since(start) }()
			phases = append(phases, phase)
			expected := int32(0)
			if phase == "after_concurrent" {
				expected = 20
			}
			if completed.Load() != expected {
				t.Fatalf("boundary %s inside burst: completed=%d", phase, completed.Load())
			}
			time.Sleep(30 * time.Millisecond)
		},
	}
	op := Operation{Method: http.MethodGet, Path: "/api/v0/status/ingesters/repository"}
	started := time.Now()
	results, err := SweepConcurrentOperations(opts, map[string]Operation{op.Path: op}, 4, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(phases) != 2 || phases[0] != "before_concurrent" || phases[1] != "after_concurrent" {
		t.Fatalf("phases=%v", phases)
	}
	if time.Since(started)-results[0].Wall < hookTime {
		t.Fatalf("diagnostic boundaries contaminated pilot wall=%s", results[0].Wall)
	}
}
