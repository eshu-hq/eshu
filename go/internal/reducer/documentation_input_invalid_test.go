// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractDocumentationEdgeRowsQuarantinesMissingDocumentID proves the
// per-fact isolation and accuracy contract for the
// documentation_entity_mention kind (Contract System v1 Wave 4e): a mention
// fact whose required document_id key is ABSENT (not merely empty) is
// QUARANTINED as an input_invalid dead-letter rather than the pre-typing
// behavior of silently skipping the fact (payloadStr returning "" and the
// handler's own `if targetID == "" || documentID == "" ...` early-continue,
// with no operator signal at all). A fully valid sibling mention in the same
// batch must still produce its DOCUMENTS edge.
func TestExtractDocumentationEdgeRowsQuarantinesMissingDocumentID(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactKind: facts.DocumentationEntityMentionFactKind,
		FactID:   "fact-mention-malformed",
		Payload: map[string]any{
			// "document_id" intentionally absent.
			"section_id":        "sec-deploy",
			"mention_kind":      "code_symbol",
			"resolution_status": facts.DocumentationMentionResolutionExact,
			"candidate_refs": []any{
				map[string]any{"kind": "entity", "id": "uid:bad"},
			},
		},
	}
	valid := documentationMentionEnvelope(
		facts.DocumentationMentionResolutionExact,
		"entity",
		[]any{map[string]any{"kind": "entity", "id": "uid:good"}},
	)

	rows, quarantined, err := ExtractDocumentationEdgeRowsWithQuarantine([]facts.Envelope{malformed, valid}, "scope-1")
	if err != nil {
		t.Fatalf("ExtractDocumentationEdgeRowsWithQuarantine() error = %v, want nil (a missing required field is a quarantine, not a fatal error)", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined = %#v, want exactly 1; the missing-document_id fact must be recorded as one input_invalid quarantine", quarantined)
	}
	if quarantined[0].field != "document_id" {
		t.Fatalf("quarantined[0].field = %q, want %q", quarantined[0].field, "document_id")
	}
	if quarantined[0].factID != "fact-mention-malformed" {
		t.Fatalf("quarantined[0].factID = %q, want %q", quarantined[0].factID, "fact-mention-malformed")
	}

	// The valid sibling must still produce its edge despite the quarantined
	// fact sharing the batch.
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; the valid mention must still project despite the quarantined sibling", len(rows))
	}
	if rows[0]["target_entity_id"] != "uid:good" {
		t.Fatalf("rows[0][target_entity_id] = %v, want %q", rows[0]["target_entity_id"], "uid:good")
	}
}

// TestDocumentationMaterializationHandlerRecordsQuarantinedMentionInputInvalid
// proves the full handler path surfaces a quarantined
// documentation_entity_mention fact through Result.SubSignals, mirroring
// TestKubernetesWorkloadMaterializationQuarantinesMissingObjectID.
func TestDocumentationMaterializationHandlerRecordsQuarantinedMentionInputInvalid(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactKind: facts.DocumentationEntityMentionFactKind,
		FactID:   "fact-mention-malformed",
		Payload: map[string]any{
			// "section_id" intentionally absent.
			"document_id":       "doc-runbook",
			"mention_kind":      "code_symbol",
			"resolution_status": facts.DocumentationMentionResolutionExact,
			"candidate_refs": []any{
				map[string]any{"kind": "entity", "id": "uid:bad"},
			},
		},
	}
	valid := documentationMentionEnvelope(
		facts.DocumentationMentionResolutionExact,
		"entity",
		[]any{map[string]any{"kind": "entity", "id": "uid:good"}},
	)

	writer := &recordingDocumentationEdgeWriter{}
	handler := DocumentationEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{malformed, valid}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-doc-quarantine",
		ScopeID:      "scope-docs",
		GenerationID: "gen-1",
		Domain:       DomainDocumentationMaterialization,
		EnqueuedAt:   time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed documentation_entity_mention fact must be quarantined per-fact, not fail the whole intent", err)
	}
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-section_id fact must be recorded as one input_invalid quarantine", got)
	}
	if len(writer.writeRows) != 1 {
		t.Fatalf("len(writer.writeRows) = %d, want 1; exactly the one valid edge must be written despite the quarantined sibling", len(writer.writeRows))
	}
}

// TestBuildDocumentationDeltaScopeWithQuarantineQuarantinesMissingDocumentID
// proves the per-fact isolation and accuracy contract for the
// documentation_document kind (Contract System v1 Wave 4e): a document fact
// whose required document_id key is ABSENT is QUARANTINED as an
// input_invalid dead-letter rather than the pre-typing behavior of silently
// excluding it from delta scope (semanticPayloadString returning "" and the
// `if documentID == "" { continue }` early-continue, with no operator signal
// at all). A fully valid sibling document in the same batch must still
// contribute to the delta scope.
func TestBuildDocumentationDeltaScopeWithQuarantineQuarantinesMissingDocumentID(t *testing.T) {
	t.Parallel()

	// The malformed document's path (docs/bad.md) is intentionally NOT in the
	// repo delta's changed-path list, so the only way it could contribute a
	// document id is by decoding its own document_id field successfully — an
	// unrelated repo-delta-path candidate-id synthesis (changedCandidateDocumentIDs)
	// would otherwise mask a decode-quarantine regression in this assertion.
	repoDelta := facts.Envelope{
		FactKind: factKindRepository,
		Payload: map[string]any{
			"repo_id":                      "repo-123",
			"local_path":                   "/repo",
			"delta_generation":             true,
			"delta_relative_paths":         []string{"README.md"},
			"delta_deleted_relative_paths": []string{},
		},
	}
	malformed := facts.Envelope{
		FactKind: facts.DocumentationDocumentFactKind,
		FactID:   "fact-doc-malformed",
		Payload: map[string]any{
			// "document_id" intentionally absent.
			"source_metadata": map[string]any{
				"path":    "docs/bad.md",
				"repo_id": "repo-123",
			},
		},
	}
	valid := facts.Envelope{
		FactKind: facts.DocumentationDocumentFactKind,
		Payload: map[string]any{
			"document_id": "doc:git:repo-123:README.md",
			"source_metadata": map[string]any{
				"path":    "README.md",
				"repo_id": "repo-123",
			},
		},
	}

	scope, quarantined, err := buildDocumentationDeltaScopeWithQuarantine([]facts.Envelope{repoDelta, malformed, valid}, "scope-docs")
	if err != nil {
		t.Fatalf("buildDocumentationDeltaScopeWithQuarantine() error = %v, want nil (a missing required field is a quarantine, not a fatal error)", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined = %#v, want exactly 1; the missing-document_id fact must be recorded as one input_invalid quarantine", quarantined)
	}
	if quarantined[0].field != "document_id" {
		t.Fatalf("quarantined[0].field = %q, want %q", quarantined[0].field, "document_id")
	}
	if quarantined[0].factID != "fact-doc-malformed" {
		t.Fatalf("quarantined[0].factID = %q, want %q", quarantined[0].factID, "fact-doc-malformed")
	}

	// The valid sibling must still contribute to the delta scope despite the
	// quarantined fact sharing the batch.
	wantDocIDs := []string{"doc:git:repo-123:README.md"}
	if len(scope.documentIDs) != 1 || scope.documentIDs[0] != wantDocIDs[0] {
		t.Fatalf("scope.documentIDs = %#v, want %#v; the valid document must still be scoped despite the quarantined sibling", scope.documentIDs, wantDocIDs)
	}
}
