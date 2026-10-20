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

// ExecuteCountProbe records the call and returns a scripted count/error
// pair, mirroring ExecuteProbe above for the #8657 count seam.
func (e *faultRecordingExecutor) ExecuteCountProbe(_ context.Context, stmt cypher.Statement) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.countProbes = append(e.countProbes, stmt)
	return e.countValue, e.countErr
}

func (e *faultRecordingExecutor) countProbeCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.countProbes)
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
// cypher.GroupExecutor must expose cypher.ProbeExecutor too -- and the
// #8657 extension: it must expose cypher.CountProbeExecutor as well, or a
// mid-chain gap silently downgrades the file-less wipe guard.
func TestFaultingExecutorProbeFollowsGroup(t *testing.T) {
	t.Parallel()

	fe := mustFaultingExecutor(t, &faultRecordingExecutor{supportsGrp: true}, faultreplay.Script{Version: faultreplay.CurrentVersion}, "")

	var wrapped cypher.Executor = fe
	_, gotGroup := wrapped.(cypher.GroupExecutor)
	_, gotProbe := wrapped.(cypher.ProbeExecutor)
	_, gotCount := wrapped.(cypher.CountProbeExecutor)
	if !gotGroup || !gotProbe || !gotCount {
		t.Fatalf("FaultingExecutor: cypher.GroupExecutor=%v, cypher.ProbeExecutor=%v, cypher.CountProbeExecutor=%v, want all true", gotGroup, gotProbe, gotCount)
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

// TestFaultingExecutorExecuteCountProbeForwardsUnconditionally proves
// ExecuteCountProbe delegates to inner and returns its count/error result
// unchanged, with no scripted fault able to intercept it -- the count-seam
// counterpart of
// TestFaultingExecutorExecuteProbeForwardsUnconditionally above.
func TestFaultingExecutorExecuteCountProbeForwardsUnconditionally(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{countValue: 7}
	script := onceThenSucceedScript(faultreplay.LaneQueueRetry, intPtr(1), nil)
	fe := mustFaultingExecutor(t, inner, script, "")

	count, err := fe.ExecuteCountProbe(context.Background(), cypher.Statement{Cypher: "MATCH (r) RETURN count(r)"})
	if err != nil {
		t.Fatalf("ExecuteCountProbe() error = %v, want nil (no fault targets this seam)", err)
	}
	if count != 7 {
		t.Fatalf("ExecuteCountProbe() count = %d, want 7", count)
	}
	if got := inner.countProbeCount(); got != 1 {
		t.Fatalf("inner.ExecuteCountProbe calls = %d, want 1", got)
	}
	if fe.OnceThenSucceedFired() {
		t.Fatal("OnceThenSucceedFired() = true, want false: ExecuteCountProbe must not consume the scripted fault")
	}
}

// TestFaultingExecutorExecuteCountProbeErrorsWhenInnerLacksCountSupport
// proves ExecuteCountProbe fails closed with
// errFaultingExecutorInnerNoCountProbe (not a silent zero) when the wrapped
// executor does not implement cypher.CountProbeExecutor.
func TestFaultingExecutorExecuteCountProbeErrorsWhenInnerLacksCountSupport(t *testing.T) {
	t.Parallel()

	inner := newFaultExecuteOnlyRecordingExecutor()
	fe := mustFaultingExecutor(t, inner, faultreplay.Script{Version: faultreplay.CurrentVersion}, "")
	count, err := fe.ExecuteCountProbe(context.Background(), cypher.Statement{Cypher: "MATCH (r) RETURN count(r)"})
	if !errors.Is(err, errFaultingExecutorInnerNoCountProbe) {
		t.Fatalf("ExecuteCountProbe() error = %v, want errFaultingExecutorInnerNoCountProbe", err)
	}
	if count != 0 {
		t.Fatalf("count = %d on an unsupported probe, want 0", count)
	}
}
