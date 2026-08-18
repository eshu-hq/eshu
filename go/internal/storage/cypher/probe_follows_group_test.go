// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// fullyCapableExecutor implements Executor, GroupExecutor, and ProbeExecutor
// so TestWrapperProbeFollowsGroup can wrap it with every Executor-decorator
// type in this package and observe which capabilities each wrapper exposes.
type fullyCapableExecutor struct{}

func (fullyCapableExecutor) Execute(context.Context, Statement) error { return nil }

func (fullyCapableExecutor) ExecuteGroup(context.Context, []Statement) error { return nil }

func (fullyCapableExecutor) ExecuteProbe(context.Context, Statement) (bool, error) {
	return true, nil
}

// wrapperProbeFollowsGroupCase names one Executor-decorator type in this
// package and states whether it is expected to expose GroupExecutor and
// ProbeExecutor when wrapping a fully-capable inner.
type wrapperProbeFollowsGroupCase struct {
	name string
	// build constructs the wrapper around a fully-capable inner and returns
	// it as a plain Executor, so the test observes capabilities purely
	// through type assertion -- the same mechanism every real caller
	// (EdgeWriter.RetractEdges, canonical writers) uses.
	build func(inner fullyCapableExecutor) Executor
	// wantGroup and wantProbe state the wrapper's documented intent. The
	// review F7 invariant under test is wantGroup == wantProbe for every
	// row: this package's contract (writer.go's ProbeExecutor doc) requires
	// every wrapper that forwards GroupExecutor to forward ProbeExecutor the
	// same way, and every wrapper that deliberately hides GroupExecutor
	// (an execute-only surface) to hide ProbeExecutor too.
	wantGroup bool
	wantProbe bool
}

// wrapperProbeFollowsGroupCases names every Executor-decorator type in this
// package this table knows how to construct. TestWrapperProbeFollowsGroup
// checks wantGroup == wantProbe for each row; the separate
// TestWrapperProbeFollowsGroupTableCoversEveryExecuteGroupReceiver test scans
// this package's production source for every ExecuteGroup receiver and
// asserts each one has a row here -- so a decorator can no longer be omitted
// from this table by hand (a full source scan replaces "audited by hand" as
// of #6157's review, which found the hand audit itself has no gate: a NEW
// decorator that forwards ExecuteGroup but not ExecuteProbe would simply be
// absent from this table and this test would stay green).
//
// FaultingExecutor (fault_executor.go) is intentionally NOT in this table: it
// only builds under the ifafaultinjection build tag, so a tag-agnostic test
// in this file cannot reference it. Its own Probe-follows-Group invariant is
// asserted directly in fault_executor_probe_test.go (same build tag); the
// coverage scan below skips any file gated behind that tag, so this
// exclusion cannot silently reappear for a future non-tag-gated type.
var wrapperProbeFollowsGroupCases = []wrapperProbeFollowsGroupCase{
	{
		name: "BackpressureExecutor",
		build: func(inner fullyCapableExecutor) Executor {
			return NewBackpressureExecutor(inner, 4, nil)
		},
		wantGroup: true,
		wantProbe: true,
	},
	{
		name: "ExecuteOnlyBackpressureExecutor",
		build: func(inner fullyCapableExecutor) Executor {
			return ExecuteOnlyBackpressureExecutor(NewBackpressureExecutor(inner, 4, nil))
		},
		wantGroup: false,
		wantProbe: false,
	},
	{
		name: "ExecuteOnlyExecutor",
		build: func(inner fullyCapableExecutor) Executor {
			return ExecuteOnlyExecutor{Inner: inner}
		},
		wantGroup: false,
		wantProbe: false,
	},
	{
		name: "InstrumentedExecutor",
		build: func(inner fullyCapableExecutor) Executor {
			return &InstrumentedExecutor{Inner: inner}
		},
		wantGroup: true,
		wantProbe: true,
	},
	{
		name: "RetryingExecutor",
		build: func(inner fullyCapableExecutor) Executor {
			return &RetryingExecutor{Inner: inner}
		},
		wantGroup: true,
		wantProbe: true,
	},
	{
		name: "TimeoutExecutor",
		build: func(inner fullyCapableExecutor) Executor {
			return TimeoutExecutor{Inner: inner}
		},
		wantGroup: true,
		wantProbe: true,
	},
}

