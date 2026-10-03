// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// The #7129 byte bars run on the #7168 calibrated base, newDeadCodeBudgetStore:
// 400 candidates with 60-byte docstrings and a full suppressed bucket, default
// arguments (limit 25). The thin-base fixtures in
// dispatch_cross_repo_handles_budget_test.go only isolate the evidence term;
// they are not a statement about a real repository's reply size.
const (
	// handlesEvidenceContributionCeiling is 17.5% of the 262,144-byte budget:
	// the most the per-entity evidence plus the boundary list may add to the
	// calibrated base under handles.
	handlesEvidenceContributionCeiling = 45875
	// calibratedEvidenceRows rows carry 40 distinct-repository items each; the
	// next row has none and falls back to the 60-item boundary list.
	calibratedEvidenceRows = 24
)

// twoCopyBytes is the est2x metric the dispatch budget enforces: the reply with
// both wire copies, even when the dispatcher already fell back to resource-only
// (where estimateResponseBytes sees one copy).
func twoCopyBytes(t *testing.T, result *dispatchResult) int {
	t.Helper()

	resourceOnly := result.ResourceOnly
	result.ResourceOnly = false
	defer func() { result.ResourceOnly = resourceOnly }()
	encoded, err := json.Marshal(renderToolResult(result.ToolName, result))
	if err != nil {
		t.Fatalf("marshal reply: %v", err)
	}
	return len(encoded)
}

// replyClass ranks the three delivery outcomes: 0 structuredContent delivered,
// 1 full payload as a resource only, 2 mcp_response_over_budget.
func replyClass(result *dispatchResult) int {
	switch {
	case result.IsError:
		return 2
	case result.ResourceOnly:
		return 1
	default:
		return 0
	}
}

func replyClassName(class int) string {
	return [...]string{"delivered", "resource-only", "over-budget"}[class]
}

// calibratedPathologicalStore is the calibrated base with the pathological
// evidence: 24 rows x 40 items in 40 distinct consumer repositories, one
// boundary-fallback row, and a 60-item boundary list. docLen > 0 replaces the
// 60-byte docstring.
func calibratedPathologicalStore(docLen int, withEvidence bool) *crossRepoHandlesStore {
	base := newDeadCodeBudgetStore(0)
	if docLen > 0 {
		padding := strings.Repeat("x", docLen)
		for id, entity := range base.Entities {
			metadata := map[string]any{}
			for key, value := range entity.Metadata {
				metadata[key] = value
			}
			metadata["docstring"] = padding
			entity.Metadata = metadata
			base.Entities[id] = entity
		}
	}
	store := &crossRepoHandlesStore{
		deadCodeBudgetStore: base,
		evidenceFor:         func(string) []deadcode.CrossRepoDeadCodeEvidence { return nil },
	}
	if !withEvidence {
		return store
	}
	base.RelationshipReadModel.Available = true
	base.RelationshipReadModel.Relationships = boundaryRelationships(handlesFixtureBoundaryItems)
	store.evidenceFor = func(entityID string) []deadcode.CrossRepoDeadCodeEvidence {
		var row int
		if _, err := fmt.Sscanf(entityID, "ts-%d", &row); err != nil || row >= calibratedEvidenceRows {
			return nil
		}
		items := make([]deadcode.CrossRepoDeadCodeEvidence, 0, handlesFixtureItemsPerRow)
		for index := 0; index < handlesFixtureItemsPerRow; index++ {
			items = append(items, handlesFixtureItem(row, index, index))
		}
		return items
	}
	return store
}

// TestFindCrossRepoDeadCodeHandlesCalibratedBaseBars is the #7129 acceptance
// bar on the calibrated base. (a) The pathological evidence adds at most 17.5%
// of the budget over the same base with no evidence. (b) The whole reply fits
// the budget with both copies and structuredContent is delivered.
func TestFindCrossRepoDeadCodeHandlesCalibratedBaseBars(t *testing.T) {
	t.Parallel()

	baseline := dispatchCrossRepoHandlesFixture(t, calibratedPathologicalStore(0, false), nil)
	if replyClass(baseline) != 0 {
		t.Fatalf("calibrated base with no evidence is %s, want delivered", replyClassName(replyClass(baseline)))
	}
	pathological := dispatchCrossRepoHandlesFixture(t, calibratedPathologicalStore(0, true), nil)

	baseBytes := twoCopyBytes(t, baseline)
	withBytes := twoCopyBytes(t, pathological)
	t.Logf("calibrated base no evidence est2x=%d; with pathological evidence (handles) est2x=%d (%.1f%% of %d) class=%s contribution=%d ceiling=%d",
		baseBytes, withBytes, float64(withBytes)*100/float64(defaultToolResponseByteBudget), defaultToolResponseByteBudget,
		replyClassName(replyClass(pathological)), withBytes-baseBytes, handlesEvidenceContributionCeiling)

	if got := withBytes - baseBytes; got > handlesEvidenceContributionCeiling {
		t.Errorf("(a) evidence contribution = %d bytes, want <= %d (17.5%% of the budget)", got, handlesEvidenceContributionCeiling)
	}
	if withBytes > defaultToolResponseByteBudget {
		t.Errorf("(b) est2x = %d, want <= %d", withBytes, defaultToolResponseByteBudget)
	}
	if pathological.ResourceOnly || pathological.IsError {
		t.Errorf("(b) reply class = %s, want structuredContent delivered", replyClassName(replyClass(pathological)))
	}
	data, _ := pathological.Envelope.Data.(map[string]any)
	// Fixture-honesty guard: the ts-%d coupling in calibratedPathologicalStore must
	// actually attach per-row evidence, or this bar passes vacuously.
	buckets, _ := data["candidate_buckets"].(map[string]any)
	groups := 0
	for _, name := range []string{"dead", "live_by_consumer", "unknown"} {
		rows, _ := buckets[name].([]any)
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			groups += int(numberValue(row["consumer_evidence_group_count"]))
		}
	}
	if groups == 0 {
		t.Errorf("pathological store carries no evidence groups; the Sscanf coupling to newDeadCodeBudgetStore is broken")
	}
	if data["evidence_detail"] != "handles" {
		t.Errorf("data.evidence_detail = %#v, want handles at MCP default", data["evidence_detail"])
	}
}

