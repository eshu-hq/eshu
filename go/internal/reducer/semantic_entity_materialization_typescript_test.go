package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractSemanticEntityRowsIncludesTypeScriptFunctionSemanticFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/src/decorators.ts",
			},
			Payload: map[string]any{
				"repo_id":         "repo-1",
				"entity_id":       "function-ts-1",
				"relative_path":   "src/decorators.ts",
				"entity_type":     "Function",
				"entity_name":     "identity",
				"language":        "typescript",
				"start_line":      4,
				"end_line":        8,
				"decorators":      []any{"@sealed"},
				"type_parameters": []any{"T"},
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
	decorators, ok := row.Metadata["decorators"].([]string)
	if !ok {
		t.Fatalf("row.Metadata[decorators] type = %T, want []string", row.Metadata["decorators"])
	}
	if got, want := len(decorators), 1; got != want || decorators[0] != "@sealed" {
		t.Fatalf("row.Metadata[decorators] = %#v, want [@sealed]", decorators)
	}
	typeParameters, ok := row.Metadata["type_parameters"].([]string)
	if !ok {
		t.Fatalf("row.Metadata[type_parameters] type = %T, want []string", row.Metadata["type_parameters"])
	}
	if got, want := len(typeParameters), 1; got != want || typeParameters[0] != "T" {
		t.Fatalf("row.Metadata[type_parameters] = %#v, want [T]", typeParameters)
	}
}
