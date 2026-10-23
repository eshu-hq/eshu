// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// inputSpyRanker records the tools slice and entries map of every Rank call.
type inputSpyRanker struct {
	mu      sync.Mutex
	tools   [][]toolcontract.ToolDefinition
	entries []map[string]catalog.Entry
}

func (s *inputSpyRanker) Rank(ctx context.Context, q Query, tools []toolcontract.ToolDefinition,
	entries map[string]catalog.Entry, allowed func(string) bool,
) ([]Candidate, error) {
	s.mu.Lock()
	s.tools = append(s.tools, tools)
	s.entries = append(s.entries, entries)
	s.mu.Unlock()
	return orderRanker{}.Rank(ctx, q, tools, entries, allowed)
}

func TestPrepareBuildsTheRankerOnceOverTheVisibleListAndHandsRankTheSameInputs(t *testing.T) {
	t.Parallel()

	visible := fixtureVisible()
	spy := &inputSpyRanker{}
	var builds int
	var builtTools []toolcontract.ToolDefinition
	var builtEntries map[string]catalog.Entry
	host := &Host{NewRanker: func(tools []toolcontract.ToolDefinition, entries map[string]catalog.Entry) Ranker {
		builds++
		builtTools, builtEntries = tools, entries
		return spy
	}}
	host.Prepare(visible, nil)
	host.Prepare(visible, nil)
	if builds != 1 {
		t.Fatalf("NewRanker ran %d times, want once", builds)
	}
	if len(builtTools) != len(visible) || &builtTools[0] != &visible[0] {
		t.Fatalf("NewRanker got a %d-tool list, want the visible slice itself (len %d)", len(builtTools), len(visible))
	}
	if builtEntries == nil {
		t.Fatal("NewRanker got nil entries, want the catalog entries map")
	}

	newConfig := func() Config { return Config{Registry: fixtureRegistry(), Resolve: fixtureResolve} }
	req := Request{Visible: visible, Session: "s1", Dispatch: (&recordingDispatcher{}).dispatch}
	for range 3 {
		got, _ := host.Serve(context.Background(), newConfig, req, FindToolName, map[string]any{"intent": "x"}).(ToolResult)
		if got.IsError {
			t.Fatalf("find_tool = %+v, want candidates from the prepared ranker", got)
		}
	}
	if len(spy.tools) != 3 {
		t.Fatalf("Rank ran %d times, want 3", len(spy.tools))
	}
	for i := range spy.tools {
		if len(spy.tools[i]) != len(builtTools) || &spy.tools[i][0] != &builtTools[0] {
			t.Fatalf("Rank call %d got a different tools slice than the ranker was built over", i)
		}
		if reflect.ValueOf(spy.entries[i]).Pointer() != reflect.ValueOf(builtEntries).Pointer() {
			t.Fatalf("Rank call %d got a different entries map than the ranker was built over", i)
		}
	}
}

func TestPrepareWithoutNewRankerLeavesTheHostUnchanged(t *testing.T) {
	t.Parallel()

	host := &Host{}
	host.Prepare(fixtureVisible(), nil)
	if host.Ranker != nil || host.entries != nil {
		t.Fatalf("Prepare with no NewRanker set Ranker=%v entries=%v, want both nil", host.Ranker, host.entries)
	}
}

func TestPrepareKeepsAnInjectedRanker(t *testing.T) {
	t.Parallel()

	injected := orderRanker{}
	host := &Host{Ranker: injected, NewRanker: func([]toolcontract.ToolDefinition, map[string]catalog.Entry) Ranker {
		t.Error("NewRanker ran although Ranker was already set")
		return nil
	}}
	host.Prepare(fixtureVisible(), nil)
	if host.Ranker != injected {
		t.Fatalf("Prepare replaced the injected Ranker with %v", host.Ranker)
	}
}

func TestPrepareWithANilBuildLeavesFindToolUnavailable(t *testing.T) {
	t.Parallel()

	host := &Host{NewRanker: func([]toolcontract.ToolDefinition, map[string]catalog.Entry) Ranker { return nil }}
	host.Prepare(fixtureVisible(), nil)
	if host.Ranker != nil {
		t.Fatalf("Ranker = %v, want nil when NewRanker returns nil", host.Ranker)
	}
	newConfig := func() Config { return Config{Registry: fixtureRegistry(), Resolve: fixtureResolve} }
	got, _ := host.Serve(context.Background(), newConfig, fixtureRequest(&recordingDispatcher{}), FindToolName, map[string]any{"intent": "x"}).(ToolResult)
	if !got.IsError || errorCode(t, got) != "mcp_router_ranker_unavailable" {
		t.Fatalf("find_tool = %+v, want mcp_router_ranker_unavailable", got)
	}
}
