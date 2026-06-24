// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestRationaleHandlerRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := RationaleEdgeMaterializationHandler{
		FactLoader:   &stubFactLoader{},
		IntentWriter: &recordingRationaleIntentWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainCodeCallMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestRationaleHandlerRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := RationaleEdgeMaterializationHandler{
		IntentWriter: &recordingRationaleIntentWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainRationaleMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestRationaleHandlerRequiresIntentWriter(t *testing.T) {
	t.Parallel()

	handler := RationaleEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainRationaleMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when intent writer is nil")
	}
}

// TestRationaleHandlerEmitsIntentsWithDeltaRefresh proves the promoted handler
// emits one whole-scope refresh intent carrying the delta scope plus one
// file-scoped per-edge intent, replacing the legacy direct retract+write path.
func TestRationaleHandlerEmitsIntentsWithDeltaRefresh(t *testing.T) {
	t.Parallel()

	writer := &recordingRationaleIntentWriter{}
	handler := RationaleEdgeMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: rationaleDeltaEntityFacts()},
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-rationale-delta",
		ScopeID:      "scope-code",
		GenerationID: "gen-2",
		SourceSystem: "git",
		Domain:       DomainRationaleMaterialization,
		EnqueuedAt:   time.Date(2026, time.June, 13, 11, 20, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.June, 13, 11, 20, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	// One per-repo refresh intent (owns the retract) plus one per-edge intent.
	if result.CanonicalWrites != 2 {
		t.Fatalf("result.CanonicalWrites = %d, want 2", result.CanonicalWrites)
	}

	refresh := writer.refreshRows()
	if len(refresh) != 1 {
		t.Fatalf("refresh intents = %d, want 1", len(refresh))
	}
	if refresh[0].ProjectionDomain != DomainRationaleEdges {
		t.Fatalf("refresh domain = %q, want %q", refresh[0].ProjectionDomain, DomainRationaleEdges)
	}
	if refresh[0].PartitionKey != rationaleWholeScopePartitionKey("repo-123") {
		t.Fatalf("refresh partition key = %q, want whole-scope key", refresh[0].PartitionKey)
	}
	if got, ok := refresh[0].Payload["delta_projection"].(bool); !ok || !got {
		t.Fatalf("refresh delta_projection = %#v, want true", refresh[0].Payload["delta_projection"])
	}
	gotPaths, ok := refresh[0].Payload["delta_file_paths"].([]string)
	if !ok {
		t.Fatalf("delta_file_paths type = %T, want []string", refresh[0].Payload["delta_file_paths"])
	}
	if want := []string{"/repo/src/handler.go"}; strings.Join(gotPaths, ",") != strings.Join(want, ",") {
		t.Fatalf("delta_file_paths = %#v, want %#v", gotPaths, want)
	}

	edges := writer.edgeRows()
	if len(edges) != 1 {
		t.Fatalf("per-edge intents = %d, want 1", len(edges))
	}
	if !rowUsesRefreshFence(edges[0]) {
		t.Fatalf("edge intent %q not marked retract_via_refresh", edges[0].IntentID)
	}
	if !strings.HasPrefix(edges[0].PartitionKey, rationalePartitionKeyVersion+":files:") {
		t.Fatalf("edge partition key %q lacks file-scoped prefix", edges[0].PartitionKey)
	}
	if got, want := anyToString(edges[0].Payload["target_path"]), "/repo/src/handler.go"; got != want {
		t.Fatalf("edge target_path = %q, want %q", got, want)
	}
}

func TestRationaleHandlerLogsCompletion(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	now := time.Date(2026, time.June, 13, 11, 20, 0, 0, time.UTC)
	handler := RationaleEdgeMaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: rationaleDeltaEntityFacts()},
		IntentWriter: &recordingRationaleIntentWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-rationale-1",
		ScopeID:      "scope-code",
		GenerationID: "gen-2",
		SourceSystem: "git",
		Domain:       DomainRationaleMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	logText := logs.String()
	for _, want := range []string{
		`"msg":"rationale materialization completed"`,
		`"edge_count":1`,
		`"repo_count":1`,
		`"intent_count":2`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s:\n%s", want, logText)
		}
	}
}

// TestRationaleHandlerSkipsWhenNoProjectionContext proves a content entity with
// no repository envelope produces no projection context, so the handler emits
// nothing rather than fabricating an unfenceable edge.
func TestRationaleHandlerSkipsWhenNoProjectionContext(t *testing.T) {
	t.Parallel()

	writer := &recordingRationaleIntentWriter{}
	handler := RationaleEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactKind: factKindContentEntity,
				Payload: map[string]any{
					"repo_id":     "repo-1",
					"entity_id":   "content-entity:func",
					"entity_type": "Function",
					"path":        "/repo/src/x.go",
					"entity_metadata": map[string]any{
						"rationale_comments": []any{
							map[string]any{"kind": "WHY", "text": "memoize because recompute is expensive"},
						},
					},
				},
			},
		}},
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-rationale-1",
		ScopeID:      "scope-code",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainRationaleMaterialization,
		EnqueuedAt:   time.Date(2026, time.June, 13, 11, 20, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.June, 13, 11, 20, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if len(writer.rows) != 0 {
		t.Fatalf("emitted %d intents, want 0 without a projection context", len(writer.rows))
	}
}

