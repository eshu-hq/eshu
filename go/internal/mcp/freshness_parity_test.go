// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// Incremental-freshness parity tests (issue #1804, parent #1797).
//
// These tests prove that the HTTP API surface and the MCP dispatch surface
// agree on incremental-freshness truth for the SAME fixture handler. The two
// read surfaces under test (both merged via #1798 and #1799) are:
//
//   - GET /api/v0/freshness/generations  / MCP get_generation_lifecycle
//     (capability freshness.generation_lifecycle)
//   - GET /api/v0/freshness/changed-since / MCP get_changed_since
//     (capability freshness.changed_since)
//
// Both MCP tools are pure transport: dispatchTool forwards the request to the
// exact same query.FreshnessHandler the HTTP API serves, then parses the
// canonical envelope. So for a given fixture the canonical envelope MUST be
// equal across surfaces in the fields that carry freshness truth: truth level,
// basis, capability, the freshness STATE, and the source generation IDs the
// answer reports (current active / since / superseded generation ids).
//
// Generic answer-workflow parity (compare_environments, get_repo_story, etc.)
// is covered by issue #1795 in answer_parity_workflows_test.go; this file is
// scoped strictly to incremental-freshness state and changed/stale/retired
// evidence behavior and does not duplicate that surface.
//
// The CLI surface (eshu freshness generations / eshu freshness changed-since)
// lives in package main (go/cmd/eshu) and cannot be imported here without a
// cycle. The matching CLI-side parity proof — that the CLI consumes the SAME
// canonical envelope and renders the SAME freshness state and generation ids —
// lives in go/cmd/eshu/freshness_parity_test.go, driven against an httptest
// server backed by the identical query.FreshnessHandler fixtures.
//
// Non-green discipline: building / stale / unavailable freshness states are
// asserted to be exactly "building" / "stale" / "unavailable" on BOTH surfaces.
// A regression that silently rendered those as "fresh" (or dropped the state)
// would fail these tests.

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// freshnessComparable captures the cross-surface fields every freshness read
// surface MUST agree on: the canonical truth envelope fields plus the source
// generation identities and per-category change counts the answer reports. An
// inequality here is a real cross-surface discrepancy, not cosmetic.
type freshnessComparable struct {
	truthLevel      query.TruthLevel
	truthBasis      query.TruthBasis
	truthCapability string
	freshnessState  query.FreshnessState
	hasError        bool
	errorCode       query.ErrorCode
	errorCapability string

	// Source generation identities the answer reports. For generation
	// lifecycle these are collected from every returned row; for changed-since
	// they are the since / current-active generation the diff compared.
	generationIDs []string
	// changeCounts is the per-category added/updated/unchanged/retired/
	// superseded counts the changed-since answer reports, flattened in
	// deterministic order so a transport that reshaped or dropped a verdict
	// would diverge.
	changeCounts []int
}

// extractFreshnessComparable reduces a canonical ResponseEnvelope from either
// freshness surface to the fields that must match across transports. It reads
// the well-known generations / changed-since data shapes and is tolerant of a
// nil truth (error envelopes).
func extractFreshnessComparable(t *testing.T, env *query.ResponseEnvelope) freshnessComparable {
	t.Helper()

	if env == nil {
		t.Fatal("envelope is nil, want canonical freshness envelope")
	}

	cmp := freshnessComparable{}
	if env.Error != nil {
		cmp.hasError = true
		cmp.errorCode = env.Error.Code
		cmp.errorCapability = env.Error.Capability
	}
	if env.Truth != nil {
		cmp.truthLevel = env.Truth.Level
		cmp.truthBasis = env.Truth.Basis
		cmp.truthCapability = env.Truth.Capability
		cmp.freshnessState = env.Truth.Freshness.State
	}

	data, _ := env.Data.(map[string]any)
	cmp.generationIDs = generationIDsFromData(data)
	cmp.changeCounts = changeCountsFromData(data)
	return cmp
}

// generationIDsFromData pulls the source generation identities an answer
// reports, in deterministic order. Generation-lifecycle answers list a
// generation per row (generation_id + current_active_generation_id);
// changed-since answers report the since and current-active generation the
// diff compared. The parity layer compares these so both surfaces are pinned to
// the SAME source generations, not just the same shape.
func generationIDsFromData(data map[string]any) []string {
	if data == nil {
		return nil
	}
	ids := []string{}
	if rows, ok := data["generations"].([]any); ok {
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			ids = append(ids, "gen:"+query.StringVal(row, "generation_id"))
			if active := query.StringVal(row, "current_active_generation_id"); active != "" {
				ids = append(ids, "active:"+active)
			}
		}
		return ids
	}
	if since := query.StringVal(data, "since_generation_id"); since != "" {
		ids = append(ids, "since:"+since)
	}
	if active := query.StringVal(data, "current_active_generation_id"); active != "" {
		ids = append(ids, "current:"+active)
	}
	return ids
}

