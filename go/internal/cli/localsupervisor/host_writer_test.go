// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestRunOwnedHostWithLayoutWritesOperatorLinesToInjectedWriter is the only
// test that reads back what the supervisor wrote. It exists because the CLI
// passes os.Stderr as `out`, so a call site left as fmt.Fprintf(os.Stderr, ...)
// produces byte-identical CLI output to a converted fmt.Fprintf(out, ...) — the
// cmd/eshu parity suite cannot tell the two apart. Every other test in this
// package passes io.Discard, which cannot tell them apart either. Assert the
// lines land in a caller-supplied writer, or the injection seam is unproven.
//
// In production `out` is written by the supervisor goroutine and by both
// finalizer goroutines. This test stubs the finalizers, so only the supervisor
// goroutine writes here, but the capture still uses the mutex-guarded
// lockedBuffer: a variant that lets a real finalizer run would race under
// -race with a bare bytes.Buffer, and the guard should already be in place.
func TestRunOwnedHostWithLayoutWritesOperatorLinesToInjectedWriter(t *testing.T) {
	t.Setenv("ESHU_QUERY_PROFILE", string(query.ProfileLocalAuthoritative))
	t.Setenv(deferContentSearchIndexesEnv, "1")

	originalPrepareWorkspace := localHostPrepareWorkspace
	originalStartEmbeddedPostgres := localHostStartEmbeddedPostgres
	originalStartManagedGraph := localHostStartManagedGraph
	originalWriteOwnerRecord := localHostWriteOwnerRecord
	originalHostname := localHostHostname
	originalStartChild := StartChildProcess
	originalWaitOwnerChildren := WaitOwnerChildren
	originalApplyBootstrap := localHostApplyBootstrap
	originalApplyGraphBootstrap := localHostApplyGraphBootstrap
	originalMarkGraphSchemaApplied := localHostMarkGraphSchemaApplied
	originalExpectedProjectors := localHostContentSearchIndexExpectedProjectors
	originalStartIaCReachabilityFinalizer := localHostStartIaCReachabilityFinalizer
	originalStartDeferredIndexes := localHostStartDeferredContentSearchIndexes
	originalStartProgressReporter := localHostStartProgressReporter
	t.Cleanup(func() {
		localHostPrepareWorkspace = originalPrepareWorkspace
		localHostStartEmbeddedPostgres = originalStartEmbeddedPostgres
		localHostStartManagedGraph = originalStartManagedGraph
		localHostWriteOwnerRecord = originalWriteOwnerRecord
		localHostHostname = originalHostname
		StartChildProcess = originalStartChild
		WaitOwnerChildren = originalWaitOwnerChildren
		localHostApplyBootstrap = originalApplyBootstrap
		localHostApplyGraphBootstrap = originalApplyGraphBootstrap
		localHostMarkGraphSchemaApplied = originalMarkGraphSchemaApplied
		localHostContentSearchIndexExpectedProjectors = originalExpectedProjectors
		localHostStartIaCReachabilityFinalizer = originalStartIaCReachabilityFinalizer
		localHostStartDeferredContentSearchIndexes = originalStartDeferredIndexes
		localHostStartProgressReporter = originalStartProgressReporter
	})

	localHostPrepareWorkspace = func(eshulocal.Layout) (*eshulocal.OwnerLock, error) {
		return &eshulocal.OwnerLock{}, nil
	}
	localHostStartEmbeddedPostgres = func(context.Context, eshulocal.Layout) (*eshulocal.ManagedPostgres, error) {
		return &eshulocal.ManagedPostgres{
			DSN:        "host=127.0.0.1 port=15439 user=eshu password=change-me dbname=postgres sslmode=disable",
			Port:       15439,
			DataDir:    "/workspace/postgres/data",
			SocketDir:  "/tmp/eshu",
			SocketPath: "/tmp/eshu/.s.PGSQL.15439",
			PID:        21,
		}, nil
	}
	localHostStartManagedGraph = func(context.Context, eshulocal.Layout, RuntimeConfig) (*ManagedGraph, error) {
		return &ManagedGraph{
			Backend:  query.GraphBackendNornicDB,
			Address:  "127.0.0.1",
			BoltPort: 17687,
			HTTPPort: 17474,
			Username: "admin",
			Password: "workspace-secret",
			PID:      88,
			Cmd:      &exec.Cmd{},
		}, nil
	}
	localHostWriteOwnerRecord = func(string, eshulocal.OwnerRecord) error { return nil }
	localHostHostname = func() (string, error) { return "local-test", nil }
	localHostApplyBootstrap = func(context.Context, string) error { return nil }
	localHostApplyGraphBootstrap = func(context.Context, RuntimeConfig, *ManagedGraph) error { return nil }
	localHostMarkGraphSchemaApplied = func(context.Context, string, RuntimeConfig, *ManagedGraph) error { return nil }
	localHostContentSearchIndexExpectedProjectors = func(string) (int, error) { return 1, nil }
	localHostStartIaCReachabilityFinalizer = func(context.Context, io.Writer, string, int) (func() error, error) {
		return func() error { return nil }, nil
	}
	// Both warning lines are reached by failing the optional starts. Neither
	// failure aborts the run, which is what makes the warning the operator's
	// only signal that the feature is off.
	localHostStartDeferredContentSearchIndexes = func(context.Context, io.Writer, string, int) (func() error, error) {
		return nil, errors.New("deferred index maintainer refused")
	}
	localHostStartProgressReporter = func(context.Context, string, string, RuntimeConfig) (localHostProgressStop, error) {
		return nil, errors.New("progress reporter refused")
	}
	StartChildProcess = func(string, []string, []string) (*exec.Cmd, error) {
		return &exec.Cmd{}, nil
	}
	WaitOwnerChildren = func(context.Context, []Child, map[string]struct{}) error { return nil }

	out := &lockedBuffer{}
	err := RunOwnedHostWithLayout(context.Background(), out, eshulocal.Layout{
		WorkspaceID:     "workspace-id",
		WorkspaceRoot:   "/workspace/repo",
		OwnerRecordPath: "/workspace/owner.json",
		CacheDir:        "/workspace/cache",
		LogsDir:         "/workspace/logs",
		GraphDir:        "/workspace/graph",
	}, ModeWatch)
	if err != nil {
		t.Fatalf("RunOwnedHostWithLayout() error = %v, want nil", err)
	}

	// One want per fmt.Fprint* call site in RunOwnedHostWithLayout. Adding a
	// line there without adding it here leaves the new call site unproven.
	wantLines := []string{
		"bootstrapping local postgres schema...",
		"local postgres schema ready",
		"bootstrapping local graph schema...",
		"local graph schema ready",
		"warning: deferred content search index maintainer unavailable: deferred index maintainer refused",
		"warning: local progress reporter unavailable: progress reporter refused",
	}
	captured := out.String()
	for _, want := range wantLines {
		if !strings.Contains(captured, want+"\n") {
			t.Fatalf("captured output missing %q\ncaptured:\n%s", want, captured)
		}
	}
}
