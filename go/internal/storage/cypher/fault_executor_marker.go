// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package cypher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
)

// The once-fired marker: how a gate running in a separate process learns that
// the scripted fail-graph-write-once-then-succeed fault actually fired, and
// which statement it hit.
//
// Split out of fault_executor.go to keep that file under the repo's 500-line
// cap. This is the whole marker unit -- the suffix, the match resolution, and
// the write -- so the pieces stay together.
//
// Why a file rather than the reducer's log: a shell gate cannot call
// OnceThenSucceedFired across a process boundary, and the log fallback races
// the logger's flush. scripts/lib/ifa_fault_injection_common.sh records an
// earlier assertion abandoned for that reason, and #5974 is the same bug one
// level up -- a log poll that read "the fault never fired" as "the log line
// arrived late" for weeks.

// onceFiredMarkerSuffix names the file the once-fault writes when it fires,
// alongside the restart sentinel. See FaultingExecutor.onceFiredPath (#5974).
const onceFiredMarkerSuffix = ".once-fired"

// onceMatchedStatement reports whether this call is the targeted one and, when
// the fault matches by substring, returns the statement that actually matched.
//
// The distinction matters for the marker: ExecuteGroup and ExecutePhaseGroup
// pass a whole slice, and the matching statement is not necessarily the first.
// Recording stmts[0] there would name a statement the fault did not target, and
// the gate asserts on that name to tell "fired on the targeted write" apart
// from "fired on some other write" -- so a wrong name is a wrong verdict, in
// either direction.
//
// Ordinal-matched faults target the call rather than a statement, so they
// return the first statement as the best available description.
func (fe *FaultingExecutor) onceMatchedStatement(ordinal int, stmts []Statement) (string, bool) {
	if fe.onceMatch != "" {
		for i := range stmts {
			if strings.Contains(stmts[i].Cypher, fe.onceMatch) {
				return stmts[i].Cypher, true
			}
		}
		return "", false
	}
	if ordinal != fe.onceOrdinal {
		return "", false
	}
	if len(stmts) > 0 {
		return stmts[0].Cypher, true
	}
	return "", true
}

// writeOnceFiredMarker records that the once-fault fired, naming the statement
// it hit.
//
// It does NOT swallow the write error. An earlier version did, with a comment
// claiming a missing marker meant "the fault never fired" -- which was exactly
// wrong, and in the same way #5974's original defect was wrong. A silently
// failed write is byte-identical to a fault that never fired, so swallowing the
// error rebuilds the ambiguity this marker exists to remove, one level down.
//
// A failed write is reported on stderr with a distinctive prefix the gate names
// in its own failure message, so "no marker" can be told apart from "marker
// write failed" by looking at one line of reducer output instead of guessing.
//
// Written with os.WriteFile (create-truncate-write-close) from the injecting
// goroutine before the fault is returned, so it is on disk by the time any
// observer can see the fault's downstream effect.
func (fe *FaultingExecutor) writeOnceFiredMarker(ordinal int, operation string) {
	if fe.onceFiredPath == "" {
		return
	}
	record := fmt.Sprintf("lane=%s ordinal=%d\noperation=%s\n", fe.onceLane, ordinal, operation)
	// #nosec G306 -- marker is a local/CI fault-injection coordination flag,
	// same trust boundary as the restart sentinel written above.
	if err := os.WriteFile(fe.onceFiredPath, []byte(record), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v (path=%s)\n", OnceFiredMarkerWriteFailedPrefix, err, fe.onceFiredPath)
	}
}

// OnceFiredMarkerWriteFailedPrefix is the stderr prefix writeOnceFiredMarker
// emits when it cannot write the marker. The fault-injection gate names this
// string in its "no marker found" failure so an operator is told to look for it
// rather than concluding the fault never fired.
const OnceFiredMarkerWriteFailedPrefix = "ifa fault: once-fired marker write failed"

