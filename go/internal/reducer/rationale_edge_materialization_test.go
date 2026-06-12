package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractRationaleEdgeRowsEmitsExplainsEdge(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: factKindContentEntity,
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:func",
				"entity_type": "Function",
				"entity_metadata": map[string]any{
					"rationale_comments": []any{
						map[string]any{"kind": "WHY", "text": "memoize because recompute is expensive"},
					},
				},
			},
		},
	}

	repoIDs, rows := ExtractRationaleEdgeRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-1" {
		t.Fatalf("repoIDs = %v, want [repo-1]", repoIDs)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (rows=%#v)", len(rows), rows)
	}
	row := rows[0]
	if got, want := row["target_entity_id"], "content-entity:func"; got != want {
		t.Errorf("target_entity_id = %#v, want %#v", got, want)
	}
	if got, want := row["comment_kind"], "WHY"; got != want {
		t.Errorf("comment_kind = %#v, want %#v", got, want)
	}
	uid, _ := row["rationale_uid"].(string)
	if uid == "" || uid[:10] != "rationale:" {
		t.Errorf("rationale_uid = %#v, want a rationale:* identity", row["rationale_uid"])
	}
	if row["excerpt_hash"] == "" || row["excerpt_hash"] == nil {
		t.Error("excerpt_hash is empty")
	}
}

func TestExtractRationaleEdgeRowsSkipsEntitiesWithoutRationale(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: factKindContentEntity,
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:plain",
				"entity_type": "Function",
			},
		},
	}
	repoIDs, rows := ExtractRationaleEdgeRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
	if len(repoIDs) != 0 {
		t.Fatalf("len(repoIDs) = %d, want 0", len(repoIDs))
	}
}

// TestExtractRationaleEdgeRowsDeduplicatesIdenticalComment proves the same
// comment on the same entity yields one stable edge.
func TestExtractRationaleEdgeRowsDeduplicatesIdenticalComment(t *testing.T) {
	t.Parallel()

	comment := map[string]any{"kind": "HACK", "text": "same"}
	envelopes := []facts.Envelope{
		{FactKind: factKindContentEntity, Payload: map[string]any{
			"repo_id": "repo-1", "entity_id": "e1", "entity_type": "Function",
			"entity_metadata": map[string]any{"rationale_comments": []any{comment, comment}},
		}},
	}
	_, rows := ExtractRationaleEdgeRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (dedup)", len(rows))
	}
}
