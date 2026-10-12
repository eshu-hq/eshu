// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type diagnosticStatementFixture struct {
	phases   []string
	failure  error
	finished bool
}

func (f *diagnosticStatementFixture) capture(phase string) { f.phases = append(f.phases, phase) }
func (f *diagnosticStatementFixture) finish() error        { f.finished = true; return f.failure }

func TestDiagnosticStatementFailurePreservesOriginalGateFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(time.Millisecond)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	failure := errors.New("closed diagnostic fixture failure")
	capture := &diagnosticStatementFixture{failure: failure}
	opts := runOptions{
		apiBaseURL: server.URL, mcpBaseURL: server.URL, concurrentWorkers: 2, concurrentRequests: 4, requestTimeout: time.Second,
		diagnosticReport: filepath.Join(t.TempDir(), "client.json"), statementCapture: capture,
	}
	op := Operation{Method: http.MethodGet, Path: "/api/v0/status/ingesters/repository"}
	err := runConcurrentPilot(opts, map[string]Operation{op.Path: op}, RouteBudgets{def: time.Second})
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "failed correctness or latency budget") || !capture.finished || len(capture.phases) != 2 {
		t.Fatalf("failure lost: err=%v capture=%+v", err, capture)
	}
}
