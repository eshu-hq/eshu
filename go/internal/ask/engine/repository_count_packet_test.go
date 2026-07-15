// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestAskEnvelopeDataFedToModel proves exact repository totals remain grounded
// in the authoritative repository-list envelope and deterministic prose.
func TestAskEnvelopeDataFedToModel(t *testing.T) {
	t.Parallel()

	repoData := map[string]any{
		"repositories": []map[string]any{
			{"id": "repo-1", "name": "acme-api"},
			{"id": "repo-2", "name": "acme-web"},
		},
		"count": 2,
		"total": 2,
	}
	env := &query.ResponseEnvelope{
		Data: repoData,
		Truth: &query.TruthEnvelope{
			Level: query.TruthLevelExact,
			Basis: query.TruthBasisAuthoritativeGraph,
		},
	}

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "d1", Name: "list_indexed_repositories", Arguments: map[string]any{"limit": 10}},
		},
		Usage: provider.TokenUsage{InputTokens: 5, OutputTokens: 3},
	}
	turn2 := provider.Completion{
		Text:  "There are 2 repositories indexed: acme-api and acme-web.",
		Usage: provider.TokenUsage{InputTokens: 10, OutputTokens: 8},
	}

	adapter := &scriptedAdapter{turns: []provider.Completion{turn1, turn2}, errOnIdx: -1}
	runner := &recordingRunner{env: env}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ans, err := eng.Ask(context.Background(), "How many repositories are indexed?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if adapter.calls != 2 {
		t.Errorf("adapter.calls = %d, want 2 (loop must terminate once model has data)", adapter.calls)
	}
	if adapter.calls >= 2 {
		msgs := adapter.received[1]
		var toolResultText string
		for _, m := range msgs {
			if m.Role == provider.RoleTool && m.ToolCallID == "d1" {
				toolResultText = m.Text
			}
		}
		if !strings.Contains(toolResultText, "list_indexed_repositories.total") {
			t.Errorf("tool-result message %q missing authoritative total evidence", toolResultText)
		}
	}

	if len(ans.Packets) != 1 {
		t.Fatalf("len(Packets) = %d, want 1", len(ans.Packets))
	}
	if ans.Packets[0].Summary == "" {
		t.Error("Packets[0].Summary is empty; exact-count evidence cannot be surfaced")
	}
	if !ans.Packets[0].Supported {
		t.Error("Packets[0].Supported = false, want true")
	}
	if got, want := ans.Packets[0].ResultRef, "eshu://api-result/repositories"; got != want {
		t.Errorf("Packets[0].ResultRef = %q, want %q", got, want)
	}
	result, ok := ans.Packets[0].Result.(map[string]any)
	if !ok {
		t.Fatalf("Packets[0].Result type = %T, want map[string]any", ans.Packets[0].Result)
	}
	if got, want := result["total"], int64(2); got != want {
		t.Errorf("Packets[0].Result[total] = %#v, want %#v", got, want)
	}

	const wantProse = "2 indexed repositories visible in your authorized scope. Evidence: list_indexed_repositories.total."
	if ans.Prose != wantProse {
		t.Errorf("Prose = %q, want %q", ans.Prose, wantProse)
	}
	for _, limitation := range ans.Limitations {
		if limitation == "no supported evidence assembled" {
			t.Errorf("Limitations contains %q despite exact count evidence", limitation)
		}
	}
}
