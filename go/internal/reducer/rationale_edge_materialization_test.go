package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestRationaleMaterializationHandlerScopesDeltaRetractToFiles(t *testing.T) {
	t.Parallel()

	writer := &recordingRationaleEdgeWriter{}
	handler := RationaleEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: rationaleDeltaEntityFacts()},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	_, err := handler.Handle(context.Background(), Intent{
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
	if writer.retractDomain != DomainRationaleEdges {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainRationaleEdges)
	}
	if len(writer.retractRows) != 1 {
		t.Fatalf("retractRows len = %d, want 1", len(writer.retractRows))
	}
	payload := writer.retractRows[0].Payload
	if got, ok := payload["delta_projection"].(bool); !ok || !got {
		t.Fatalf("delta_projection = %#v, want true", payload["delta_projection"])
	}
	gotPaths, ok := payload["delta_file_paths"].([]string)
	if !ok {
		t.Fatalf("delta_file_paths type = %T, want []string", payload["delta_file_paths"])
	}
	wantPaths := []string{"/repo/src/handler.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("delta_file_paths = %#v, want %#v", gotPaths, wantPaths)
	}
	if len(writer.writeRows) != 1 {
		t.Fatalf("writeRows len = %d, want 1", len(writer.writeRows))
	}
}

func TestRationaleMaterializationHandlerDeletedOnlyDeltaRetractsWithoutWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingRationaleEdgeWriter{}
	handler := RationaleEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactKind: factKindRepository,
				Payload: map[string]any{
					"repo_id":                      "repo-123",
					"local_path":                   "/repo",
					"delta_generation":             true,
					"delta_deleted_relative_paths": []string{"src/deleted.go"},
				},
			},
		}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-rationale-deleted",
		ScopeID:      "scope-code",
		GenerationID: "gen-2",
		SourceSystem: "git",
		Domain:       DomainRationaleMaterialization,
		EnqueuedAt:   time.Date(2026, time.June, 13, 11, 25, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.June, 13, 11, 25, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if writer.retractDomain != DomainRationaleEdges {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainRationaleEdges)
	}
	if len(writer.retractRows) != 1 {
		t.Fatalf("retractRows len = %d, want 1", len(writer.retractRows))
	}
	gotPaths, ok := writer.retractRows[0].Payload["delta_file_paths"].([]string)
	if !ok {
		t.Fatalf("delta_file_paths type = %T, want []string", writer.retractRows[0].Payload["delta_file_paths"])
	}
	wantPaths := []string{"/repo/src/deleted.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("delta_file_paths = %#v, want %#v", gotPaths, wantPaths)
	}
	if len(writer.writeRows) != 0 {
		t.Fatalf("writeRows len = %d, want 0", len(writer.writeRows))
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
			Payload: map[string]any{
				"repo_id":                      "repo-123",
				"local_path":                   "/repo",
				"delta_generation":             true,
				"delta_relative_paths":         []string{"src/handler.go", "../outside.go"},
				"delta_deleted_relative_paths": []string{},
			},
		},
		{
			FactKind: factKindContentEntity,
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:handler",
				"entity_type": "Function",
				"entity_name": "Handle",
				"entity_metadata": map[string]any{
					"rationale_comments": []any{
						map[string]any{"kind": "WHY", "text": "explain cached projector path"},
					},
				},
			},
		},
	}
}

type recordingRationaleEdgeWriter struct {
	retractDomain       string
	retractRows         []SharedProjectionIntentRow
	writeDomain         string
	writeEvidenceSource string
	writeRows           []SharedProjectionIntentRow
}

func (r *recordingRationaleEdgeWriter) RetractEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	_ string,
) error {
	r.retractDomain = domain
	r.retractRows = append(r.retractRows, rows...)
	return nil
}

func (r *recordingRationaleEdgeWriter) WriteEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	evidenceSource string,
) error {
	r.writeDomain = domain
	r.writeEvidenceSource = evidenceSource
	r.writeRows = append(r.writeRows, rows...)
	return nil
}
