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
	assertIndexedRepositoryAggregateResult(t, answer.Packets[0], 896)
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
	assertIndexedRepositoryAggregateResult(t, answer.Packets[0], 896)
}

func TestAskSelectsIndexedRepositoryInventoryAfterUnrelatedSupportedPacket(t *testing.T) {
	t.Parallel()

	adapter := &scriptedAdapter{
		turns: []provider.Completion{
			{ToolCalls: []provider.ToolCall{
				{ID: "unrelated-1", Name: "list_collectors"},
				{ID: "inventory-1", Name: indexedRepositoryInventoryTool},
			}},
			{Text: "provider supplied an unrelated result"},
		},
		errOnIdx: -1,
	}
	runner := repositoryCountMultiPacketRunner(indexedRepositoryCountEnvelope(1, 896))
	engine, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	answer, err := engine.Ask(context.Background(), "Return the exact count of indexed repositories.")
	if err != nil {
		t.Fatalf("Ask() error = %v, want nil", err)
	}
	assertIndexedRepositoryMultiPacketSelection(t, answer)
}

func TestAskStreamSelectsIndexedRepositoryInventoryAfterUnrelatedSupportedPacket(t *testing.T) {
	t.Parallel()

	adapter := &scriptedStreamingAdapter{turns: []provider.Completion{
		{ToolCalls: []provider.ToolCall{
			{ID: "unrelated-stream-1", Name: "list_collectors"},
			{ID: "inventory-stream-1", Name: indexedRepositoryInventoryTool},
		}},
		{Text: "provider supplied an unrelated result"},
	}}
	runner := repositoryCountMultiPacketRunner(indexedRepositoryCountEnvelope(1, 896))
	engine, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	answer, err := engine.AskStream(
		context.Background(),
		"Return the exact count of indexed repositories.",
		func(StreamEvent) {},
	)
	if err != nil {
		t.Fatalf("AskStream() error = %v, want nil", err)
	}
	assertIndexedRepositoryMultiPacketSelection(t, answer)
}

func TestAskSelectsUnavailableInventoryAfterUnrelatedSupportedPacket(t *testing.T) {
	t.Parallel()

	adapter := &scriptedAdapter{
		turns: []provider.Completion{
			{ToolCalls: []provider.ToolCall{
				{ID: "unrelated-unavailable-1", Name: "list_collectors"},
				{ID: "inventory-unavailable-1", Name: indexedRepositoryInventoryTool},
			}},
			{Text: "provider supplied an unrelated result"},
		},
		errOnIdx: -1,
	}
	inventoryEnvelope := indexedRepositoryCountEnvelope(1, 0)
	engine, err := New(
		adapter,
		repositoryCountMultiPacketRunner(inventoryEnvelope),
		nil,
		DefaultOptions(),
	)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	answer, err := engine.Ask(context.Background(), "Return the exact count of indexed repositories.")
	if err != nil {
		t.Fatalf("Ask() error = %v, want nil", err)
	}
	if answer.PrimaryPacketIndex == nil || *answer.PrimaryPacketIndex != 1 {
		t.Fatalf("answer.PrimaryPacketIndex = %v, want unavailable inventory packet 1", answer.PrimaryPacketIndex)
	}
	assertIndexedRepositoryCountUnavailable(t, answer)
}

func assertIndexedRepositoryMultiPacketSelection(t *testing.T, answer Answer) {
	t.Helper()
	if got, want := len(answer.Packets), 2; got != want {
		t.Fatalf("len(answer.Packets) = %d, want %d", got, want)
	}
	if got, want := answer.Packets[0].PrimaryTool, "list_collectors"; got != want {
		t.Fatalf("first packet tool = %q, want %q; packet order must follow dispatch order", got, want)
	}
	if got, want := answer.Trace[0].Tool, "list_collectors"; got != want {
		t.Fatalf("first trace tool = %q, want %q", got, want)
	}
	if got, want := answer.Trace[1].Tool, indexedRepositoryInventoryTool; got != want {
		t.Fatalf("second trace tool = %q, want %q", got, want)
	}
	if answer.PrimaryPacketIndex == nil {
		t.Fatal("answer.PrimaryPacketIndex = nil, want explicit inventory selection")
	}
	if got, want := *answer.PrimaryPacketIndex, 1; got != want {
		t.Fatalf("answer.PrimaryPacketIndex = %d, want %d", got, want)
	}
	if got, want := answer.Prose, indexedRepositoryCountSummary(896); got != want {
		t.Fatalf("answer.Prose = %q, want %q", got, want)
	}
	assertIndexedRepositoryAggregateResult(t, answer.Packets[*answer.PrimaryPacketIndex], 896)
}

func repositoryCountMultiPacketRunner(inventoryEnvelope *query.ResponseEnvelope) Runner {
	return RunnerFunc(func(_ context.Context, toolName string, _ map[string]any) (RunResult, error) {
		switch toolName {
		case "list_collectors":
			return RunResult{Envelope: embeddedPacketEnvelope(map[string]any{
				"primary_tool": "list_collectors",
				"summary":      "unrelated supported collector result",
				"truth_class":  string(query.AnswerTruthDerived),
				"supported":    true,
				"result_ref":   "eshu://api-result/collectors",
				"citation_ref": "eshu://citations/unrelated",
			})}, nil
		case indexedRepositoryInventoryTool:
			envelope := inventoryEnvelope
			envelope.Data.(map[string]any)["answer_packet"] = map[string]any{
				"primary_tool": indexedRepositoryInventoryTool,
				"summary":      "inventory placeholder",
				"truth_class":  string(query.AnswerTruthDeterministic),
				"supported":    true,
				"citation_ref": "eshu://citations/repository-inventory",
			}
			return RunResult{Envelope: envelope}, nil
		default:
			return RunResult{}, nil
		}
	})
}

func embeddedPacketEnvelope(packet map[string]any) *query.ResponseEnvelope {
	return &query.ResponseEnvelope{
		Data: map[string]any{"answer_packet": packet},
		Truth: &query.TruthEnvelope{
			Basis: query.TruthBasisAuthoritativeGraph,
			Level: query.TruthLevelExact,
		},
	}
}

func assertIndexedRepositoryAggregateResult(t *testing.T, packet query.AnswerPacket, wantTotal int64) {
	t.Helper()
	if got, want := packet.ResultRef, "eshu://api-result/repositories"; got != want {
		t.Fatalf("packet result_ref = %q, want %q", got, want)
	}
	result, ok := packet.Result.(map[string]any)
	if !ok {
		t.Fatalf("packet result type = %T, want map[string]any", packet.Result)
	}
	if got := result["total"]; got != wantTotal {
		t.Fatalf("packet result total = %#v, want %d", got, wantTotal)
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
