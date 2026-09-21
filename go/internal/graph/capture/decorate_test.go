// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
package capture

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// fakeReader answers one canned row without a backend, so the decorator
// tests prove the capture path hermetically.
type fakeReader struct{}

func (fakeReader) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return []map[string]any{{"n": 1}}, nil
}

func (fakeReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{"n": 1}, nil
}

// fakeWriter accepts every statement, so the decorator tests prove the
// write-capture path hermetically.
type fakeWriter struct{}

func (fakeWriter) Execute(context.Context, sourcecypher.Statement) error { return nil }

// TestOpenDisabledReturnsNilSession pins the hot-path contract: without the
// opt-in flag, Open returns a nil session, and every decorator on it is a
// passthrough returning the inner seam unchanged.
func TestOpenDisabledReturnsNilSession(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "")
	getenv := func(string) string { return "" }
	session, err := Open(getenv, "drain-test")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if session != nil {
		t.Fatal("Open() session != nil, want nil when capture is off")
	}
	reader := fakeReader{}
	if got := session.Reader(reader); got != backendconformance.GraphQuery(reader) {
		t.Fatal("Reader() wrapped the seam while capture is off")
	}
	writer := fakeWriter{}
	if got := session.Writer(writer); got != sourcecypher.Executor(writer) {
		t.Fatal("Writer() wrapped the seam while capture is off")
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// TestOpenFlagWithoutDirFailsClosed pins the fail-closed contract: the
// opt-in flag without a recordings directory errors at startup instead of
// running a full replay that records nothing.
func TestOpenFlagWithoutDirFailsClosed(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	getenv := func(string) string { return "" }
	if _, err := Open(getenv, "drain-test"); err == nil {
		t.Fatal("Open() error = nil, want fail-closed without a capture dir")
	}
}

// TestSessionCapturesReadsAndWrites is the end-to-end decorator proof
// without a backend: statements run through both decorated seams land in
// the recordings directory labeled with the configured backend.
func TestSessionCapturesReadsAndWrites(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	dir := t.TempDir()
	getenv := func(key string) string {
		switch key {
		case "ESHU_DIFFERENTIAL_CAPTURE_DIR":
			return dir
		case "ESHU_GRAPH_BACKEND":
			return "neo4j"
		}
		return ""
	}
	session, err := Open(getenv, "drain-test")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	ctx := context.Background()
	if _, err := session.Reader(fakeReader{}).Run(ctx, "MATCH (n:Repository) RETURN n", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := session.Writer(fakeWriter{}).Execute(ctx, sourcecypher.Statement{Cypher: "CREATE (n:Repository)"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	byBackend, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(byBackend["neo4j"]) != 2 {
		t.Fatalf("LoadDir() neo4j records = %d, want 2", len(byBackend["neo4j"]))
	}
}
