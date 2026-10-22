// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestValidateArgumentsCapsFieldErrors(t *testing.T) {
	t.Parallel()

	schema := map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
	args := map[string]any{}
	for i := range 500 {
		args[strings.Repeat("k", i+1)] = i
	}
	errs := ValidateArguments(schema, args)
	if len(errs) != MaxFieldErrors {
		t.Fatalf("errors = %d, want capped at %d", len(errs), MaxFieldErrors)
	}
}

func TestValidateArgumentsStopsItemsAfterMaxItems(t *testing.T) {
	t.Parallel()

	schema := map[string]any{"type": "object", "properties": map[string]any{
		"handles": map[string]any{"type": "array", "maxItems": 2, "items": map[string]any{"type": "object"}},
	}}
	errs := ValidateArguments(schema, map[string]any{"handles": []any{1, 2, 3}})
	if len(errs) != 1 || errs[0].Keyword != "maxItems" {
		t.Fatalf("errors = %+v, want only the maxItems error", errs)
	}
}

func TestValidateArgumentsBoundsEchoedPaths(t *testing.T) {
	t.Parallel()

	schema := map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
	key := strings.Repeat("é", 4096)
	errs := ValidateArguments(schema, map[string]any{key: 1})
	if len(errs) != 1 || len(errs[0].Path) > MaxEchoBytes+len("...") || !utf8.ValidString(errs[0].Path) {
		t.Fatalf("path = %d bytes (valid utf8 %v), want <= %d", len(errs[0].Path), utf8.ValidString(errs[0].Path), MaxEchoBytes)
	}
}

func TestCallRejectsOverlongNameWithoutEcho(t *testing.T) {
	t.Parallel()

	name := strings.Repeat("y", MaxToolNameBytes+1)
	d := &recordingDispatcher{}
	out := newFixtureEngine(t, orderRanker{}).Call(context.Background(), fixtureRequest(d),
		map[string]any{"name": name, "arguments": map[string]any{}})
	if out.Dispatched || errorCode(t, out.Result) != "mcp_router_invalid_arguments" {
		t.Fatalf("Call(long name) = %+v, want mcp_router_invalid_arguments", out)
	}
	raw, _ := json.Marshal(out.Result)
	if strings.Contains(string(raw), name[:64]) {
		t.Fatalf("result echoes the overlong name: %d bytes", len(raw))
	}
	at := strings.Repeat("y", MaxToolNameBytes)
	if errorCode(t, newFixtureEngine(t, orderRanker{}).Call(context.Background(), fixtureRequest(d),
		map[string]any{"name": at, "arguments": map[string]any{}}).Result) != "mcp_router_unknown_tool" {
		t.Fatalf("a name at the bound must still get unknown_tool suggestions")
	}
}

func TestFindBoundsUnknownAnchorErrors(t *testing.T) {
	t.Parallel()

	anchors := map[string]any{}
	for i := range 300 {
		anchors[strings.Repeat("a", 2048)+strings.Repeat("b", i)] = "v"
	}
	result := newFixtureEngine(t, orderRanker{}).Find(context.Background(), fixtureRequest(&recordingDispatcher{}),
		map[string]any{"intent": "x", "anchors": anchors})
	raw, _ := json.Marshal(result)
	if !result.IsError || len(raw) > 32<<10 {
		t.Fatalf("Find(300 long unknown anchors) = %d bytes isError=%v, want a bounded error", len(raw), result.IsError)
	}
	details := structured(t, result)["error"].(map[string]any)["details"].(map[string]any)
	if details["field_errors_truncated"] != true {
		t.Fatalf("details = %v, want field_errors_truncated", details["field_errors_truncated"])
	}
}
