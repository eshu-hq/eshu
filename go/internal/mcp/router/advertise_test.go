// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"reflect"
	"testing"

	toolcontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

func advertiseFixture() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{Name: "alpha", Description: "a"},
		{Name: "beta", Description: "b"},
		{Name: "gamma", Description: "c"},
		{Name: "delta", Description: "d"},
	}
}

func names(tools []toolcontract.ToolDefinition) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

func TestAdvertiseFullReturnsInputUnchanged(t *testing.T) {
	t.Parallel()
	in := advertiseFixture()
	got := Advertise(in, SurfaceFull)
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("full = %v, want input %v", names(got), names(in))
	}
}

func TestAdvertiseRouterReturnsRouterToolsOnly(t *testing.T) {
	t.Parallel()
	got := names(Advertise(advertiseFixture(), SurfaceRouter))
	want := []string{FindToolName, CallToolName}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("router = %v, want %v", got, want)
	}
}

func TestAdvertiseCoreRouterKeepsRegistrationOrder(t *testing.T) {
	t.Parallel()
	got := names(advertiseCoreRouter(advertiseFixture(), []string{"delta", "beta", "missing"}))
	want := []string{"beta", "delta", FindToolName, CallToolName}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("core_router = %v, want %v", got, want)
	}
}

func TestAdvertiseCoreRouterWithEmptyCoreSetIsRouterPair(t *testing.T) {
	t.Parallel()
	got := names(advertiseCoreRouter(advertiseFixture(), nil))
	want := []string{FindToolName, CallToolName}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("core_router(empty core) = %v, want %v", got, want)
	}
}

func TestAdvertiseCoreRouterSurfaceUsesCoreToolNames(t *testing.T) {
	t.Parallel()
	tools := advertiseFixture()
	got := names(Advertise(tools, SurfaceCoreRouter))
	var want []string
	core := map[string]bool{}
	for _, n := range CoreToolNames() {
		core[n] = true
	}
	for _, tool := range tools {
		if core[tool.Name] {
			want = append(want, tool.Name)
		}
	}
	want = append(want, FindToolName, CallToolName)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("core_router = %v, want %v", got, want)
	}
}

func TestAdvertiseDoesNotAliasInputOrSharedSchemas(t *testing.T) {
	t.Parallel()
	in := advertiseFixture()
	got := Advertise(in, SurfaceRouter)
	got[0].Name = "mutated"
	if in[0].Name != "alpha" {
		t.Fatalf("input mutated: %v", names(in))
	}
	again := Advertise(in, SurfaceRouter)
	if again[0].Name != FindToolName {
		t.Fatalf("second call saw mutation: %v", names(again))
	}
}

func TestAdvertiseUnknownSurfaceFallsBackToNothingHidden(t *testing.T) {
	t.Parallel()
	in := advertiseFixture()
	got := Advertise(in, Surface("bogus"))
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("unknown surface = %v, want full list", names(got))
	}
}
