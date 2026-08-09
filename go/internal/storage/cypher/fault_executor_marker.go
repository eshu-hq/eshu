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

// observedOpsSuffix names the file recording every distinct statement shape the
// executor saw while a substring-matched once-fault was armed (#5974).
const observedOpsSuffix = ".observed-operations"

// recordObservedOperations appends each newly-seen statement to
// observedOpsPath, in full.
//
// This exists because "the fault never fired" is only half an observation. The
// other half -- what actually ran, so the anchor can be compared against it --
// was never recorded, and #5974 stalled on exactly that gap.
//
// The FULL statement is recorded, not its first line. The first version of this
// kept first lines only, on the stated reasoning that "the MERGE clause the
// anchor targets is what matters" -- which was backwards. Every SQL relationship
// template opens with `UNWIND $rows AS row` and puts the MERGE on line 4
// (canonical.go:161-164), so first-line recording collapsed every distinct
// statement to one shape and discarded the exact line the anchor targets. CI
// printed a single `UNWIND $rows AS row` and answered nothing.
//
// Dedup is by full text, so the volume is still bounded by distinct statement
// shapes rather than call count: a drive issuing thousands of writes records a
// handful of templates. Matching itself has always used the whole statement
// (strings.Contains over stmt.Cypher), so recording the whole statement is what
// makes the record comparable to the thing being matched.
func (fe *FaultingExecutor) recordObservedOperations(stmts []Statement) {
	if fe.observedOpsPath == "" || fe.onceMatch == "" {
		return
	}
	fe.observedOpsMu.Lock()
	defer fe.observedOpsMu.Unlock()
	if fe.observedOps == nil {
		fe.observedOps = make(map[string]struct{})
	}
	for i := range stmts {
		full := strings.TrimSpace(stmts[i].Cypher)
		if full == "" {
			continue
		}
		if _, seen := fe.observedOps[full]; seen {
			continue
		}
		fe.observedOps[full] = struct{}{}
		// #nosec G304,G302 -- gate-local coordination file, same trust boundary
		// as the marker and restart sentinel.
		f, err := os.OpenFile(fe.observedOpsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v (path=%s)\n", OnceFiredMarkerWriteFailedPrefix, err, fe.observedOpsPath)
			return
		}
		fmt.Fprintf(f, "--- statement ---\n%s\n", full)
		_ = f.Close()
	}
}
