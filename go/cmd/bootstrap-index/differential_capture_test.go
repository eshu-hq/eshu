// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/capture"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// fakeCaptureExecutor accepts every statement without a backend, so the
// capture wiring proves itself hermetically.
type fakeCaptureExecutor struct{}

func (fakeCaptureExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func openTestCaptureSession(t *testing.T, backend string) (*capture.Session, string) {
	t.Helper()
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	dir := t.TempDir()
	getenv := func(key string) string {
		switch key {
		case "ESHU_DIFFERENTIAL_CAPTURE_DIR":
			return dir
		case "ESHU_GRAPH_BACKEND":
			return backend
		}
		return ""
	}
	session, err := capture.Open(getenv, "bootstrap-index-test")
	if err != nil {
		t.Fatalf("capture.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("session.Close() error = %v", err)
		}
	})
	return session, dir
}

// TestBootstrapCanonicalExecutorCapturesStatements proves the capture wrap
// sits on the executed path: a statement through the wired executor lands
// in the recordings directory labeled with the backend.
func TestBootstrapCanonicalExecutorCapturesStatements(t *testing.T) {
	session, dir := openTestCaptureSession(t, "neo4j")
	executor, err := bootstrapCanonicalExecutorForGraphBackend(
		fakeCaptureExecutor{},
		runtimecfg.GraphBackendNeo4j,
		func(string) string { return "" },
		nil,
		nil,
		nil,
		session,
	)
	if err != nil {
		t.Fatalf("bootstrapCanonicalExecutorForGraphBackend() error = %v", err)
	}
	if err := executor.Execute(context.Background(), sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalUpsert,
		Cypher:    "MERGE (n:Repository)",
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	byBackend, err := capture.LoadDir(dir)
	if err != nil {
		t.Fatalf("capture.LoadDir() error = %v", err)
	}
	if len(byBackend["neo4j"]) != 1 {
		t.Fatalf("captured neo4j records = %d, want 1", len(byBackend["neo4j"]))
	}
}

// TestOpenBootstrapCanonicalWriterFailsClosedOnCaptureFlagWithoutDir pins
// the fail-closed contract: the flag without a directory errors before any
// datastore opens.
func TestOpenBootstrapCanonicalWriterFailsClosedOnCaptureFlagWithoutDir(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	_, _, err := openBootstrapCanonicalWriter(context.Background(), nil, func(string) string { return "" }, nil, nil)
	if err == nil {
		t.Fatal("openBootstrapCanonicalWriter() error = nil, want fail-closed capture error")
	}
	if !strings.Contains(err.Error(), "differential capture") {
		t.Fatalf("openBootstrapCanonicalWriter() error = %q, want differential capture context", err)
	}
}