// changeCountsFromData flattens the changed-since per-category counts in a
// deterministic verdict order. Generation-lifecycle answers have no such block
// and return nil. The fixed verdict order means a transport that collapsed
// retired into superseded (or dropped a category) would produce a different
// slice and fail parity.
func changeCountsFromData(data map[string]any) []int {
	if data == nil {
		return nil
	}
	categories, ok := data["categories"].([]any)
	if !ok {
		return nil
	}
	counts := []int{}
	for _, raw := range categories {
		category, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		block, _ := category["counts"].(map[string]any)
		for _, verdict := range []string{"added", "updated", "unchanged", "retired", "superseded"} {
			counts = append(counts, intFromAny(block[verdict]))
		}
	}
	return counts
}

func intFromAny(v any) int {
	switch typed := v.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	default:
		return 0
	}
}

// requireFreshnessParity asserts two surface projections of the same freshness
// question agree on every comparable field. This is the heart of the freshness
// parity layer.
func requireFreshnessParity(t *testing.T, surfaceA, surfaceB string, a, b freshnessComparable) {
	t.Helper()

	if a.hasError != b.hasError {
		t.Fatalf("%s hasError=%t but %s hasError=%t; surfaces disagree on error vs success", surfaceA, a.hasError, surfaceB, b.hasError)
	}
	if a.errorCode != b.errorCode {
		t.Fatalf("error code parity: %s=%q, %s=%q", surfaceA, a.errorCode, surfaceB, b.errorCode)
	}
	if a.errorCapability != b.errorCapability {
		t.Fatalf("error capability parity: %s=%q, %s=%q", surfaceA, a.errorCapability, surfaceB, b.errorCapability)
	}
	if a.truthLevel != b.truthLevel {
		t.Fatalf("truth level parity: %s=%q, %s=%q", surfaceA, a.truthLevel, surfaceB, b.truthLevel)
	}
	if a.truthBasis != b.truthBasis {
		t.Fatalf("truth basis parity: %s=%q, %s=%q", surfaceA, a.truthBasis, surfaceB, b.truthBasis)
	}
	if a.truthCapability != b.truthCapability {
		t.Fatalf("truth capability parity: %s=%q, %s=%q", surfaceA, a.truthCapability, surfaceB, b.truthCapability)
	}
	if a.freshnessState != b.freshnessState {
		t.Fatalf("freshness state parity: %s=%q, %s=%q", surfaceA, a.freshnessState, surfaceB, b.freshnessState)
	}
	if !equalStringSlices(a.generationIDs, b.generationIDs) {
		t.Fatalf("source generation parity: %s=%v, %s=%v", surfaceA, a.generationIDs, surfaceB, b.generationIDs)
	}
	if !equalIntSlices(a.changeCounts, b.changeCounts) {
		t.Fatalf("change-count parity: %s=%v, %s=%v", surfaceA, a.changeCounts, surfaceB, b.changeCounts)
	}
}

func equalIntSlices(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mountFreshnessHandler mounts a real query.FreshnessHandler backed by the given
// fixture readers on a fresh mux, returning the handler both the HTTP and MCP
// surfaces share. Both surfaces dispatch into the SAME handler instance, so any
// truth divergence is a transport bug, not a fixture difference.
func mountFreshnessHandler(
	t *testing.T,
	generations query.GenerationLifecycleReader,
	changedSince query.ChangedSinceReader,
) http.Handler {
	t.Helper()

	mux := http.NewServeMux()
	handler := &query.FreshnessHandler{
		Generations:  generations,
		ChangedSince: changedSince,
		Profile:      query.ProfileProduction,
	}
	handler.Mount(mux)
	return mux
}

// fixedGenerationReader is a deterministic generation-lifecycle fixture. It
// returns one prepared page regardless of filter so the HTTP and MCP surfaces
// observe the identical rows.
type fixedGenerationReader struct {
	page status.GenerationLifecyclePage
}

func (r fixedGenerationReader) ListGenerationLifecycle(
	_ context.Context,
	_ status.GenerationLifecycleFilter,
) (status.GenerationLifecyclePage, error) {
	return r.page, nil
}

// fixedChangedSinceReader is a deterministic changed-since fixture returning one
// prepared summary regardless of filter.
type fixedChangedSinceReader struct {
	summary status.ChangedSinceSummary
}

func (r fixedChangedSinceReader) ComputeChangedSinceDelta(
	_ context.Context,
	_ status.ChangedSinceFilter,
) (status.ChangedSinceSummary, error) {
	return r.summary, nil
}
