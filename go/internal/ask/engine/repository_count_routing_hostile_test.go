// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestIndexedRepositoryCountIntentRejectsQualifiedAndCompoundQuestions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		question string
	}{
		{name: "language qualifier", question: "How many indexed repositories are written in Go?"},
		{name: "vulnerability qualifier", question: "How many indexed repositories have critical vulnerabilities?"},
		{name: "compound breakdown", question: "How many indexed repositories are there, and what is their ecosystem breakdown?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if asksForIndexedRepositoryCount(tt.question) {
				t.Fatalf("asksForIndexedRepositoryCount(%q) = true, want false", tt.question)
			}
		})
	}
}

func TestIndexedRepositoryCountIntentAcceptsSoleExactQuestions(t *testing.T) {
	t.Parallel()

	questions := []string{
		"How many repositories are currently indexed? Return the count and cite the evidence used.",
		"Return only the exact number of currently indexed repositories, then name the evidence source.",
		"What is the exact total of indexed repositories?",
	}
	for _, question := range questions {
		if !asksForIndexedRepositoryCount(question) {
			t.Errorf("asksForIndexedRepositoryCount(%q) = false, want true", question)
		}
	}
}

func TestAskDoesNotRewriteQualifiedIndexedRepositoryCount(t *testing.T) {
	t.Parallel()

	adapter := &scriptedAdapter{
		turns: []provider.Completion{
			{ToolCalls: []provider.ToolCall{{ID: "qualified-1", Name: "get_ecosystem_overview"}}},
			{Text: "qualified ecosystem answer"},
		},
		errOnIdx: -1,
	}
	runner := &recordingRunner{env: supportedEnvelope()}
	engine, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	answer, err := engine.Ask(context.Background(), "How many indexed repositories are written in Go?")
	if err != nil {
		t.Fatalf("Ask() error = %v, want nil", err)
	}
	if got, want := runner.calls[0].name, "get_ecosystem_overview"; got != want {
		t.Fatalf("runner tool = %q, want %q", got, want)
	}
	if got, want := answer.Prose, "qualified ecosystem answer"; got != want {
		t.Fatalf("answer.Prose = %q, want %q", got, want)
	}
}

func TestAskStreamDoesNotRewriteCompoundIndexedRepositoryCount(t *testing.T) {
	t.Parallel()

	adapter := &scriptedStreamingAdapter{turns: []provider.Completion{
		{ToolCalls: []provider.ToolCall{{ID: "compound-1", Name: "get_ecosystem_overview"}}},
		{Text: "compound ecosystem answer"},
	}}
	runner := &recordingRunner{env: supportedEnvelope()}
	engine, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	answer, err := engine.AskStream(
		context.Background(),
		"How many indexed repositories are there, and what is their ecosystem breakdown?",
		func(StreamEvent) {},
	)
	if err != nil {
		t.Fatalf("AskStream() error = %v, want nil", err)
	}
	if got, want := runner.calls[0].name, "get_ecosystem_overview"; got != want {
		t.Fatalf("runner tool = %q, want %q", got, want)
	}
	if got, want := answer.Prose, "compound ecosystem answer"; got != want {
		t.Fatalf("answer.Prose = %q, want %q", got, want)
	}
}

func TestAskRejectsIndexedRepositoryTotalSmallerThanPageCount(t *testing.T) {
	t.Parallel()

	answer := askIndexedRepositoryCount(t, indexedRepositoryCountEnvelope(1, 0))
	assertIndexedRepositoryCountUnavailable(t, answer)
	if got := answer.Packets[0].Result; got != nil {
		t.Fatalf("packet.Result = %#v, want nil for invalid total", got)
	}
}

func TestAskStreamRejectsIndexedRepositoryTotalSmallerThanPageCount(t *testing.T) {
	t.Parallel()

	answer := askIndexedRepositoryCountStream(t, indexedRepositoryCountEnvelope(1, 0))
	assertIndexedRepositoryCountUnavailable(t, answer)
}

func TestAskRejectsIndexedRepositoryCountFromErrorEnvelope(t *testing.T) {
	t.Parallel()

	envelope := indexedRepositoryCountEnvelope(1, 896)
	envelope.Truth = nil
	envelope.Error = &query.ErrorEnvelope{
		Code:    query.ErrorCodeInternalError,
		Message: "repository count failed",
	}
	answer := askIndexedRepositoryCount(t, envelope)
	assertIndexedRepositoryCountUnavailable(t, answer)
	if got := answer.Packets[0].Summary; got != "" {
		t.Fatalf("packet.Summary = %q, want empty for error envelope", got)
	}
	if got := answer.Packets[0].Result; got != nil {
		t.Fatalf("packet.Result = %#v, want nil for error envelope", got)
	}
}