// TestWrapperProbeFollowsGroup is the static guard for the #5998 review F1
// bug class: InstrumentedExecutor forwarded GroupExecutor but not
// ProbeExecutor, which let the reducer's real backpressure-gated chain still
// type-assert as ProbeExecutor at the top (every BackpressureExecutor always
// implements the method) while the capability silently died at
// InstrumentedExecutor in the middle -- undetectable by any single
// isolated wrapper's own unit tests. It checks wantGroup == wantProbe for
// every row in wrapperProbeFollowsGroupCases, so a future wrapper that
// forwards one capability but not the other fails HERE instead of shipping
// silently inert.
func TestWrapperProbeFollowsGroup(t *testing.T) {
	t.Parallel()

	for _, tc := range wrapperProbeFollowsGroupCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wrapped := tc.build(fullyCapableExecutor{})
			_, gotGroup := wrapped.(GroupExecutor)
			_, gotProbe := wrapped.(ProbeExecutor)

			if gotGroup != gotProbe {
				t.Fatalf("%s: GroupExecutor=%v, ProbeExecutor=%v -- Probe-follows-Group violated (writer.go's ProbeExecutor doc requires these to match)", tc.name, gotGroup, gotProbe)
			}
			if gotGroup != tc.wantGroup {
				t.Errorf("%s: GroupExecutor exposed = %v, want %v", tc.name, gotGroup, tc.wantGroup)
			}
			if gotProbe != tc.wantProbe {
				t.Errorf("%s: ProbeExecutor exposed = %v, want %v", tc.name, gotProbe, tc.wantProbe)
			}
		})
	}
}

// executeGroupReceiverPattern matches a top-level `func (recv [*]Type)
// ExecuteGroup(` method declaration and captures the receiver's type name
// (stripping any pointer `*`). The receiver's variable name is optional --
// Go permits an unnamed receiver (`func (Type) Method()`, the exact shape
// this file's own fullyCapableExecutor test fixture uses), and a pattern
// that assumed a name was present would silently miss a real decorator
// written the same way.
var executeGroupReceiverPattern = regexp.MustCompile(`(?m)^func \((?:[A-Za-z_][A-Za-z0-9_]* )?\*?([A-Za-z_][A-Za-z0-9_]*)\) ExecuteGroup\(`)

// executeGroupReceiverTypesFromSource scans every non-test, non-tag-gated
// .go file in this package's own directory and returns the receiver type
// name of every ExecuteGroup method it finds. Reads this test file's own
// directory via runtime.Caller so it resolves correctly regardless of the
// working directory `go test` runs with.
func executeGroupReceiverTypesFromSource(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	var types []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", name, err)
		}
		// FaultingExecutor's file only builds under the ifafaultinjection
		// build tag; skip any file gated behind it, matching
		// wrapperProbeFollowsGroupCases' documented exclusion above.
		if bytes.Contains(src, []byte("//go:build ifafaultinjection")) {
			continue
		}
		for _, match := range executeGroupReceiverPattern.FindAllSubmatch(src, -1) {
			types = append(types, string(match[1]))
		}
	}
	return types
}

// TestWrapperProbeFollowsGroupTableCoversEveryExecuteGroupReceiver is the
// #6157 review fix: TestWrapperProbeFollowsGroup's table used to be "audited
// by hand" with no gate proving the audit stayed current, so a NEW decorator
// that forwards ExecuteGroup but not ExecuteProbe would simply be absent from
// the table and both tests would stay green -- the exact bug class
// TestWrapperProbeFollowsGroup exists to catch, just reachable through the
// coverage gap in how its own table gets populated. This scans production
// source for every ExecuteGroup receiver and asserts each one has a row in
// wrapperProbeFollowsGroupCases, so an omission fails here instead of never
// being checked at all.
func TestWrapperProbeFollowsGroupTableCoversEveryExecuteGroupReceiver(t *testing.T) {
	t.Parallel()

	known := make(map[string]bool, len(wrapperProbeFollowsGroupCases))
	for _, tc := range wrapperProbeFollowsGroupCases {
		known[tc.name] = true
	}
	for _, typeName := range executeGroupReceiverTypesFromSource(t) {
		if !known[typeName] {
			t.Errorf("type %s implements ExecuteGroup in production source but has no row in wrapperProbeFollowsGroupCases -- add one (name, build, wantGroup, wantProbe) so a decorator that forwards ExecuteGroup but not ExecuteProbe cannot ship undetected", typeName)
		}
	}
}