// heavyRowStore is the reviewer's heavy base: long paths and names, decorators,
// docstrings the read-time clip cuts to 512 bytes, and python suppressed rows
// that fill the suppressed bucket with the same weight. It is deliberately
// heavier than the calibrated base; the bar is that handles never makes its
// outcome class worse than full does.
func heavyRowStore(withEvidence bool) *crossRepoHandlesStore {
	store := newCrossRepoHandlesStore(func(index int) int { return index })
	doc := strings.Repeat("Handles the payment settlement reconciliation callback. ", 2000/56+1)[:2000]
	rows := make([]map[string]any, 0, 2*(handlesFixtureRows+1))
	for index := 0; index <= handlesFixtureRows; index++ {
		entityID := handlesFixtureProducerID(index)
		entity := store.Entities[entityID]
		entity.RelativePath = fmt.Sprintf("services/payments/settlement/reconciliation/handlers/v2/settlementReconciliationHandler%03d.ts", index)
		entity.Metadata = map[string]any{"docstring": doc, "decorators": []any{"@Injectable()", "@Traced('settlement')"}}
		store.Entities[entityID] = entity
		rows = append(rows, map[string]any{
			"entity_id": entityID, "name": fmt.Sprintf("handleSettlementReconciliationCallback%03d", index),
			"labels": []any{"Function"}, "file_path": entity.RelativePath,
			"repo_id": deadCodeBudgetProducerRepoID, "repo_name": "payments-lib",
			"language": "typescript", "start_line": int64(10), "end_line": int64(20),
		})
		suppressedID := fmt.Sprintf("content-entity:e_%012x", 0x6b0000000000+index)
		suppressedPath := fmt.Sprintf("services/payments/api/routes/settlement_route_%03d.py", index)
		store.Entities[suppressedID] = querycontract.EntityContent{
			EntityID: suppressedID, RepoID: deadCodeBudgetProducerRepoID, RelativePath: suppressedPath,
			EntityType: "Function", EntityName: suppressedID, StartLine: 1, EndLine: 9, Language: "python",
			SourceCache: "def f(): pass",
			Metadata: map[string]any{
				"docstring": doc, "dead_code_root_kinds": []string{"python.flask_route_decorator"},
				"decorators": []any{"@app.route('/settle')"},
			},
		}
		rows = append(rows, map[string]any{
			"entity_id": suppressedID, "name": fmt.Sprintf("settlement_route_%03d", index),
			"labels": []any{"Function"}, "file_path": suppressedPath, "repo_id": deadCodeBudgetProducerRepoID,
			"repo_name": "payments-lib", "language": "python", "start_line": int64(1), "end_line": int64(9),
		})
	}
	store.rows = rows
	if !withEvidence {
		store.evidenceFor = func(string) []deadcode.CrossRepoDeadCodeEvidence { return nil }
		store.RelationshipReadModel.Relationships = nil
	}
	return store
}

// TestFindCrossRepoDeadCodeHandlesNeverWorsensHeavyRowOutcome pins that, on a
// base far heavier than the calibrated one, handles (the MCP default) never
// turns an outcome class worse than the full payload gives: a delivered reply
// stays delivered, a resource-only reply stays resource-only. The row base
// echoes each docstring about six times, so on such a repository the reply can
// still be resource-only; that is the echo dedupe's job, not the evidence cap's.
func TestFindCrossRepoDeadCodeHandlesNeverWorsensHeavyRowOutcome(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		evidence bool
	}{
		{name: "pathological evidence", evidence: true},
		{name: "no evidence", evidence: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			full := dispatchCrossRepoHandlesFixture(t, heavyRowStore(tc.evidence), map[string]any{"evidence_detail": "full"})
			handles := dispatchCrossRepoHandlesFixture(t, heavyRowStore(tc.evidence), nil)
			t.Logf("heavy rows (%s): full=%s handles=%s handles est2x=%d", tc.name,
				replyClassName(replyClass(full)), replyClassName(replyClass(handles)), twoCopyBytes(t, handles))
			if replyClass(handles) > replyClass(full) {
				t.Fatalf("handles outcome %s is worse than full outcome %s", replyClassName(replyClass(handles)), replyClassName(replyClass(full)))
			}
		})
	}
}
