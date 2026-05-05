package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractSemanticEntityRowsIncludesPythonGeneratorFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/src/generators.py",
			},
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "function-1",
				"relative_path": "src/generators.py",
				"entity_type":   "Function",
				"entity_name":   "create_ids",
				"language":      "python",
				"start_line":    1,
				"end_line":      3,
				"semantic_kind": "generator",
			},
		},
	}

	repoIDs, rows := ExtractSemanticEntityRows(envelopes)
	if got, want := repoIDs, []string{"repo-1"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("ExtractSemanticEntityRows() repoIDs = %v, want %v", got, want)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("ExtractSemanticEntityRows() rows = %d, want %d", got, want)
	}

	row := rows[0]
	if got, want := row.EntityType, "Function"; got != want {
		t.Fatalf("row.EntityType = %q, want %q", got, want)
	}
	if got, want := row.Metadata["semantic_kind"], "generator"; got != want {
		t.Fatalf("row.Metadata[semantic_kind] = %#v, want %#v", got, want)
	}
}
