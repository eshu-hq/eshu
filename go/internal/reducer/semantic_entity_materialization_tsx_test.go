package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractSemanticEntityRowsIncludesTSXFunctionFragmentFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/src/Screen.tsx",
			},
			Payload: map[string]any{
				"repo_id":                "repo-1",
				"entity_id":              "function-tsx-1",
				"relative_path":          "src/Screen.tsx",
				"entity_type":            "Function",
				"entity_name":            "Screen",
				"language":               "tsx",
				"start_line":             7,
				"end_line":               14,
				"jsx_fragment_shorthand": true,
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
	if got, want := row.Metadata["jsx_fragment_shorthand"], true; got != want {
		t.Fatalf("row.Metadata[jsx_fragment_shorthand] = %#v, want %#v", got, want)
	}
}

func TestExtractSemanticEntityRowsIncludesTSXComponentTypeAssertionFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/src/Screen.tsx",
			},
			Payload: map[string]any{
				"repo_id":                  "repo-1",
				"entity_id":                "variable-tsx-1",
				"relative_path":            "src/Screen.tsx",
				"entity_type":              "Variable",
				"entity_name":              "Screen",
				"language":                 "tsx",
				"start_line":               6,
				"end_line":                 6,
				"component_type_assertion": "ComponentType",
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
	if got, want := row.EntityType, "Variable"; got != want {
		t.Fatalf("row.EntityType = %q, want %q", got, want)
	}
	if got, want := row.Metadata["component_type_assertion"], "ComponentType"; got != want {
		t.Fatalf("row.Metadata[component_type_assertion] = %#v, want %#v", got, want)
	}
}

func TestExtractSemanticEntityRowsIncludesTSXReactFunctionComponentTypeAssertionFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/src/Screen.tsx",
			},
			Payload: map[string]any{
				"repo_id":                  "repo-1",
				"entity_id":                "variable-tsx-2",
				"relative_path":            "src/Screen.tsx",
				"entity_type":              "Variable",
				"entity_name":              "Screen",
				"language":                 "tsx",
				"start_line":               6,
				"end_line":                 6,
				"component_type_assertion": "React.FunctionComponent",
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
	if got, want := row.Metadata["component_type_assertion"], "React.FunctionComponent"; got != want {
		t.Fatalf("row.Metadata[component_type_assertion] = %#v, want %#v", got, want)
	}
}

func TestExtractSemanticEntityRowsIncludesTSXComponentWrapperFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/src/Screen.tsx",
			},
			Payload: map[string]any{
				"repo_id":                "repo-1",
				"entity_id":              "component-tsx-1",
				"relative_path":          "src/Screen.tsx",
				"entity_type":            "Component",
				"entity_name":            "MemoButton",
				"language":               "tsx",
				"start_line":             3,
				"end_line":               3,
				"component_wrapper_kind": "memo",
				"jsx_fragment_shorthand": false,
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
	if got, want := row.EntityType, "Component"; got != want {
		t.Fatalf("row.EntityType = %q, want %q", got, want)
	}
	if got, want := row.Metadata["component_wrapper_kind"], "memo"; got != want {
		t.Fatalf("row.Metadata[component_wrapper_kind] = %#v, want %#v", got, want)
	}
}
