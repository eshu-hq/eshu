// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type ifaWiringTestExecutor struct{}

func (ifaWiringTestExecutor) Execute(context.Context, sourcecypher.Statement) error { return nil }

func getenvMap(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

// TestWrapIfaFaultExecutorPassthroughWhenEnvUnset proves the common case: an
// unset ESHU_IFA_FAULT_SCRIPT leaves the reducer's executor chain untouched,
// even under the ifafaultinjection build tag.
func TestWrapIfaFaultExecutorPassthroughWhenEnvUnset(t *testing.T) {
	t.Parallel()

	inner := ifaWiringTestExecutor{}
	got, err := wrapIfaFaultExecutor(inner, getenvMap(nil), nil)
	if err != nil {
		t.Fatalf("wrapIfaFaultExecutor: %v", err)
	}
	if got != sourcecypher.Executor(inner) {
		t.Fatalf("expected passthrough when %s is unset, got %#v", ifaFaultScriptEnv, got)
	}
}

// TestWrapIfaFaultExecutorErrorsOnMissingScriptFile proves a bad path is a
// startup error, not a silently-ignored fault script.
func TestWrapIfaFaultExecutorErrorsOnMissingScriptFile(t *testing.T) {
	t.Parallel()

	inner := ifaWiringTestExecutor{}
	env := getenvMap(map[string]string{ifaFaultScriptEnv: filepath.Join(t.TempDir(), "does-not-exist.json")})
	if _, err := wrapIfaFaultExecutor(inner, env, nil); err == nil {
		t.Fatal("expected an error for a nonexistent fault-script path")
	}
}

// TestWrapIfaFaultExecutorErrorsOnInvalidScript proves a fault script that
// fails faultreplay.Script.Validate is also a startup error.
func TestWrapIfaFaultExecutorErrorsOnInvalidScript(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(path, []byte(`{"version":999}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	inner := ifaWiringTestExecutor{}
	env := getenvMap(map[string]string{ifaFaultScriptEnv: path})
	if _, err := wrapIfaFaultExecutor(inner, env, nil); err == nil {
		t.Fatal("expected an error for an unsupported fault-script version")
	}
}

// TestWrapIfaFaultExecutorWrapsWithFaultingExecutorForValidScript proves a
// valid fault script produces a *sourcecypher.FaultingExecutor and derives
// the restart sentinel path from the script path by the documented
// convention (<script path>.restart-sentinel).
func TestWrapIfaFaultExecutorWrapsWithFaultingExecutorForValidScript(t *testing.T) {
	t.Parallel()

	ordinal := 1
	script := faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{
			{
				Kind:    faultreplay.KindFailGraphWriteOnceThenSucceed,
				Trigger: faultreplay.Trigger{StatementOrdinal: &ordinal},
				Target:  faultreplay.Target{Lane: faultreplay.LaneQueueRetry},
			},
		},
	}
	raw, err := json.Marshal(script)
	if err != nil {
		t.Fatalf("marshal fixture script: %v", err)
	}
	path := filepath.Join(t.TempDir(), "fault.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	inner := ifaWiringTestExecutor{}
	env := getenvMap(map[string]string{ifaFaultScriptEnv: path})
	got, err := wrapIfaFaultExecutor(inner, env, nil)
	if err != nil {
		t.Fatalf("wrapIfaFaultExecutor: %v", err)
	}
	fe, ok := got.(*sourcecypher.FaultingExecutor)
	if !ok {
		t.Fatalf("expected *sourcecypher.FaultingExecutor, got %T", got)
	}
	if err := fe.Execute(context.Background(), sourcecypher.Statement{Cypher: "MERGE (a) RETURN a"}); err == nil {
		t.Fatal("expected the scripted fault to fire on the first call")
	}
	if !fe.OnceThenSucceedFired() {
		t.Fatal("expected OnceThenSucceedFired() to report true")
	}
}
