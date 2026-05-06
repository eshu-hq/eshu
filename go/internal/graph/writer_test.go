package graph

import (
	"context"
	"testing"
)

func TestMaterializationScopeGenerationKey(t *testing.T) {
	t.Parallel()

	got := Materialization{
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
	}

	if want := "scope-123:generation-456"; got.ScopeGenerationKey() != want {
		t.Fatalf("Materialization.ScopeGenerationKey() = %q, want %q", got.ScopeGenerationKey(), want)
	}
}

func TestMaterializationCloneCopiesRecords(t *testing.T) {
	t.Parallel()

	original := Materialization{
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Records: []Record{
			{
				RecordID: "node-1",
				Kind:     "repository",
				Deleted:  false,
				Attributes: map[string]string{
					"name": "eshu",
				},
			},
		},
	}

	cloned := original.Clone()
	cloned.Records[0].Attributes["name"] = "mutated"

	if got, want := original.Records[0].Attributes["name"], "eshu"; got != want {
		t.Fatalf("original record attribute = %q, want %q", got, want)
	}
}

func TestMemoryWriterStoresClone(t *testing.T) {
	t.Parallel()

	writer := &MemoryWriter{}
	got, err := writer.Write(context.Background(), Materialization{
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Records: []Record{{
			RecordID: "node-1",
			Kind:     "repository",
			Attributes: map[string]string{
				"name": "eshu",
			},
		}},
	})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if got.RecordCount != 1 {
		t.Fatalf("Write().RecordCount = %d, want 1", got.RecordCount)
	}

	writer.Writes[0].Records[0].Attributes["name"] = "mutated"
	if got := writer.Writes[0].Records[0].Attributes["name"]; got != "mutated" {
		t.Fatalf("stored write mutation did not persist, got %q", got)
	}
}
