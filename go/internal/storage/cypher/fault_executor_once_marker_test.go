// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package cypher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
)

// TestFaultingExecutorWritesOnceFiredMarker is the #5974 proof: when the
// once-fault fires, the executor records it in a file, so a gate running in a
// separate process can prove the fault genuinely ran without reading the
// reducer's captured stderr.
//
// The stderr route is what #5974 is about: an earlier log-grep assertion was
// abandoned after the injected-failure line reached the captured file a
// minute-plus after the event in CI, and cell_failgraphwrite_sql then reused
// that same technique and went inert in CI for the same reason.
func TestFaultingExecutorWritesOnceFiredMarker(t *testing.T) {
	t.Parallel()

	sentinel := filepath.Join(t.TempDir(), "script.json.restart-sentinel")
	marker := sentinel + onceFiredMarkerSuffix

	inner := &faultRecordingExecutor{}
	match := "MERGE (source)-[rel:QUERIES_TABLE]->(target)"
	script := onceThenSucceedScript(faultreplay.LaneQueueRetry, nil, &match)
	fe := mustFaultingExecutor(t, inner, script, sentinel)

	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker must not exist before the fault fires; stat err = %v", err)
	}

	// A non-matching write must not create the marker: the marker means "the
	// fault fired", not "a graph write happened".
	if err := fe.Execute(context.Background(), Statement{Cypher: "MERGE (r:CloudResource) RETURN r"}); err != nil {
		t.Fatalf("non-matching write: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("marker was written for a write the fault did not target")
	}

	// The matching write fires the fault and must leave the marker behind.
	err := fe.Execute(context.Background(), Statement{Cypher: match + " SET rel.confidence = 0.95"})
	if err == nil {
		t.Fatal("expected the scripted fault to fire on the matching write")
	}
	raw, readErr := os.ReadFile(marker) // #nosec G304 -- test-local temp path
	if readErr != nil {
		t.Fatalf("marker missing after the fault fired: %v", readErr)
	}
	body := string(raw)
	if !strings.Contains(body, "lane="+faultreplay.LaneQueueRetry) {
		t.Errorf("marker does not name the lane; got %q", body)
	}
	if !strings.Contains(body, "QUERIES_TABLE") {
		t.Errorf("marker does not name the operation it hit; got %q", body)
	}
}

// TestFaultingExecutorOnceMarkerSkippedWithoutSentinelPath keeps the in-process
// tests working: they pass an empty sentinel path and assert through
// OnceThenSucceedFired, so an empty path must simply skip the marker rather
// than erroring or writing to a stray relative path.
func TestFaultingExecutorOnceMarkerSkippedWithoutSentinelPath(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{}
	script := onceThenSucceedScript(faultreplay.LaneQueueRetry, intPtr(1), nil)
	fe := mustFaultingExecutor(t, inner, script, "")

	if err := fe.Execute(context.Background(), Statement{Cypher: "MERGE (a) RETURN a"}); err == nil {
		t.Fatal("expected the scripted fault to fire")
	}
	if !fe.OnceThenSucceedFired() {
		t.Fatal("OnceThenSucceedFired must still report the firing without a marker path")
	}
	if fe.onceFiredPath != "" {
		t.Errorf("onceFiredPath = %q, want empty when no sentinel path was given", fe.onceFiredPath)
	}
}

// TestFaultingExecutorMarkerNamesTheMatchingStatementInAGroup is the regression
// for the defect review caught: the marker used to record stmts[0], while
// onceMatches scans the whole slice. ExecuteGroup passes several statements and
// the targeted one is frequently not first, so the marker named a statement the
// fault did not target.
//
// That is not cosmetic. The gate asserts on the recorded operation to tell
// "the fault fired on the targeted write" apart from "it fired on some other
// write", so a wrong name is a wrong verdict in either direction: a real hit
// reported as the wrong target, or a wrong hit accepted as the right one.
func TestFaultingExecutorMarkerNamesTheMatchingStatementInAGroup(t *testing.T) {
	t.Parallel()

	sentinel := filepath.Join(t.TempDir(), "script.json.restart-sentinel")
	marker := sentinel + onceFiredMarkerSuffix

	inner := &faultRecordingExecutor{}
	match := "MERGE (source)-[rel:QUERIES_TABLE]->(target)"
	script := onceThenSucceedScript(faultreplay.LaneQueueRetry, nil, &match)
	fe := mustFaultingExecutor(t, inner, script, sentinel)

	// The targeted statement is deliberately last, behind two decoys.
	err := fe.ExecuteGroup(context.Background(), []Statement{
		{Cypher: "MERGE (r:CloudResource {uid: row.uid}) RETURN r"},
		{Cypher: "MERGE (a)-[:HAS_COLUMN]->(b) RETURN a"},
		{Cypher: match + " SET rel.confidence = 0.95"},
	})
	if err == nil {
		t.Fatal("expected the scripted fault to fire on the group containing the targeted statement")
	}

	raw, readErr := os.ReadFile(marker) // #nosec G304 -- test-local temp path
	if readErr != nil {
		t.Fatalf("marker missing after the fault fired: %v", readErr)
	}
	body := string(raw)
	if !strings.Contains(body, "QUERIES_TABLE") {
		t.Errorf("marker does not name the statement that matched; got %q", body)
	}
	if strings.Contains(body, "CloudResource") {
		t.Errorf("marker named the first statement instead of the matching one; got %q", body)
	}
}
