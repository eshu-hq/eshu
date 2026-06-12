package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

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
