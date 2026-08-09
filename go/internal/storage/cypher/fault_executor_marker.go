// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package cypher

import (
	"fmt"
	"os"
	"strings"
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
