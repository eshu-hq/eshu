// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// orderRecordingExecutor implements Executor and GroupExecutor while
// recording every call in order, so tests can pin the commit sequence the
// writer uses for multi-statement claims.
type orderRecordingExecutor struct {
	calls []recordedCall
}

type recordedCall struct {
	grouped bool
	cyphers []string
}

func (e *orderRecordingExecutor) Execute(_ context.Context, stmt Statement) error {
	e.calls = append(e.calls, recordedCall{grouped: false, cyphers: []string{stmt.Cypher}})
	return nil
}

func (e *orderRecordingExecutor) ExecuteGroup(_ context.Context, stmts []Statement) error {
	call := recordedCall{grouped: true}
	for _, stmt := range stmts {
		call.cyphers = append(call.cyphers, stmt.Cypher)
	}
	e.calls = append(e.calls, call)
	return nil
}

func artifactRowForOrderTest() reducer.SharedProjectionIntentRow {
	row := keeperRepoDependencyRow("order", 0)
	row.IntentID = "order-intent-0"
	return row
}

// TestWriteEdgesRepoDependencyArtifactFollowsMainCommit pins the #6184 run15
// fix structurally: the evidence-artifact statements must not share a
// managed transaction with the main statements that MERGE their endpoint
// nodes. On NornicDB a MATCH in a later statement of the same managed txn
// does not see those in-transaction MERGEs, so the co-located artifact batch
// silently writes nothing while the call succeeds (#5410/#4367 family).
// Artifact statements therefore execute after the main group commits, via
// sequential calls.
func TestWriteEdgesRepoDependencyArtifactFollowsMainCommit(t *testing.T) {
	t.Parallel()

	executor := &orderRecordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	if _, err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency,
		[]reducer.SharedProjectionIntentRow{artifactRowForOrderTest()}, "resolver/cross-repo"); err != nil {
		t.Fatalf("WriteEdges errored: %v", err)
	}

	mainGroupIdx, firstArtifactIdx := -1, -1
	for i, call := range executor.calls {
		for _, cypher := range call.cyphers {
			if strings.Contains(cypher, "EvidenceArtifact") {
				if firstArtifactIdx < 0 {
					firstArtifactIdx = i
				}
			} else if strings.Contains(cypher, "DEPLOYS_FROM") {
				if call.grouped && mainGroupIdx < 0 {
					mainGroupIdx = i
				}
			}
		}
	}
	if mainGroupIdx < 0 {
		t.Fatalf("no grouped main DEPLOYS_FROM statement executed: %+v", executor.calls)
	}
	if firstArtifactIdx < 0 {
		t.Fatalf("no evidence-artifact statement executed: %+v", executor.calls)
	}
	if firstArtifactIdx == mainGroupIdx {
		t.Fatalf("artifact statements share the main managed transaction (call %d); they must execute after it commits", mainGroupIdx)
	}
	if firstArtifactIdx < mainGroupIdx {
		t.Fatalf("artifact statements (call %d) execute before the main group (call %d)", firstArtifactIdx, mainGroupIdx)
	}
	for _, call := range executor.calls[firstArtifactIdx:] {
		if call.grouped {
			for _, cypher := range call.cyphers {
				if strings.Contains(cypher, "EvidenceArtifact") {
					t.Fatalf("artifact statement executed inside a managed group; use sequential calls: %.80q", cypher)
				}
			}
		}
	}
}