func TestBuildRationaleRetractRowsKeepsMalformedDeltaScoped(t *testing.T) {
	t.Parallel()

	rows := buildRationaleRetractRows([]string{"repo-123"}, rationaleDeltaScope{
		repositoryIDs: []string{"repo-123"},
		hasDelta:      true,
	})
	if len(rows) != 1 {
		t.Fatalf("retract rows len = %d, want 1", len(rows))
	}
	payload := rows[0].Payload
	if got, ok := payload["delta_projection"].(bool); !ok || !got {
		t.Fatalf("delta_projection = %#v, want true", payload["delta_projection"])
	}
	if gotPaths := semanticPayloadStringSlice(payload, "delta_file_paths"); len(gotPaths) != 0 {
		t.Fatalf("delta_file_paths = %#v, want empty malformed delta scope", gotPaths)
	}
}

func TestExtractRationaleEdgeRowsEmitsExplainsEdge(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: factKindContentEntity,
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:func",
				"entity_type": "Function",
				"path":        "/repo/src/func.go",
				"entity_metadata": map[string]any{
					"rationale_comments": []any{
						map[string]any{"kind": "WHY", "text": "memoize because recompute is expensive"},
					},
				},
			},
		},
	}

	repoIDs, rows := ExtractRationaleEdgeRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-1" {
		t.Fatalf("repoIDs = %v, want [repo-1]", repoIDs)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (rows=%#v)", len(rows), rows)
	}
	row := rows[0]
	if got, want := row["target_entity_id"], "content-entity:func"; got != want {
		t.Errorf("target_entity_id = %#v, want %#v", got, want)
	}
	if got, want := row["target_path"], "/repo/src/func.go"; got != want {
		t.Errorf("target_path = %#v, want %#v", got, want)
	}
	if got, want := row["comment_kind"], "WHY"; got != want {
		t.Errorf("comment_kind = %#v, want %#v", got, want)
	}
	uid, _ := row["rationale_uid"].(string)
	if uid == "" || uid[:10] != "rationale:" {
		t.Errorf("rationale_uid = %#v, want a rationale:* identity", row["rationale_uid"])
	}
	if row["excerpt_hash"] == "" || row["excerpt_hash"] == nil {
		t.Error("excerpt_hash is empty")
	}
}

func TestExtractRationaleEdgeRowsSkipsEntitiesWithoutRationale(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: factKindContentEntity,
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:plain",
				"entity_type": "Function",
			},
		},
	}
	repoIDs, rows := ExtractRationaleEdgeRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
	if len(repoIDs) != 0 {
		t.Fatalf("len(repoIDs) = %d, want 0", len(repoIDs))
	}
}

// TestExtractRationaleEdgeRowsDeduplicatesIdenticalComment proves the same
// comment on the same entity yields one stable edge.
func TestExtractRationaleEdgeRowsDeduplicatesIdenticalComment(t *testing.T) {
	t.Parallel()

	comment := map[string]any{"kind": "HACK", "text": "same"}
	envelopes := []facts.Envelope{
		{FactKind: factKindContentEntity, Payload: map[string]any{
			"repo_id": "repo-1", "entity_id": "e1", "entity_type": "Function",
			"entity_metadata": map[string]any{"rationale_comments": []any{comment, comment}},
		}},
	}
	_, rows := ExtractRationaleEdgeRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (dedup)", len(rows))
	}
}

func TestLoadRationaleMaterializationFactsUsesSingleLegacyFallback(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{envelopes: rationaleDeltaEntityFacts()}
	envelopes, err := loadRationaleMaterializationFacts(context.Background(), loader, "scope-code", "gen-2")
	if err != nil {
		t.Fatalf("loadRationaleMaterializationFacts() error = %v, want nil", err)
	}
	if loader.calls != 1 {
		t.Fatalf("ListFacts() calls = %d, want 1 fallback load", loader.calls)
	}
	if len(envelopes) != len(rationaleDeltaEntityFacts()) {
		t.Fatalf("envelopes len = %d, want %d", len(envelopes), len(rationaleDeltaEntityFacts()))
	}
}

func rationaleDeltaEntityFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: factKindRepository,
			ScopeID:  "scope-code",
			Payload: map[string]any{
				"repo_id":                      "repo-123",
				"local_path":                   "/repo",
				"source_run_id":                "run-1",
				"delta_generation":             true,
				"delta_relative_paths":         []string{"src/handler.go", "../outside.go"},
				"delta_deleted_relative_paths": []string{},
			},
		},
		{
			FactKind: factKindContentEntity,
			ScopeID:  "scope-code",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:handler",
				"entity_type": "Function",
				"entity_name": "Handle",
				"path":        "/repo/src/handler.go",
				"entity_metadata": map[string]any{
					"rationale_comments": []any{
						map[string]any{"kind": "WHY", "text": "explain cached projector path"},
					},
				},
			},
		},
	}
}

// recordingRationaleIntentWriter captures the durable shared-projection intents
// the promoted RationaleEdgeMaterializationHandler emits, so handler tests assert
// on emitted intents instead of direct edge writes (#2869).
type recordingRationaleIntentWriter struct {
	rows []SharedProjectionIntentRow
}

func (w *recordingRationaleIntentWriter) UpsertIntents(_ context.Context, rows []SharedProjectionIntentRow) error {
	w.rows = append(w.rows, rows...)
	return nil
}

// refreshRows returns the per-repo refresh intents (the rows that own the
// retract) the writer captured.
func (w *recordingRationaleIntentWriter) refreshRows() []SharedProjectionIntentRow {
	var out []SharedProjectionIntentRow
	for _, row := range w.rows {
		if isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	return out
}

// edgeRows returns the write-only per-edge intents the writer captured.
func (w *recordingRationaleIntentWriter) edgeRows() []SharedProjectionIntentRow {
	var out []SharedProjectionIntentRow
	for _, row := range w.rows {
		if !isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].IntentID < out[j].IntentID })
	return out
}
