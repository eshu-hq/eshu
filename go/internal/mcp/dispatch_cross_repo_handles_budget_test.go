// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// The #7129 evidence-isolation fixtures. The boundary-hoist fixture removed the
// evidence every fallback row repeated; what remains is per-entity evidence,
// which the SQL page can return up to 1000 items for. These fixtures drive the
// real dispatch and HTTP handler over that worst case on a THIN candidate base
// (no suppressed rows, no padded docstrings), so the byte count isolates the
// evidence term. They are not the acceptance bar and say nothing about a real
// repository's reply size: the bar is the calibrated-base test in
// dispatch_cross_repo_handles_calibrated_test.go. Identifiers are shaped like
// production ones so short test ids do not flatter the counts.
const (
	handlesFixtureRows          = 24
	handlesFixtureItemsPerRow   = 40
	handlesFixtureBoundaryItems = 60
	// handlesFixtureBudgetBar is 75% of the 262,144-byte dispatch budget, the
	// ceiling for the thin-base isolation fixture below.
	handlesFixtureBudgetBar = 196608
)

// crossRepoHandlesStore serves a thin candidate set (no suppressed rows, no
// padded docstrings, so the evidence term is what the byte count measures) and
// per-entity consumer evidence from a configurable producer function.
type crossRepoHandlesStore struct {
	*deadCodeBudgetStore
	evidenceFor func(entityID string) []deadcode.CrossRepoDeadCodeEvidence
}

func (s *crossRepoHandlesStore) CrossRepoDeadCodeConsumerEvidence(
	_ context.Context,
	_ string,
	entityIDs []string,
	_ code.CrossRepoDeadCodeConsumerReads,
) (map[string][]deadcode.CrossRepoDeadCodeEvidence, code.CrossRepoDeadCodeHiddenConsumers, error) {
	result := make(map[string][]deadcode.CrossRepoDeadCodeEvidence, len(entityIDs))
	for _, entityID := range entityIDs {
		if items := s.evidenceFor(entityID); len(items) > 0 {
			result[entityID] = items
		}
	}
	return result, code.CrossRepoDeadCodeHiddenConsumers{}, nil
}

func handlesFixtureProducerID(index int) string {
	return fmt.Sprintf("content-entity:e_%012x", 0x7a3c10000000+index)
}

// handlesFixtureItem is one production-shaped evidence item: production-style
// repository and entity ids and a five-segment citation, roughly 616 bytes
// serialized in full.
func handlesFixtureItem(row, index, consumerSlot int) deadcode.CrossRepoDeadCodeEvidence {
	consumerRepo := fmt.Sprintf("repository:r_%08x", 0x5b1d0000+consumerSlot)
	consumerEntity := fmt.Sprintf("content-entity:e_%012x", 0x3f9a20000000+row*1000+index)
	return deadcode.CrossRepoDeadCodeEvidence{
		ConsumerRepoID:   consumerRepo,
		ConsumerRepoName: fmt.Sprintf("platform-consumer-service-%02d", consumerSlot),
		ConsumerEntityID: consumerEntity,
		RelationshipType: "CALLS",
		EvidenceFamily:   "direct_code",
		Citation: fmt.Sprintf("code_reachability_rows:scope-%08x/gen-%08x/%s/%s/%s",
			0x2c4e1a90, 0x91d3b7f0, consumerRepo, consumerEntity, handlesFixtureProducerID(row)),
		Confidence:       0.5,
		ConfidenceLabel:  "low",
		ResolutionMethod: "repo_unique_name",
		Depth:            2,
		GenerationID:     "generation:5b7e2f90-1c3a-4d68-a0e4-93f1c7d2b8a6",
		GenerationStatus: "active",
		Ambiguous:        true,
		NeedsEvidence:    true,
		Reason:           "ambiguous_consumer_ownership",
	}
}

