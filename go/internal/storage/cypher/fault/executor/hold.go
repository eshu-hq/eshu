// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package executor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// The held-statement marker: how a gate running in a separate process learns
// that the scripted hold-statement-before-delegation fault actually engaged,
// and which statement it engaged on.
//
// Split out of fault.go to keep that file under the repo's 500-line cap. This
// is the whole hold unit -- the suffix, the match resolution, the marker
// write, and the block -- so the pieces stay together.
//
// Why a file rather than the reducer's log: same reason as the once-fired
// marker (marker.go, #5974) -- a shell gate cannot call HoldFired across a
// process boundary, and the log fallback races the logger's flush. The hold
// marker is written synchronously by the goroutine engaging the hold, before
// it blocks, so a reader that sees the fault's effect (a claimed row that
// never completes) can always see the marker too.
//
// The marker path derives from the restart sentinel's path (like the
// once-fired marker): <sentinel>.statement-held, so the gate derives it from
// the fault-script path the same way it already does for the once marker.

// holdFiredMarkerSuffix names the file the hold writes when it engages,
// alongside the restart sentinel. See FaultingExecutor.holdFiredPath.
const holdFiredMarkerSuffix = ".statement-held"

// HoldMarkerWriteFailedPrefix is the stderr prefix writeHoldMarker emits when
// it cannot write the marker. The fault-injection gate names this string in
// its "no marker found" failure so "no marker" can be told apart from "marker
// write failed" by looking at one line of reducer output instead of guessing.
const HoldMarkerWriteFailedPrefix = "ifa fault: hold marker write failed"

// holdMatchedStatement reports whether this call carries a statement the
// scripted hold engages, and, when it does, the exact statement text that
// matched.
//
// The distinction matters for the marker: ExecuteGroup and ExecutePhaseGroup
// pass a whole slice, and the matching statement is not necessarily the
// first. Recording stmts[0] there would name a statement the hold did not
// engage, and the gate asserts on that name to tell "held on the targeted
// write" apart from "held on some other write" -- so a wrong name is a wrong
// verdict, in either direction.
func (fe *FaultingExecutor) holdMatchedStatement(stmts []cypher.Statement) (string, bool) {
	if fe.holdMatch == "" {
		return "", false
	}
	for i := range stmts {
		if strings.Contains(stmts[i].Cypher, fe.holdMatch) {
			return stmts[i].Cypher, true
		}
	}
	return "", false
}

// writeHoldMarker records that the hold engaged, naming the statement it
// holds.
//
// It does NOT swallow the write error (same reason as writeOnceFiredMarker in
// marker.go): a silently failed write is byte-identical to a hold that never
// engaged, rebuilding the ambiguity this marker exists to remove.
//
// Written with os.WriteFile (create-truncate-write-close) from the engaging
// goroutine before it blocks, so the marker is on disk by the time any
// observer can see the hold's downstream effect (a claimed row that never
// completes).
func (fe *FaultingExecutor) writeHoldMarker(ordinal int, operation string) {
	if fe.holdFiredPath == "" {
		return
	}
	record := fmt.Sprintf("kind=%s ordinal=%d\noperation=%s\n", faultreplay.KindHoldStatementBeforeDelegation, ordinal, operation)
	// #nosec G306 -- marker is a local/CI fault-injection coordination flag,
	// same trust boundary as the restart sentinel written above.
	if err := os.WriteFile(fe.holdFiredPath, []byte(record), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v (path=%s)\n", HoldMarkerWriteFailedPrefix, err, fe.holdFiredPath)
	}
}

// applyHoldFault records one hold-statement-before-delegation script op's
// effect on fe, or returns a construction-time error for an ambiguous or
// unusable script: more than one hold per run (the second would silently
// never fire), a blank operation_match (no statement to engage), a lane
// target (a hold has no retry lane -- it blocks before delegation), or an
// empty sentinel path (nothing to block on -- a hold without a sentinel is a
// hold that never holds).
func (fe *FaultingExecutor) applyHoldFault(f faultreplay.FaultOp, sentinelPath string) error {
	if fe.holdMatch != "" {
		return fmt.Errorf("faulting executor: script names more than one %s fault; only one is supported per run", faultreplay.KindHoldStatementBeforeDelegation)
	}
	if f.Trigger.OperationMatch == nil || strings.TrimSpace(*f.Trigger.OperationMatch) == "" {
		return fmt.Errorf("faulting executor: %s requires trigger.operation_match", faultreplay.KindHoldStatementBeforeDelegation)
	}
	if f.Target.Lane != "" {
		return fmt.Errorf("faulting executor: %s takes no target.lane, got %q", faultreplay.KindHoldStatementBeforeDelegation, f.Target.Lane)
	}
	if strings.TrimSpace(sentinelPath) == "" {
		return fmt.Errorf("faulting executor: %s requires a non-empty sentinel path", faultreplay.KindHoldStatementBeforeDelegation)
	}
	fe.holdMatch = *f.Trigger.OperationMatch
	fe.sentinelPath = sentinelPath
	// Derived from the restart sentinel's path rather than taking a new
	// constructor argument, so the gate derives the marker path from the
	// fault-script path the same way it already does for the once marker.
	fe.holdFiredPath = sentinelPath + holdFiredMarkerSuffix
	return nil
}

// HoldFired reports whether the scripted hold-statement-before-delegation
// fault (if any) has engaged. Tests and the gate's marker assert use this
// (in-process) and the marker file (cross-process) respectively to prove the
// hold genuinely engaged rather than silently no-op'ing.
func (fe *FaultingExecutor) HoldFired() bool {
	return fe.holdMatch != "" && fe.holdFired.Load()
}

// maybeHoldBeforeDelegation engages the scripted
// hold-statement-before-delegation fault the first time a call carries a
// statement whose Cypher text contains the scripted substring. A sync/atomic
// CompareAndSwap gate makes engaging safe under concurrent callers: exactly
// one caller observes the held attempt, even if many match; the CAS losers
// delegate immediately.
//
// On engaging, it synchronously writes the hold marker naming the matched
// statement, then blocks on sentinel-removal/ctx.Done() (the restart-fault
// pattern from marker.go) and never delegates while held: a nil return means
// the sentinel went away and the caller must now delegate normally; a
// non-nil return means the hold was canceled and the caller must return the
// error without delegating.
func (fe *FaultingExecutor) maybeHoldBeforeDelegation(
	ctx context.Context,
	ordinal int,
	stmts []cypher.Statement,
) error {
	matched, ok := fe.holdMatchedStatement(stmts)
	if !ok {
		return nil
	}
	if !fe.holdFired.CompareAndSwap(false, true) {
		return nil
	}
	// Record the engagement durably before blocking, so the gate's proof
	// does not depend on the reducer's stderr reaching its captured file.
	fe.writeHoldMarker(ordinal, matched)
	ticker := time.NewTicker(ifaFaultSentinelPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"ifa fault: %s: canceled waiting for sentinel %q removal: %w",
				faultreplay.KindHoldStatementBeforeDelegation,
				fe.sentinelPath,
				ctx.Err(),
			)
		case <-ticker.C:
			if _, err := os.Stat(fe.sentinelPath); os.IsNotExist(err) {
				return nil
			}
		}
	}
}
