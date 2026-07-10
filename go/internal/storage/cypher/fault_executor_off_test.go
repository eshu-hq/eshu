// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !ifafaultinjection

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
)

// offTestExecutor is a trivial Executor whose identity a test can compare
// against NewFaultingExecutor's return value to prove passthrough.
type offTestExecutor struct{ calls int }

func (e *offTestExecutor) Execute(context.Context, Statement) error {
	e.calls++
	return nil
}

// TestNewFaultingExecutorExcludesFaultByDefault proves the ifafaultinjection
// build tag's real decorator (fault_executor.go) is absent from every normal
// build: this test file carries the !ifafaultinjection build tag, so it runs
// in the default `go test` and CI lane, where NewFaultingExecutor must
// return inner completely unchanged -- not merely behaviorally equivalent,
// but the identical value -- even when handed a script that would fire a
// fault under the tagged build. This is the regression guard for issue
// #4580 P6 S4's in-binary fault decorator never leaking into a production
// build. Its counterpart, the tagged tests in fault_executor_test.go, assert
// the fault actually fires under the ifafaultinjection tag.
func TestNewFaultingExecutorExcludesFaultByDefault(t *testing.T) {
	t.Parallel()

	ordinal := 1
	inner := &offTestExecutor{}
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

	got, err := NewFaultingExecutor(inner, script, "/does/not/exist/sentinel")
	if err != nil {
		t.Fatalf("NewFaultingExecutor: %v", err)
	}
	if got != Executor(inner) {
		t.Fatalf("expected NewFaultingExecutor to return inner unchanged outside the ifafaultinjection build tag, got a different value %#v", got)
	}

	// Calling Execute the scripted number of times must never fail: outside
	// the tag, there is no fault to fire at all.
	for i := 0; i < 3; i++ {
		if err := got.Execute(context.Background(), Statement{Cypher: "MERGE (a) RETURN a"}); err != nil {
			t.Fatalf("call %d: expected success (no fault outside the build tag), got %v", i+1, err)
		}
	}
	if inner.calls != 3 {
		t.Fatalf("expected all 3 calls to reach inner directly, got %d", inner.calls)
	}
}
