// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"testing"
	"time"

	codecall "github.com/eshu-hq/eshu/go/internal/reducer/code/call"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// statefulCodeCallProjectionEdgeWriter models the graph as an edge set so the
// F4 heal test can observe edge loss and recovery across cycles. Retract
// deletes every edge of the retracted scope; write adds one edge per row keyed
// by caller/callee identity.
type statefulCodeCallProjectionEdgeWriter struct {
	edges map[string]struct{}
}

func codeCallHealEdgeKey(row sharedintent.Row) string {
	repo, _ := row.Payload["repo_id"].(string)
	caller, _ := row.Payload["caller_entity_id"].(string)
	callee, _ := row.Payload["callee_entity_id"].(string)
	return repo + "|" + caller + "->" + callee
}

// codeCallHealRetractRepoID resolves the retracted repository the way the real
// writer does: retract rows carry only RepositoryID plus a repo_id payload
// (buildCodeCallRetractRows), so the retract is repo-scoped.
func codeCallHealRetractRepoID(row sharedintent.Row) string {
	if row.RepositoryID != "" {
		return row.RepositoryID
	}
	repo, _ := row.Payload["repo_id"].(string)
	return repo
}

func (w *statefulCodeCallProjectionEdgeWriter) RetractEdges(_ context.Context, _ string, rows []sharedintent.Row, _ string) error {
	if w.edges == nil {
		w.edges = map[string]struct{}{}
	}
	for _, row := range rows {
		prefix := codeCallHealRetractRepoID(row) + "|"
		for key := range w.edges {
			if len(key) > len(prefix) && key[:len(prefix)] == prefix {
				delete(w.edges, key)
			}
		}
	}
	return nil
}

func (w *statefulCodeCallProjectionEdgeWriter) WriteEdges(_ context.Context, _ string, rows []sharedintent.Row, _ string) (sharedintent.WriteReport, error) {
	if w.edges == nil {
		w.edges = map[string]struct{}{}
	}
	for _, row := range rows {
		w.edges[codeCallHealEdgeKey(row)] = struct{}{}
	}
	return sharedintent.WriteReport{}, nil
}

func (w *statefulCodeCallProjectionEdgeWriter) has(scope, caller, callee string) bool {
	_, ok := w.edges[scope+"|"+caller+"->"+callee]
	return ok
}

func quarantineHealCodeCallRow(intentID, genID, callee string, now time.Time) sharedintent.Row {
	return sharedintent.Row{
		IntentID:         intentID,
		ProjectionDomain: reducercontract.DomainCodeCalls,
		PartitionKey:     "caller->" + callee,
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		RepositoryID:     "repo-a",
		SourceRunID:      "run-1",
		GenerationID:     genID,
		Payload: map[string]any{
			"repo_id":          "repo-a",
			"caller_entity_id": "caller",
			"callee_entity_id": callee,
			"evidence_source":  codecall.EvidenceSource,
		},
		CreatedAt: now,
	}
}

