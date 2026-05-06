package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractSemanticEntityRowsSkipsPlainGoFunctions(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/go/plain.go",
			},
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "function-go-plain",
				"relative_path": "go/plain.go",
				"entity_type":   "Function",
				"entity_name":   "plain",
				"language":      "go",
				"start_line":    10,
				"end_line":      12,
			},
		},
	}

	repoIDs, rows := ExtractSemanticEntityRows(envelopes)
	if got, want := repoIDs, []string{"repo-1"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("ExtractSemanticEntityRows() repoIDs = %v, want %v", got, want)
	}
	if got := len(rows); got != 0 {
		t.Fatalf("ExtractSemanticEntityRows() rows = %d, want 0 for plain Go function", got)
	}
}

func TestExtractSemanticEntityRowsIncludesEnrichedGoFunctions(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/go/handler.go",
			},
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "function-go-handler",
				"relative_path": "go/handler.go",
				"entity_type":   "Function",
				"entity_name":   "ServeHTTP",
				"language":      "go",
				"start_line":    20,
				"end_line":      40,
				"entity_metadata": map[string]any{
					"class_context": "Handler",
				},
			},
		},
	}

	_, rows := ExtractSemanticEntityRows(envelopes)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("ExtractSemanticEntityRows() rows = %d, want %d", got, want)
	}
	if got, want := rows[0].Metadata["class_context"], "Handler"; got != want {
		t.Fatalf("rows[0].Metadata[class_context] = %#v, want %#v", got, want)
	}
}