// newCrossRepoHandlesStore builds handlesFixtureRows evidence rows plus one row
// with no entity evidence, which falls back to the repository boundary.
// consumerSlots maps an item index to the consumer repository it lands in:
// the identity for the pathological fixture (every item in its own repository,
// nothing collapses) and a modulo for the collapse fixture.
func newCrossRepoHandlesStore(consumerSlot func(index int) int) *crossRepoHandlesStore {
	base := &deadCodeBudgetStore{}
	entities := make(map[string]querycontract.EntityContent)
	rows := make([]map[string]any, 0, handlesFixtureRows+1)
	for index := 0; index <= handlesFixtureRows; index++ {
		entityID := handlesFixtureProducerID(index)
		path := fmt.Sprintf("src/payments/handler%03d.ts", index)
		entities[entityID] = querycontract.EntityContent{
			EntityID: entityID, RepoID: deadCodeBudgetProducerRepoID, RelativePath: path,
			EntityType: "Function", EntityName: fmt.Sprintf("handle%03d", index),
			StartLine: 10, EndLine: 20, Language: "typescript",
			SourceCache: "function handle() {}",
		}
		rows = append(rows, map[string]any{
			"entity_id": entityID, "name": fmt.Sprintf("handle%03d", index),
			"labels": []any{"Function"}, "file_path": path,
			"repo_id": deadCodeBudgetProducerRepoID, "repo_name": "payments-lib",
			"language": "typescript", "start_line": int64(10), "end_line": int64(20),
		})
	}
	base.Entities = entities
	base.Repositories = []querycontract.RepositoryCatalogEntry{
		{ID: deadCodeBudgetProducerRepoID, Name: "payments-lib"},
	}
	base.rows = rows
	base.RelationshipReadModel.Available = true
	base.RelationshipReadModel.Relationships = boundaryRelationships(handlesFixtureBoundaryItems)
	producerRow := make(map[string]int, handlesFixtureRows)
	for index := 0; index < handlesFixtureRows; index++ {
		producerRow[handlesFixtureProducerID(index)] = index
	}
	return &crossRepoHandlesStore{
		deadCodeBudgetStore: base,
		evidenceFor: func(entityID string) []deadcode.CrossRepoDeadCodeEvidence {
			row, ok := producerRow[entityID]
			if !ok {
				return nil
			}
			items := make([]deadcode.CrossRepoDeadCodeEvidence, 0, handlesFixtureItemsPerRow)
			for index := 0; index < handlesFixtureItemsPerRow; index++ {
				items = append(items, handlesFixtureItem(row, index, consumerSlot(index)))
			}
			return items
		},
	}
}