// TestCodeCallQuarantinedFileHealsOnNextValidGeneration is the #7736 F4
// regression pin for the accepted transient loss: when a successor generation
// quarantines a file (its fact is absent from the successor's intents), the
// emitted-full-successor drain drops that file's last-valid edges — the F5
// forced retract wipes the scope and the partial successor cannot re-emit the
// missing file. The loss is transient by construction: the next valid
// generation re-emits the file and its cycle restores the edges. This test
// drives that full arc through the real runner: write G, drain G, partial F,
// healing H.
func TestCodeCallQuarantinedFileHealsOnNextValidGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	writer := &statefulCodeCallProjectionEdgeWriter{}

	// Cycle 1: G accepted and projected. Both files' edges land.
	genG := map[string][]sharedintent.Row{"scope-a|repo-a|run-1": {
		quarantineHealCodeCallRow("intent-g-1", "gen-g", "callee-file-1", now),
		quarantineHealCodeCallRow("intent-g-2", "gen-g", "callee-file-2", now),
	}}
	readerG := &fakeCodeCallIntentStore{
		pendingByDomain:     genG["scope-a|repo-a|run-1"],
		pendingByAcceptance: genG,
		leaseGranted:        true,
	}
	runnerG := Runner{
		IntentReader: readerG,
		LeaseManager: readerG,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-g", true),
		Config:       RunnerConfig{PollInterval: 10 * time.Millisecond},
	}
	if _, err := runnerG.processOnce(ctx, now); err != nil {
		t.Fatalf("cycle G processOnce() error = %v", err)
	}
	if !writer.has("repo-a", "caller", "callee-file-1") || !writer.has("repo-a", "caller", "callee-file-2") {
		t.Fatalf("edges after G = %v, want both files present", writer.edges)
	}

	// Cycle 2: G's replayed rows drain under the emitted full successor. The
	// F5 forced retract wipes the scope and nothing writes.
	genGDrain := map[string][]sharedintent.Row{"scope-a|repo-a|run-1": {
		quarantineHealCodeCallRow("intent-g-3", "gen-g", "callee-file-1", now),
		quarantineHealCodeCallRow("intent-g-4", "gen-g", "callee-file-2", now),
	}}
	readerDrain := &drainHistoryCodeCallIntentStore{
		historyAwareCodeCallIntentStore: &historyAwareCodeCallIntentStore{
			fakeCodeCallIntentStore: &fakeCodeCallIntentStore{
				pendingByDomain:     genGDrain["scope-a|repo-a|run-1"],
				pendingByAcceptance: genGDrain,
				leaseGranted:        true,
			},
			hasCompleted: false,
		},
		covered: map[string]struct{}{"gen-g": {}},
	}
	runnerDrain := Runner{
		IntentReader: readerDrain,
		LeaseManager: readerDrain.fakeCodeCallIntentStore,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-g", true),
		Config:       RunnerConfig{PollInterval: 10 * time.Millisecond},
	}
	drainResult, err := runnerDrain.processOnce(ctx, now)
	if err != nil {
		t.Fatalf("drain cycle processOnce() error = %v", err)
	}
	if drainResult.CoveredByFullSuccessorIntents != 2 {
		t.Fatalf("CoveredByFullSuccessorIntents = %d, want 2", drainResult.CoveredByFullSuccessorIntents)
	}
	if len(writer.edges) != 0 {
		t.Fatalf("edges after drain = %v, want empty: the drain drops last-valid edges", writer.edges)
	}

	// Cycle 3: F accepted, but file 2's fact quarantined upstream, so F emits
	// only file 1. The transient loss is visible: file 2 stays absent.
	genF := map[string][]sharedintent.Row{"scope-a|repo-a|run-1": {
		quarantineHealCodeCallRow("intent-f-1", "gen-f", "callee-file-1", now),
	}}
	readerF := &fakeCodeCallIntentStore{
		pendingByDomain:     genF["scope-a|repo-a|run-1"],
		pendingByAcceptance: genF,
		leaseGranted:        true,
	}
	runnerF := Runner{
		IntentReader: readerF,
		LeaseManager: readerF,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-f", true),
		Config:       RunnerConfig{PollInterval: 10 * time.Millisecond},
	}
	if _, err := runnerF.processOnce(ctx, now); err != nil {
		t.Fatalf("cycle F processOnce() error = %v", err)
	}
	if !writer.has("repo-a", "caller", "callee-file-1") {
		t.Fatalf("edges after F = %v, want file 1 present", writer.edges)
	}
	if writer.has("repo-a", "caller", "callee-file-2") {
		t.Fatalf("edges after F = %v, want file 2 absent (quarantined upstream)", writer.edges)
	}

	// Cycle 4: H accepted with both files valid. Its retract/write cycle
	// restores file 2's edges: the loss heals on the next valid generation.
	genH := map[string][]sharedintent.Row{"scope-a|repo-a|run-1": {
		quarantineHealCodeCallRow("intent-h-1", "gen-h", "callee-file-1", now),
		quarantineHealCodeCallRow("intent-h-2", "gen-h", "callee-file-2", now),
	}}
	readerH := &fakeCodeCallIntentStore{
		pendingByDomain:     genH["scope-a|repo-a|run-1"],
		pendingByAcceptance: genH,
		leaseGranted:        true,
	}
	runnerH := Runner{
		IntentReader: readerH,
		LeaseManager: readerH,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-h", true),
		Config:       RunnerConfig{PollInterval: 10 * time.Millisecond},
	}
	if _, err := runnerH.processOnce(ctx, now); err != nil {
		t.Fatalf("cycle H processOnce() error = %v", err)
	}
	if !writer.has("repo-a", "caller", "callee-file-1") || !writer.has("repo-a", "caller", "callee-file-2") {
		t.Fatalf("edges after H = %v, want both files healed", writer.edges)
	}
}
