// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDocumentationMaterializationHandlerScopesDeltaRetractToDocuments(t *testing.T) {
	t.Parallel()

	writer := &recordingDocumentationEdgeWriter{}
	handler := DocumentationEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: documentationDeltaFacts()},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-doc-delta",
		ScopeID:      "scope-docs",
		GenerationID: "gen-2",
		SourceSystem: "git",
		Domain:       DomainDocumentationMaterialization,
		EnqueuedAt:   time.Date(2026, time.June, 13, 12, 10, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.June, 13, 12, 10, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.retractDomain != DomainDocumentationEdges {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainDocumentationEdges)
	}
	if len(writer.retractRows) != 1 {
		t.Fatalf("retractRows len = %d, want 1", len(writer.retractRows))
	}
	payload := writer.retractRows[0].Payload
	if got, ok := payload["delta_projection"].(bool); !ok || !got {
		t.Fatalf("delta_projection = %#v, want true", payload["delta_projection"])
	}
	gotDocIDs, ok := payload["document_ids"].([]string)
	if !ok {
		t.Fatalf("document_ids type = %T, want []string", payload["document_ids"])
	}
	wantDocIDs := []string{"doc:git:repo-123:README.md"}
	if !reflect.DeepEqual(gotDocIDs, wantDocIDs) {
		t.Fatalf("document_ids = %#v, want %#v", gotDocIDs, wantDocIDs)
	}
	if gotSectionUIDs := semanticPayloadStringSlice(payload, "section_uids"); len(gotSectionUIDs) != 0 {
		t.Fatalf("section_uids = %#v, want empty for file-granular delta", gotSectionUIDs)
	}
	if len(writer.writeRows) != 1 {
		t.Fatalf("writeRows len = %d, want 1", len(writer.writeRows))
	}
}

func TestDocumentationMaterializationHandlerDeletedOnlyDeltaRetractsWithoutWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingDocumentationEdgeWriter{}
	handler := DocumentationEdgeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			{
				FactKind: factKindRepository,
				Payload: map[string]any{
					"repo_id":                      "repo-123",
					"local_path":                   "/repo",
					"delta_generation":             true,
					"delta_deleted_relative_paths": []string{"docs/deleted.md"},
				},
			},
		}},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-doc-deleted",
		ScopeID:      "scope-docs",
		GenerationID: "gen-2",
		SourceSystem: "git",
		Domain:       DomainDocumentationMaterialization,
		EnqueuedAt:   time.Date(2026, time.June, 13, 12, 15, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.June, 13, 12, 15, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
	if writer.retractDomain != DomainDocumentationEdges {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainDocumentationEdges)
	}
	if len(writer.retractRows) != 1 {
		t.Fatalf("retractRows len = %d, want 1", len(writer.retractRows))
	}
	gotDocIDs, ok := writer.retractRows[0].Payload["document_ids"].([]string)
	if !ok {
		t.Fatalf("document_ids type = %T, want []string", writer.retractRows[0].Payload["document_ids"])
	}
	wantDocIDs := []string{"doc:git:repo-123:docs/deleted.md"}
	if !reflect.DeepEqual(gotDocIDs, wantDocIDs) {
		t.Fatalf("document_ids = %#v, want %#v", gotDocIDs, wantDocIDs)
	}
	if len(writer.writeRows) != 0 {
		t.Fatalf("writeRows len = %d, want 0", len(writer.writeRows))
	}
}

func TestBuildDocumentationRetractRowsKeepsMalformedDeltaScoped(t *testing.T) {
	t.Parallel()

	rows := buildDocumentationRetractRows([]string{"scope-docs"}, documentationDeltaScope{
		hasDelta: true,
	})
	if len(rows) != 1 {
		t.Fatalf("retract rows len = %d, want 1", len(rows))
	}
	payload := rows[0].Payload
	if got, ok := payload["delta_projection"].(bool); !ok || !got {
		t.Fatalf("delta_projection = %#v, want true", payload["delta_projection"])
	}
	if gotDocIDs := semanticPayloadStringSlice(payload, "document_ids"); len(gotDocIDs) != 0 {
		t.Fatalf("document_ids = %#v, want empty malformed delta scope", gotDocIDs)
	}
	if gotSectionUIDs := semanticPayloadStringSlice(payload, "section_uids"); len(gotSectionUIDs) != 0 {
		t.Fatalf("section_uids = %#v, want empty malformed delta scope", gotSectionUIDs)
	}
}

func TestBuildDocumentationRetractRowsCarryScopeID(t *testing.T) {
	t.Parallel()

	wholeScope := buildDocumentationRetractRows([]string{"scope-docs"}, documentationDeltaScope{})
	if len(wholeScope) != 1 {
		t.Fatalf("whole-scope retract rows len = %d, want 1", len(wholeScope))
	}
	if got := wholeScope[0].ScopeID; got != "scope-docs" {
		t.Fatalf("whole-scope ScopeID = %q, want scope-docs", got)
	}

	delta := buildDocumentationRetractRows([]string{"scope-docs"}, documentationDeltaScope{
		hasDelta:    true,
		documentIDs: []string{"doc:git:repo-123:README.md"},
	})
	if len(delta) != 1 {
		t.Fatalf("delta retract rows len = %d, want 1", len(delta))
	}
	if got := delta[0].ScopeID; got != "scope-docs" {
		t.Fatalf("delta ScopeID = %q, want scope-docs", got)
	}
}

