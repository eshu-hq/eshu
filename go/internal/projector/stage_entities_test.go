package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestEntityStageProjectsFromPayload(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "e-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_name":  "UserService",
				"entity_kind":  "Class",
				"content_path": "/src/service.py",
				"start_line":   10,
				"end_line":     50,
				"language":     "python",
			},
		},
	}

	result := ProjectEntityStage("repo:r1", envelopes)
	if len(result.Entities) != 1 {
		t.Fatalf("len = %d, want 1", len(result.Entities))
	}
	e := result.Entities[0]
	if e.EntityName != "UserService" {
		t.Errorf("EntityName = %q", e.EntityName)
	}
	if e.EntityType != "Class" {
		t.Errorf("EntityType = %q", e.EntityType)
	}
	if e.StartLine != 10 {
		t.Errorf("StartLine = %d", e.StartLine)
	}
	if e.EndLine != 50 {
		t.Errorf("EndLine = %d", e.EndLine)
	}
}

func TestEntityStageSkipsNonEntityFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactID: "f-1", FactKind: "file", Payload: map[string]any{"content_path": "/src/main.py"}},
	}

	result := ProjectEntityStage("repo:r1", envelopes)
	if len(result.Entities) != 0 {
		t.Errorf("len = %d, want 0", len(result.Entities))
	}
}

func TestEntityStageDeduplicates(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "e-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_name":  "Foo",
				"entity_kind":  "Function",
				"content_path": "/src/lib.py",
				"start_line":   1,
			},
		},
		{
			FactID:   "e-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_name":  "Foo",
				"entity_kind":  "Function",
				"content_path": "/src/lib.py",
				"start_line":   1,
			},
		},
	}

	result := ProjectEntityStage("repo:r1", envelopes)
	if len(result.Entities) != 1 {
		t.Errorf("len = %d, want 1 (deduped)", len(result.Entities))
	}
}