func TestAskStreamRejectsPartialIndexedRepositoryCount(t *testing.T) {
	t.Parallel()

	envelope := indexedRepositoryCountEnvelope(1, 896)
	envelope.Truth.Freshness.State = query.FreshnessStale
	answer := askIndexedRepositoryCountStream(t, envelope)
	assertIndexedRepositoryCountUnavailable(t, answer)
	if got := answer.Packets[0].Summary; got != "" {
		t.Fatalf("packet.Summary = %q, want empty for partial packet", got)
	}
}

func TestAskIndexedRepositoryCountRetainsBoundedEvidence(t *testing.T) {
	t.Parallel()

	answer := askIndexedRepositoryCount(t, indexedRepositoryCountEnvelope(1, 896))
	result, ok := answer.Packets[0].Result.(map[string]any)
	if !ok {
		t.Fatalf("packet.Result type = %T, want map[string]any", answer.Packets[0].Result)
	}
	if got, want := result["total"], int64(896); got != want {
		t.Fatalf("packet.Result[total] = %#v, want %#v", got, want)
	}
	if got, want := len(result), 1; got != want {
		t.Fatalf("len(packet.Result) = %d, want %d", got, want)
	}
	if got := answer.Prose; !strings.Contains(got, "visible in your authorized scope") {
		t.Fatalf("answer.Prose = %q, want authorization-scope qualifier", got)
	}
}

func TestAskIndexedRepositoryCountMaxIterationStillLogsWarning(t *testing.T) {
	t.Parallel()

	adapter := &scriptedAdapter{
		turns: []provider.Completion{{
			ToolCalls: []provider.ToolCall{{ID: "max-iteration-1", Name: "get_index_status"}},
		}},
		errOnIdx: -1,
	}
	runner := &recordingRunner{env: indexedRepositoryCountEnvelope(1, 896)}
	opts := DefaultOptions()
	opts.MaxIterations = 1
	engine, err := New(adapter, runner, nil, opts)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	var logs bytes.Buffer
	engine.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))

	if _, err := engine.Ask(context.Background(), "Return the exact count of indexed repositories."); err != nil {
		t.Fatalf("Ask() error = %v, want nil", err)
	}
	if got := logs.String(); !strings.Contains(got, "ask: reached max reasoning iterations") {
		t.Fatalf("logs = %q, want max-iteration warning", got)
	}
}

func askIndexedRepositoryCount(t *testing.T, envelope *query.ResponseEnvelope) Answer {
	t.Helper()
	adapter := &scriptedAdapter{
		turns: []provider.Completion{
			{ToolCalls: []provider.ToolCall{{ID: "count-1", Name: "get_index_status"}}},
			{Text: "provider supplied an unrelated count"},
		},
		errOnIdx: -1,
	}
	engine, err := New(adapter, &recordingRunner{env: envelope}, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	answer, err := engine.Ask(context.Background(), "Return the exact count of indexed repositories.")
	if err != nil {
		t.Fatalf("Ask() error = %v, want nil", err)
	}
	return answer
}

func askIndexedRepositoryCountStream(t *testing.T, envelope *query.ResponseEnvelope) Answer {
	t.Helper()
	adapter := &scriptedStreamingAdapter{turns: []provider.Completion{
		{ToolCalls: []provider.ToolCall{{ID: "count-stream-1", Name: "get_index_status"}}},
		{Text: "provider supplied an unrelated count"},
	}}
	engine, err := New(adapter, &recordingRunner{env: envelope}, nil, DefaultOptions())
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
	return answer
}

func assertIndexedRepositoryCountUnavailable(t *testing.T, answer Answer) {
	t.Helper()
	if !answer.Partial {
		t.Fatal("answer.Partial = false, want true")
	}
	if got := answer.Prose; got != "" {
		t.Fatalf("answer.Prose = %q, want empty when authoritative total is unavailable", got)
	}
	if got := strings.Join(answer.Limitations, " "); !strings.Contains(got, "authoritative indexed repository total unavailable") {
		t.Fatalf("limitations = %q, want unavailable-total reason", got)
	}
}
