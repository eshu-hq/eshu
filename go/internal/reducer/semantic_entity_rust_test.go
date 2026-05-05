package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractSemanticEntityRowsIncludesRustImplBlocksAndFunctionImplContext(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-rust",
			},
		},
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/src/point.rs",
			},
			Payload: map[string]any{
				"repo_id":       "repo-rust",
				"entity_id":     "impl-1",
				"relative_path": "src/point.rs",
				"entity_type":   "ImplBlock",
				"entity_name":   "Point",
				"language":      "rust",
				"start_line":    1,
				"end_line":      18,
				"kind":          "trait_impl",
				"trait":         "Display",
				"target":        "Point",
			},
		},
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/src/point.rs",
			},
			Payload: map[string]any{
				"repo_id":       "repo-rust",
				"entity_id":     "fn-new",
				"relative_path": "src/point.rs",
				"entity_type":   "Function",
				"entity_name":   "new",
				"language":      "rust",
				"start_line":    3,
				"end_line":      7,
				"impl_context":  "Point",
			},
		},
	}

	repoIDs, rows := ExtractSemanticEntityRows(envelopes)
	if got, want := repoIDs, []string{"repo-rust"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("repoIDs = %v, want %v", got, want)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}

	rowsByType := make(map[string]SemanticEntityRow, len(rows))
	for _, row := range rows {
		rowsByType[row.EntityType] = row
	}

	implBlock := rowsByType["ImplBlock"]
	if got, want := implBlock.Metadata["kind"], "trait_impl"; got != want {
		t.Fatalf("impl block kind = %#v, want %#v", got, want)
	}
	if got, want := implBlock.Metadata["trait"], "Display"; got != want {
		t.Fatalf("impl block trait = %#v, want %#v", got, want)
	}
	if got, want := implBlock.Metadata["target"], "Point"; got != want {
		t.Fatalf("impl block target = %#v, want %#v", got, want)
	}

	fn := rowsByType["Function"]
	if got, want := fn.Metadata["impl_context"], "Point"; got != want {
		t.Fatalf("function impl_context = %#v, want %#v", got, want)
	}
}
