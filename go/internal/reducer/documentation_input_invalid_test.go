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

// TestExtractDocumentationEdgeRowsTrimsDocumentAndSectionIDs proves the typed
// path trims document_id/section_id/mention_kind before the empty check and
// before building the section-node UID, exactly as the pre-typing raw path did
// (payloadStr = strings.TrimSpace(fmt.Sprint(val)), candidate_loader.go). This
// is the byte-identical guarantee: a padded-but-non-empty document_id must
// produce the SAME graph identity it produced pre-typing (docsection:doc1|sec1,
// not docsection: doc1 | sec1 ), and a whitespace-only id must skip the edge
// the same way payloadStr's trim-then-empty-check skipped it. Codex P2 on
// PR #4738 (documentation_edge_materialization.go:190).
func TestExtractDocumentationEdgeRowsTrimsDocumentAndSectionIDs(t *testing.T) {
	t.Parallel()

	padded := facts.Envelope{
		FactKind: facts.DocumentationEntityMentionFactKind,
		FactID:   "fact-mention-padded",
		Payload: map[string]any{
			"document_id":       "  doc1  ",
			"section_id":        "  sec1  ",
			"mention_kind":      "  code_symbol  ",
			"resolution_status": facts.DocumentationMentionResolutionExact,
			"candidate_refs": []any{
				map[string]any{"kind": "entity", "id": "uid:target"},
			},
		},
	}

	rows, quarantined, err := ExtractDocumentationEdgeRowsWithQuarantine([]facts.Envelope{padded}, "scope-1")
	if err != nil {
		t.Fatalf("ExtractDocumentationEdgeRowsWithQuarantine() error = %v, want nil", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want 0; a padded-but-non-empty id is a valid decode", quarantined)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; a padded id must still project one edge", len(rows))
	}
	// The graph identity must match the pre-typing (trimmed) UID, not carry the
	// surrounding whitespace into the node uid.
	wantSectionUID := "docsection:doc1|sec1"
	if got := rows[0]["section_uid"]; got != wantSectionUID {
		t.Fatalf("section_uid = %q, want %q; document_id/section_id must be trimmed before building the node uid", got, wantSectionUID)
	}
	if got := rows[0]["document_id"]; got != "doc1" {
		t.Fatalf("document_id = %q, want %q; the projected row must carry the trimmed id", got, "doc1")
	}
	if got := rows[0]["section_id"]; got != "sec1" {
		t.Fatalf("section_id = %q, want %q; the projected row must carry the trimmed id", got, "sec1")
	}
	if got := rows[0]["mention_kind"]; got != "code_symbol" {
		t.Fatalf("mention_kind = %q, want %q; mention_kind was trimmed by the pre-typing payloadStr path", got, "code_symbol")
	}
}

// TestExtractDocumentationEdgeRowsUnsupportedMajorIsFatal proves the
// classification behavior the decode-site comment documents: an unsupported
// schema major is classified input_invalid by the contracts module
// (decodeLatestMajor), but partitionDecodeFailures escalates the
// ErrUnsupportedSchemaMajor sentinel to a FATAL error (isQuarantine=false,
// non-nil error) rather than a per-fact quarantine, because version skew must
// fail the whole intent for durable triage (it can succeed once the reducer
// supports the major). This is the Copilot classification question on
// PR #4738 (documentation_edge_materialization.go:160): the comment is
// correct, and this test is the evidence.
func TestExtractDocumentationEdgeRowsUnsupportedMajorIsFatal(t *testing.T) {
	t.Parallel()

	unsupported := facts.Envelope{
		FactKind:      facts.DocumentationEntityMentionFactKind,
		FactID:        "fact-mention-badmajor",
		SchemaVersion: "2.0.0", // unsupported major
		Payload: map[string]any{
			"document_id":       "doc1",
			"section_id":        "sec1",
			"mention_kind":      "code_symbol",
			"resolution_status": facts.DocumentationMentionResolutionExact,
			"candidate_refs": []any{
				map[string]any{"kind": "entity", "id": "uid:target"},
			},
		},
	}

	rows, quarantined, err := ExtractDocumentationEdgeRowsWithQuarantine([]facts.Envelope{unsupported}, "scope-1")
	if err == nil {
		t.Fatalf("err = nil, want a fatal error; an unsupported schema major must fail the whole intent, not be quarantined per-fact")
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want 0; an unsupported major is fatal, never a per-fact quarantine", quarantined)
	}
	if rows != nil {
		t.Fatalf("rows = %#v, want nil on a fatal error", rows)
	}
}

// TestExtractDocumentationEdgeRowsSkipsWhitespaceOnlyIDs proves a
// whitespace-only document_id or section_id (a valid, present decode) is
// skipped exactly as the pre-typing path skipped it: payloadStr trimmed the
// value to "" before the `documentID == "" || sectionID == ""` check, so no
// edge was projected. The typed path must trim before that same check.
func TestExtractDocumentationEdgeRowsSkipsWhitespaceOnlyIDs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		documentID string
		sectionID  string
	}{
		{"whitespace_document_id", "   ", "sec1"},
		{"whitespace_section_id", "doc1", "   "},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := facts.Envelope{
				FactKind: facts.DocumentationEntityMentionFactKind,
				FactID:   "fact-mention-ws",
				Payload: map[string]any{
					"document_id":       tc.documentID,
					"section_id":        tc.sectionID,
					"mention_kind":      "code_symbol",
					"resolution_status": facts.DocumentationMentionResolutionExact,
					"candidate_refs": []any{
						map[string]any{"kind": "entity", "id": "uid:target"},
					},
				},
			}
			rows, quarantined, err := ExtractDocumentationEdgeRowsWithQuarantine([]facts.Envelope{env}, "scope-1")
			if err != nil {
				t.Fatalf("ExtractDocumentationEdgeRowsWithQuarantine() error = %v, want nil", err)
			}
			if len(quarantined) != 0 {
				t.Fatalf("quarantined = %#v, want 0; a whitespace-only id is a valid (present) decode, not input_invalid", quarantined)
			}
			if len(rows) != 0 {
				t.Fatalf("len(rows) = %d, want 0; a whitespace-only id trims to empty and must skip the edge exactly as the pre-typing payloadStr path did", len(rows))
			}
		})
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
