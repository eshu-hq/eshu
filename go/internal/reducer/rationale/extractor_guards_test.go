// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rationale

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

func TestExtractRationaleEdgeRowsTopLevelCommentsPrecedeNestedPoison(t *testing.T) {
	t.Parallel()
	const poisonText = "nested poison must lose to top-level comments"
	envelope := facts.Envelope{FactKind: factload.FactKindContentEntity, Payload: map[string]any{
		"repo_id": "repo-1", "entity_id": "content-entity:one", "relative_path": "src/one.py",
		"rationale_comments": []any{map[string]any{"kind": "WHY", "text": "selected"}},
		"entity_metadata": map[string]any{"rationale_comments": []any{
			map[string]any{"kind": "HACK", "text": poisonText},
		}},
	}}
	_, rows := ExtractRows([]facts.Envelope{envelope})
	if len(rows) != 1 {
		t.Fatalf("precedence rows = %d, want exactly 1", len(rows))
	}
	if got, want := rows[0]["comment_kind"], "WHY"; got != want {
		t.Fatalf("precedence comment kind = %#v, want %#v", got, want)
	}
	poisonUID := "rationale:content-entity:one:HACK:" + rationaleExcerptHash(poisonText)
	if got := rows[0]["rationale_uid"]; got == poisonUID {
		t.Fatalf("nested poison rationale UID %q projected", got)
	}
}

func TestExtractRationaleEdgeRowsRejectsMalformedEnvelopes(t *testing.T) {
	t.Parallel()
	validPayload := func() map[string]any {
		return map[string]any{
			"repo_id": "repo-1", "entity_id": "content-entity:one", "relative_path": "src/one.py",
			"entity_metadata": map[string]any{"rationale_comments": []any{
				map[string]any{"kind": "WHY", "text": "otherwise edge-capable"},
			}},
		}
	}
	tests := []struct {
		name     string
		envelope facts.Envelope
	}{
		{"wrong fact kind", facts.Envelope{FactKind: "file", Payload: validPayload()}},
		{"tombstone", facts.Envelope{FactKind: factload.FactKindContentEntity, IsTombstone: true, Payload: validPayload()}},
		{"missing repo", facts.Envelope{FactKind: factload.FactKindContentEntity, Payload: func() map[string]any { p := validPayload(); delete(p, "repo_id"); return p }()}},
		{"blank entity", facts.Envelope{FactKind: factload.FactKindContentEntity, Payload: func() map[string]any { p := validPayload(); p["entity_id"] = " "; return p }()}},
		{"blank kind", facts.Envelope{FactKind: factload.FactKindContentEntity, Payload: func() map[string]any {
			p := validPayload()
			p["entity_metadata"].(map[string]any)["rationale_comments"] = []any{map[string]any{"kind": " ", "text": "text"}}
			return p
		}()}},
		{"blank text", facts.Envelope{FactKind: factload.FactKindContentEntity, Payload: func() map[string]any {
			p := validPayload()
			p["entity_metadata"].(map[string]any)["rationale_comments"] = []any{map[string]any{"kind": "WHY", "text": " "}}
			return p
		}()}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repos, rows := ExtractRows([]facts.Envelope{test.envelope})
			if len(repos) != 0 || len(rows) != 0 {
				t.Fatalf("malformed envelope projected repos:%v rows:%#v", repos, rows)
			}
		})
	}

	poison := facts.Envelope{FactKind: "file", Payload: validPayload()}
	poison.FactKind = factload.FactKindContentEntity
	repos, rows := ExtractRows([]facts.Envelope{poison})
	if len(repos) != 1 || len(rows) != 1 {
		t.Fatalf("FactKind-only repair projected repos:%v rows:%#v, want one otherwise valid edge", repos, rows)
	}
}
