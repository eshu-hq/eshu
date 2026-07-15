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

func TestAskRoutesIndexedRepositoryCountToAuthoritativeInventoryTotal(t *testing.T) {
	t.Parallel()

	adapter := &scriptedAdapter{
		turns: []provider.Completion{
			{
				ToolCalls: []provider.ToolCall{{
					ID:        "count-1",
					Name:      "get_ecosystem_overview",
					Arguments: map[string]any{},
				}},
			},
			{Text: "896 indexed repositories from list_indexed_repositories.total"},
		},
		errOnIdx: -1,
	}
	runner := &recordingRunner{env: indexedRepositoryCountEnvelope(1, 896)}
	engine, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	answer, err := engine.Ask(
		context.Background(),
		"How many repositories are currently indexed? Return the count and cite the evidence used.",
	)
	if err != nil {
		t.Fatalf("Ask() error = %v, want nil", err)
	}
	if got, want := len(runner.calls), 1; got != want {
		t.Fatalf("len(runner.calls) = %d, want %d", got, want)
	}
	call := runner.calls[0]
	if got, want := call.name, "list_indexed_repositories"; got != want {
		t.Fatalf("runner tool = %q, want %q", got, want)
	}
	if got, want := call.args["limit"], 1; got != want {
		t.Fatalf("runner limit = %#v, want %#v", got, want)
	}
	if got, want := call.args["offset"], 0; got != want {
		t.Fatalf("runner offset = %#v, want %#v", got, want)
	}
	if got, want := answer.Trace[0].Tool, "list_indexed_repositories"; got != want {
		t.Fatalf("trace tool = %q, want %q", got, want)
	}
	if got, want := answer.Packets[0].Summary, "896 indexed repositories visible in your authorized scope. Evidence: list_indexed_repositories.total."; got != want {
		t.Fatalf("packet summary = %q, want %q", got, want)
	}
	if got := answer.Prose; strings.Contains(got, "15,717,502") {
		t.Fatalf("prose retained unrelated provider count: %q", got)
	}
	var replayedToolName string
	for _, message := range adapter.received[1] {
		if message.Role == provider.RoleTool && message.ToolCallID == "count-1" {
			replayedToolName = message.ToolName
			break
		}
	}
	if got, want := replayedToolName, "list_indexed_repositories"; got != want {
		t.Fatalf("replayed tool result name = %q, want %q", got, want)
	}
}

func TestAskRejectsExactRepositoryCountWithoutAuthoritativeTotal(t *testing.T) {
	t.Parallel()

	adapter := &scriptedAdapter{
		turns: []provider.Completion{
			{ToolCalls: []provider.ToolCall{{ID: "missing-total-1", Name: "get_index_status"}}},
			{Text: "There are 15,717,502 indexed repositories."},
		},
		errOnIdx: -1,
	}
	runner := &recordingRunner{env: indexedRepositoryCountEnvelope(1, nil)}
	engine, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	answer, err := engine.Ask(context.Background(), "Return the exact count of indexed repositories and cite the evidence used.")
	if err != nil {
		t.Fatalf("Ask() error = %v, want nil", err)
	}
	if strings.Contains(answer.Prose, "15,717,502") {
		t.Fatalf("prose = %q, want unrelated count rejected", answer.Prose)
	}
	if !answer.Partial {
		t.Fatal("answer.Partial = false, want true when authoritative total is absent")
	}
	if got := strings.Join(answer.Limitations, " "); !strings.Contains(got, "authoritative indexed repository total unavailable") {
		t.Fatalf("limitations = %q, want unavailable-total reason", got)
	}
}

func TestAskStreamRoutesIndexedRepositoryCountAndFeedsTotalToModel(t *testing.T) {
	t.Parallel()

	adapter := &scriptedStreamingAdapter{
		turns: []provider.Completion{
			{
				ToolCalls: []provider.ToolCall{{
					ID:        "count-stream-1",
					Name:      "get_index_status",
					Arguments: map[string]any{},
				}},
			},
			{Text: "896 indexed repositories from list_indexed_repositories.total"},
		},
	}
	runner := &recordingRunner{env: indexedRepositoryCountEnvelope(1, 896)}
	engine, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	answer, err := engine.AskStream(
		context.Background(),
		"Return only the exact number of currently indexed repositories, then name the evidence source.",
		func(StreamEvent) {},
	)
	if err != nil {
		t.Fatalf("AskStream() error = %v, want nil", err)
	}
	if got, want := runner.calls[0].name, "list_indexed_repositories"; got != want {
		t.Fatalf("runner tool = %q, want %q", got, want)
	}
	if got, want := answer.Trace[0].Tool, "list_indexed_repositories"; got != want {
		t.Fatalf("trace tool = %q, want %q", got, want)
	}
	if got, want := answer.Packets[0].Summary, "896 indexed repositories visible in your authorized scope. Evidence: list_indexed_repositories.total."; got != want {
		t.Fatalf("packet summary = %q, want %q", got, want)
	}
}

func indexedRepositoryCountEnvelope(count int, total any) *query.ResponseEnvelope {
	data := map[string]any{"count": count}
	if total != nil {
		data["total"] = total
	}
	return &query.ResponseEnvelope{
		Data: data,
		Truth: &query.TruthEnvelope{
			Basis: query.TruthBasisAuthoritativeGraph,
			Level: query.TruthLevelExact,
		},
	}
}

func TestAskDoesNotRewriteNonCountEcosystemQuestion(t *testing.T) {
	t.Parallel()

	adapter := &scriptedAdapter{
		turns: []provider.Completion{
			{
				ToolCalls: []provider.ToolCall{{
					ID:        "ecosystem-1",
					Name:      "get_ecosystem_overview",
					Arguments: map[string]any{},
				}},
			},
			{Text: "ecosystem answer"},
		},
		errOnIdx: -1,
	}
	runner := &recordingRunner{env: supportedEnvelope()}
	engine, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	if _, err := engine.Ask(context.Background(), "Summarize the indexed repository ecosystem."); err != nil {
		t.Fatalf("Ask() error = %v, want nil", err)
	}
	if got, want := runner.calls[0].name, "get_ecosystem_overview"; got != want {
		t.Fatalf("runner tool = %q, want %q", got, want)
	}
}