func TestBuildDocumentationDeltaScopeIgnoresExternalDocumentPathMetadata(t *testing.T) {
	t.Parallel()

	scope := buildDocumentationDeltaScope([]facts.Envelope{
		{
			FactKind: factKindRepository,
			Payload: map[string]any{
				"repo_id":          "repo-123",
				"local_path":       "/repo",
				"delta_generation": true,
				"delta_relative_paths": []string{
					"README.md",
				},
			},
		},
		{
			FactKind: facts.DocumentationDocumentFactKind,
			Payload: map[string]any{
				"document_id": "doc:confluence:12345",
				"source_metadata": map[string]any{
					"path": "README.md",
				},
			},
		},
	}, "scope-docs")
	if !scope.hasDelta {
		t.Fatal("hasDelta = false, want true for repository delta")
	}
	wantDocIDs := []string{"doc:git:repo-123:README.md"}
	if !reflect.DeepEqual(scope.documentIDs, wantDocIDs) {
		t.Fatalf("documentIDs = %#v, want %#v", scope.documentIDs, wantDocIDs)
	}
	if len(scope.sectionUIDs) != 0 {
		t.Fatalf("sectionUIDs = %#v, want empty without git section facts", scope.sectionUIDs)
	}
}

func documentationMentionEnvelope(resolution string, kind string, refs []any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.DocumentationEntityMentionFactKind,
		Payload: map[string]any{
			"document_id":       "doc-runbook",
			"section_id":        "sec-deploy",
			"mention_kind":      "code_symbol",
			"resolution_status": resolution,
			"candidate_refs":    refs,
		},
	}
}

func TestExtractDocumentationEdgeRowsEmitsExactEntityEdge(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		documentationMentionEnvelope(
			facts.DocumentationMentionResolutionExact,
			"entity",
			[]any{map[string]any{"kind": "entity", "id": "uid:func"}},
		),
	}

	rows := ExtractDocumentationEdgeRows(envelopes, "scope-1")
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (rows=%#v)", len(rows), rows)
	}
	row := rows[0]
	if got, want := row["target_entity_id"], "uid:func"; got != want {
		t.Errorf("target_entity_id = %#v, want %#v", got, want)
	}
	if got, want := row["target_kind"], "entity"; got != want {
		t.Errorf("target_kind = %#v, want %#v", got, want)
	}
	if got, want := row["section_uid"], "docsection:doc-runbook|sec-deploy"; got != want {
		t.Errorf("section_uid = %#v, want %#v", got, want)
	}
	if got, want := row["scope_id"], "scope-1"; got != want {
		t.Errorf("scope_id = %#v, want %#v", got, want)
	}
}

func TestExtractDocumentationEdgeRowsSkipsNonExactAndServiceAndMulti(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		resolution string
		refs       []any
	}{
		{"ambiguous", facts.DocumentationMentionResolutionAmbiguous, []any{map[string]any{"kind": "entity", "id": "uid:a"}}},
		{"unmatched", facts.DocumentationMentionResolutionUnmatched, []any{}},
		{"multi_candidate", facts.DocumentationMentionResolutionExact, []any{
			map[string]any{"kind": "entity", "id": "uid:a"},
			map[string]any{"kind": "entity", "id": "uid:b"},
		}},
		{"service_target", facts.DocumentationMentionResolutionExact, []any{map[string]any{"kind": "service", "id": "svc-1"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envelopes := []facts.Envelope{documentationMentionEnvelope(tc.resolution, "entity", tc.refs)}
			rows := ExtractDocumentationEdgeRows(envelopes, "scope-1")
			if len(rows) != 0 {
				t.Fatalf("len(rows) = %d, want 0 (rows=%#v)", len(rows), rows)
			}
		})
	}
}

func TestExtractDocumentationEdgeRowsEmitsWorkloadEdge(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		documentationMentionEnvelope(
			facts.DocumentationMentionResolutionExact,
			"workload",
			[]any{map[string]any{"kind": "workload", "id": "wl-1"}},
		),
	}
	rows := ExtractDocumentationEdgeRows(envelopes, "scope-1")
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["target_kind"], "workload"; got != want {
		t.Errorf("target_kind = %#v, want %#v", got, want)
	}
}

func documentationDeltaFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: factKindRepository,
			Payload: map[string]any{
				"repo_id":                      "repo-123",
				"local_path":                   "/repo",
				"delta_generation":             true,
				"delta_relative_paths":         []string{"README.md", "../outside.md"},
				"delta_deleted_relative_paths": []string{},
			},
		},
		{
			FactKind: facts.DocumentationDocumentFactKind,
			Payload: map[string]any{
				"document_id": "doc:git:repo-123:README.md",
				"source_metadata": map[string]any{
					"path":    "README.md",
					"repo_id": "repo-123",
				},
			},
		},
		{
			FactKind: facts.DocumentationEntityMentionFactKind,
			Payload: map[string]any{
				"document_id":       "doc:git:repo-123:README.md",
				"section_id":        "sec-overview",
				"mention_kind":      "code_symbol",
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"candidate_refs": []any{
					map[string]any{"kind": "entity", "id": "uid:func"},
				},
			},
		},
	}
}

type recordingDocumentationEdgeWriter struct {
	retractDomain string
	retractRows   []SharedProjectionIntentRow
	writeDomain   string
	writeRows     []SharedProjectionIntentRow
}

func (r *recordingDocumentationEdgeWriter) RetractEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	_ string,
) error {
	r.retractDomain = domain
	r.retractRows = append(r.retractRows, rows...)
	return nil
}

func (r *recordingDocumentationEdgeWriter) WriteEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	_ string,
) error {
	r.writeDomain = domain
	r.writeRows = append(r.writeRows, rows...)
	return nil
}
