// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// ExecuteProbe records the call and returns a scripted found/error pair, for
// asserting FaultingExecutor.ExecuteProbe forwards unconditionally (#5998; see
// FaultingExecutor.ExecuteProbe's doc comment for why no fault kind targets
// this seam).
func (e *faultRecordingExecutor) ExecuteProbe(_ context.Context, stmt cypher.Statement) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.probes = append(e.probes, stmt)
	return e.probeFound, e.probeErr
}

func (e *faultRecordingExecutor) probeCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.probes)
}

// TestFaultingExecutorExecuteProbeForwardsUnconditionally proves ExecuteProbe
// delegates to inner and returns its found/error result unchanged, with no
// scripted fault able to intercept it (#5998) -- unlike Execute/ExecuteGroup,
// which the once-then-succeed fault below would otherwise fire on.
func TestFaultingExecutorExecuteProbeForwardsUnconditionally(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{probeFound: true}
	// Script a fault that would fire on the very first graph-write call if it
	// could reach ExecuteProbe; it must not, proving the seam is untouched.
	script := onceThenSucceedScript(faultreplay.LaneQueueRetry, intPtr(1), nil)
	fe := mustFaultingExecutor(t, inner, script, "")

	found, err := fe.ExecuteProbe(context.Background(), cypher.Statement{Cypher: "MATCH (r) RETURN r LIMIT 1"})
	if err != nil {
		t.Fatalf("ExecuteProbe() error = %v, want nil (no fault targets this seam)", err)
	}
	if !found {
		t.Fatal("ExecuteProbe() found = false, want true")
	}
	if got := inner.probeCount(); got != 1 {
		t.Fatalf("inner.ExecuteProbe calls = %d, want 1", got)
	}
	if fe.OnceThenSucceedFired() {
		t.Fatal("OnceThenSucceedFired() = true, want false: ExecuteProbe must not consume the scripted fault")
	}
}

// TestFaultingExecutorProbeFollowsGroup is the ifafaultinjection-tagged
// counterpart of TestWrapperProbeFollowsGroup (probe_follows_group_test.go),
// which cannot reference FaultingExecutor since it only builds under this
// tag. Asserts the same #5998 review F7 invariant: a wrapper that exposes
// cypher.GroupExecutor must expose cypher.ProbeExecutor too.
func TestFaultingExecutorProbeFollowsGroup(t *testing.T) {
	t.Parallel()

	fe := mustFaultingExecutor(t, &faultRecordingExecutor{supportsGrp: true}, faultreplay.Script{Version: faultreplay.CurrentVersion}, "")

	var wrapped cypher.Executor = fe
	_, gotGroup := wrapped.(cypher.GroupExecutor)
	_, gotProbe := wrapped.(cypher.ProbeExecutor)
	if !gotGroup || !gotProbe {
		t.Fatalf("FaultingExecutor: cypher.GroupExecutor=%v, cypher.ProbeExecutor=%v, want both true", gotGroup, gotProbe)
	}
}

// TestFaultingExecutorExecuteProbeErrorsWhenInnerLacksProbeSupport proves
// ExecuteProbe fails closed with errFaultingExecutorInnerNoProbe (not a silent
// "not found") when the wrapped executor does not implement cypher.ProbeExecutor.
func TestFaultingExecutorExecuteProbeErrorsWhenInnerLacksProbeSupport(t *testing.T) {
	t.Parallel()

	inner := newFaultExecuteOnlyRecordingExecutor()
	fe := mustFaultingExecutor(t, inner, faultreplay.Script{Version: faultreplay.CurrentVersion}, "")
	found, err := fe.ExecuteProbe(context.Background(), cypher.Statement{Cypher: "MATCH (r) RETURN r LIMIT 1"})
	if !errors.Is(err, errFaultingExecutorInnerNoProbe) {
		t.Fatalf("ExecuteProbe() error = %v, want errFaultingExecutorInnerNoProbe", err)
	}
	if found {
		t.Fatal("found = true on an unsupported probe, want false")
	}
}