const (
	restartTriggerRecordSuffix   = ".trigger.json"
	restartSurfaceExecuteGroup   = "execute_group"
	restartSurfaceExecutePhase   = "execute_phase_group"
	ifaFaultSentinelPollInterval = 200 * time.Millisecond
	maxRestartTriggerStatements  = 1_000
	maxRestartTriggerRecordBytes = 64 << 20
)

// restartTriggerRecord is the JSON representation of the test-only write group
// whose executor call returned success immediately before the restart sentinel
// became visible. It identifies one acknowledged executor request at the fault
// boundary; it does not establish backend durability, reconstruct every
// submitted group, or distinguish projection, scheduling, and backend causes
// by itself.
type restartTriggerRecord struct {
	GroupOrdinal int         `json:"group_ordinal"`
	Surface      string      `json:"surface"`
	Statements   []Statement `json:"statements"`
}

// writeRestartTriggerRecord atomically publishes the acknowledged executor
// group before the restart sentinel. A reader that observes the sentinel can
// therefore inspect that exact executor input at the fault boundary.
func (fe *FaultingExecutor) writeRestartTriggerRecord(
	groupOrdinal int,
	surface string,
	statements []Statement,
) error {
	if len(statements) > maxRestartTriggerStatements {
		return fmt.Errorf(
			"restart trigger group has %d statements, limit is %d",
			len(statements),
			maxRestartTriggerStatements,
		)
	}
	record := restartTriggerRecord{
		GroupOrdinal: groupOrdinal,
		Surface:      surface,
		Statements:   statements,
	}
	contents, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal restart trigger record: %w", err)
	}
	if len(contents) > maxRestartTriggerRecordBytes {
		return fmt.Errorf(
			"restart trigger record has %d bytes, limit is %d",
			len(contents),
			maxRestartTriggerRecordBytes,
		)
	}
	contents = append(contents, '\n')

	path := fe.sentinelPath + restartTriggerRecordSuffix
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("create restart trigger record %q: %w", path, err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write restart trigger record %q: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close restart trigger record %q: %w", path, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish restart trigger record %q: %w", path, err)
	}
	removeTemporary = false
	return nil
}

// maybeRestartAfterGroup fires after the configured executor group returns
// success, publishes its trigger record, then exposes the sentinel and waits
// for the harness to restart the backend and remove that sentinel.
func (fe *FaultingExecutor) maybeRestartAfterGroup(
	ctx context.Context,
	groupOrdinal int,
	surface string,
	statements []Statement,
) error {
	if fe.restartAfterGroups == 0 || groupOrdinal != fe.restartAfterGroups {
		return nil
	}
	if !fe.restartFired.CompareAndSwap(false, true) {
		return nil
	}
	captureStarted := time.Now()
	if err := fe.writeRestartTriggerRecord(groupOrdinal, surface, statements); err != nil {
		return fmt.Errorf("ifa fault: %s: %w", faultreplay.KindRestartBackendBetweenPhaseGroups, err)
	}
	fmt.Fprintf(
		os.Stderr,
		"ifa fault: restart trigger captured group_ordinal=%d statements=%d duration=%s\n",
		groupOrdinal,
		len(statements),
		time.Since(captureStarted),
	)
	// #nosec G306 -- sentinel is a local/CI fault-injection coordination flag
	// that the gate must be able to remove.
	if err := os.WriteFile(fe.sentinelPath, []byte("waiting-for-backend-restart\n"), 0o644); err != nil {
		return fmt.Errorf(
			"ifa fault: %s: write sentinel %q: %w",
			faultreplay.KindRestartBackendBetweenPhaseGroups,
			fe.sentinelPath,
			err,
		)
	}
	ticker := time.NewTicker(ifaFaultSentinelPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"ifa fault: %s: canceled waiting for sentinel %q removal: %w",
				faultreplay.KindRestartBackendBetweenPhaseGroups,
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
