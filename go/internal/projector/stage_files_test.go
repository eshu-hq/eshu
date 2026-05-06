package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestFilterFileFactsSelectsFileObserved(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactID: "f-1", FactKind: "file", Payload: map[string]any{"content_path": "/src/main.py"}},
		{FactID: "f-2", FactKind: "repository", Payload: map[string]any{"repo_id": "r1"}},
		{FactID: "f-3", FactKind: "fileFact", Payload: map[string]any{"content_path": "/src/util.py"}},
	}

	filtered := FilterFileFacts(envelopes)
	if len(filtered) != 2 {
		t.Fatalf("len = %d, want 2 (FileObserved + FileObservedFact)", len(filtered))
	}
	if filtered[0].FactID != "f-1" {
		t.Errorf("[0].FactID = %q", filtered[0].FactID)
	}
	if filtered[1].FactID != "f-3" {
		t.Errorf("[1].FactID = %q", filtered[1].FactID)
	}
}

func TestFilterFileFactsDeduplicates(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactID: "f-1", FactKind: "file", Payload: map[string]any{"content_path": "/src/main.py"}},
		{FactID: "f-1", FactKind: "file", Payload: map[string]any{"content_path": "/src/main.py"}},
	}

	filtered := FilterFileFacts(envelopes)
	if len(filtered) != 1 {
		t.Fatalf("len = %d, want 1 (deduped)", len(filtered))
	}
}

func TestFilterEntityFactsSelectsParsedEntityObserved(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactID: "e-1", FactKind: "content_entity", Payload: map[string]any{"entity_name": "Foo"}},
		{FactID: "e-2", FactKind: "file", Payload: map[string]any{"content_path": "/src/main.py"}},
		{FactID: "e-3", FactKind: "content_entityFact", Payload: map[string]any{"entity_name": "Bar"}},
	}

	filtered := FilterEntityFacts(envelopes)
	if len(filtered) != 2 {
		t.Fatalf("len = %d, want 2", len(filtered))
	}
}

func TestFilterRepositoryFactsSelectsRepositoryObserved(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactID: "r-1", FactKind: "repository", Payload: map[string]any{"repo_id": "r1"}},
		{FactID: "r-2", FactKind: "file", Payload: map[string]any{"content_path": "/src/main.py"}},
		{FactID: "r-3", FactKind: "repositoryFact", Payload: map[string]any{"repo_id": "r2"}},
	}

	filtered := FilterRepositoryFacts(envelopes)
	if len(filtered) != 2 {
		t.Fatalf("len = %d, want 2", len(filtered))
	}
}

func TestNormalizeFactKindStripsSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"file", "file"},
		{"fileFact", "file"},
		{"content_entityFact", "content_entity"},
		{"repository", "repository"},
		{"Fact", ""},
	}

	for _, tc := range tests {
		got := NormalizeFactKind(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeFactKind(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