func crossRepoHandlesMux(store *crossRepoHandlesStore) http.Handler {
	handler := &codequery.CodeHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j:   graph.FakeGraphReader{},
		Content: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

func dispatchCrossRepoHandlesFixture(t *testing.T, store *crossRepoHandlesStore, extra map[string]any) *dispatchResult {
	t.Helper()

	args := map[string]any{"repo_id": deadCodeBudgetProducerRepoID}
	for key, value := range extra {
		args[key] = value
	}
	result, err := dispatchTool(context.Background(), crossRepoHandlesMux(store),
		"find_cross_repo_dead_code", args, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || result == nil || result.Envelope == nil {
		t.Fatalf("dispatchTool() = %#v, %v; want a canonical result", result, err)
	}
	return result
}

// TestFindCrossRepoDeadCodeHandlesPathologicalFixtureFitsDefaultBudget is an
// evidence-isolation measurement on a thin base, not the acceptance bar: 24
// rows of 40 evidence items, each in its own consumer repository (so grouping
// cannot collapse anything and the group cap fires on every row), plus a
// 60-item boundary list, at MCP default arguments. The reply must be delivered
// as structuredContent and stay within 75% of the budget. The bar is
// TestFindCrossRepoDeadCodeHandlesCalibratedBaseBars.
func TestFindCrossRepoDeadCodeHandlesPathologicalFixtureFitsDefaultBudget(t *testing.T) {
	t.Parallel()

	store := newCrossRepoHandlesStore(func(index int) int { return index })
	result := dispatchCrossRepoHandlesFixture(t, store, nil)
	est2x := estimateResponseBytes(result)
	t.Logf("pathological handles est2x=%d bar=%d resource_only=%v", est2x, handlesFixtureBudgetBar, result.ResourceOnly)
	if result.IsError || result.ResourceOnly {
		if result.Envelope.Error != nil {
			t.Logf("error code=%s response_bytes=%v", result.Envelope.Error.Code, result.Envelope.Error.Details["response_bytes"])
		}
		t.Fatalf("pathological default reply: is_error=%v resource_only=%v est2x=%d, want structuredContent delivered",
			result.IsError, result.ResourceOnly, est2x)
	}
	if est2x > handlesFixtureBudgetBar {
		t.Fatalf("pathological default reply est2x = %d, want <= %d (75%% of %d)",
			est2x, handlesFixtureBudgetBar, defaultToolResponseByteBudget)
	}
	data, _ := result.Envelope.Data.(map[string]any)
	if data["evidence_detail"] != "handles" {
		t.Fatalf("data.evidence_detail = %#v, want handles at MCP default", data["evidence_detail"])
	}
}

// TestFindCrossRepoDeadCodeFullDetailPathologicalFixtureExceedsBudget keeps the
// bar test honest: the same fixture at evidence_detail full does not fit, so the
// handles default is what delivers it.
func TestFindCrossRepoDeadCodeFullDetailPathologicalFixtureExceedsBudget(t *testing.T) {
	t.Parallel()

	store := newCrossRepoHandlesStore(func(index int) int { return index })
	result := dispatchCrossRepoHandlesFixture(t, store, map[string]any{"evidence_detail": "full"})
	est2x := estimateResponseBytes(result)
	t.Logf("pathological full est2x=%d budget=%d resource_only=%v is_error=%v",
		est2x, defaultToolResponseByteBudget, result.ResourceOnly, result.IsError)
	if result.Envelope.Error != nil {
		t.Logf("pathological full error code=%s response_bytes=%v", result.Envelope.Error.Code, result.Envelope.Error.Details["response_bytes"])
	}
	if !result.ResourceOnly && !result.IsError && est2x <= defaultToolResponseByteBudget {
		t.Fatalf("pathological full reply est2x = %d fits the budget; the fixture no longer exercises the worst case", est2x)
	}
}

// TestFindCrossRepoDeadCodeHandlesCollapseKeepsEveryGroup covers the collapse
// case on the thin base: 40 items per row across 3 consumer repositories
// collapse to 3 groups, which is under the cap, so no row carries the
// truncation marker and handles loses nothing but the per-item detail.
func TestFindCrossRepoDeadCodeHandlesCollapseKeepsEveryGroup(t *testing.T) {
	t.Parallel()

	store := newCrossRepoHandlesStore(func(index int) int { return index % 3 })
	result := dispatchCrossRepoHandlesFixture(t, store, nil)
	est2x := estimateResponseBytes(result)
	t.Logf("thin-base collapse (40 items / 3 repos per row) handles est2x=%d bar=%d", est2x, handlesFixtureBudgetBar)
	if result.IsError || result.ResourceOnly || est2x > handlesFixtureBudgetBar {
		t.Fatalf("collapse default reply: is_error=%v resource_only=%v est2x=%d, want structuredContent within %d",
			result.IsError, result.ResourceOnly, est2x, handlesFixtureBudgetBar)
	}
	data, _ := result.Envelope.Data.(map[string]any)
	buckets, _ := data["candidate_buckets"].(map[string]any)
	checked := 0
	for _, name := range []string{"dead", "live_by_consumer", "unknown"} {
		rows, _ := buckets[name].([]any)
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			if row["consumer_evidence_source"] != "entity" {
				continue
			}
			checked++
			if _, present := row["consumer_evidence_handles_truncated"]; present {
				t.Fatalf("row %v carries consumer_evidence_handles_truncated with only 3 groups: %#v", row["entity_id"], row)
			}
			if got := numberValue(row["consumer_evidence_group_count"]); got != 3 {
				t.Fatalf("row %v consumer_evidence_group_count = %v, want 3", row["entity_id"], got)
			}
			if got := numberValue(row["consumer_evidence_count"]); got != handlesFixtureItemsPerRow {
				t.Fatalf("row %v consumer_evidence_count = %v, want %d", row["entity_id"], got, handlesFixtureItemsPerRow)
			}
		}
	}
	if checked != handlesFixtureRows {
		t.Fatalf("checked %d entity-evidence rows, want %d", checked, handlesFixtureRows)
	}
}
