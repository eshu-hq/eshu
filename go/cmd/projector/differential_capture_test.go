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
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

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
	session, err := capture.Open(getenv, "projector-test")
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

// TestProjectorCanonicalExecutorCapturesStatements proves the capture wrap
// sits on the executed path: a statement through the wired executor lands
// in the recordings directory labeled with the backend.
func TestProjectorCanonicalExecutorCapturesStatements(t *testing.T) {
	getenv := func(string) string { return "" }
	session, dir := openTestCaptureSession(t, "nornicdb")
	executor := projectorCanonicalExecutorForGraphBackend(
		&projectorRetryRecordingExecutor{},
		runtimecfg.GraphBackendNornicDB,
		projectorNornicDBConfigForTest(t, getenv),
		getenv,
		nil,
		nil,
		session,
	)
	if err := executor.Execute(context.Background(), sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalUpsert,
		Cypher:    "MERGE (p:Package {uid: $uid})",
		Parameters: map[string]any{
			"uid": "npm://registry.npmjs.org/@angular/core",
		},
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	byBackend, err := capture.LoadDir(dir)
	if err != nil {
		t.Fatalf("capture.LoadDir() error = %v", err)
	}
	if len(byBackend["nornicdb"]) != 1 {
		t.Fatalf("captured nornicdb records = %d, want 1", len(byBackend["nornicdb"]))
	}
}

// TestOpenProjectorCanonicalWriterFailsClosedOnCaptureFlagWithoutDir pins
// the fail-closed contract: the flag without a directory errors before any
// datastore opens.
func TestOpenProjectorCanonicalWriterFailsClosedOnCaptureFlagWithoutDir(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	_, _, err := openProjectorCanonicalWriter(context.Background(), postgres.SQLDB{}, func(string) string { return "" }, nil, nil)
	if err == nil {
		t.Fatal("openProjectorCanonicalWriter() error = nil, want fail-closed capture error")
	}
	if !strings.Contains(err.Error(), "differential capture") {
		t.Fatalf("openProjectorCanonicalWriter() error = %q, want differential capture context", err)
	}
}
