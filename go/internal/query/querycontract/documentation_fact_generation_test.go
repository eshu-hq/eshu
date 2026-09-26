// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"strings"
	"testing"
)

func TestDocumentationFactEmptyScopeState(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		found      bool
		status     string
		active     string
		wantReason string
		wantState  FreshnessState
		wantCause  FreshnessCause
		wantGen    string
		wantActive bool
	}{
		{name: "unknown scope", wantReason: DocumentationFactEmptyScopeNotFound},
		{
			name: "dead-lettered scope", found: true, status: "failed",
			wantReason: DocumentationFactEmptyNoActiveGeneration,
			wantState:  FreshnessUnavailable, wantCause: FreshnessCauseDeadLetteredDomain,
		},
		{
			name: "scope still activating", found: true, status: "pending",
			wantReason: DocumentationFactEmptyNoActiveGeneration,
			wantState:  FreshnessBuilding, wantCause: FreshnessCausePendingRepoGeneration,
		},
		{
			name: "active generation without matching facts", found: true, status: "active", active: "gen-1",
			wantReason: DocumentationFactEmptyNoRows, wantGen: "gen-1", wantActive: true,
		},
	} {
		got := DocumentationFactEmptyScopeState(tc.found, tc.status, tc.active)
		if got.EmptyReason != tc.wantReason || got.Freshness.State != tc.wantState || got.Freshness.Cause != tc.wantCause {
			t.Errorf("%s: got reason=%q freshness=%#v, want reason=%q state=%q cause=%q",
				tc.name, got.EmptyReason, got.Freshness, tc.wantReason, tc.wantState, tc.wantCause)
		}
		if got.Binding.GenerationID != tc.wantGen || got.Binding.IsActive != tc.wantActive {
			t.Errorf("%s: binding = %#v, want gen %q active %v", tc.name, got.Binding, tc.wantGen, tc.wantActive)
		}
		if tc.wantState != "" && got.Freshness.Detail == "" {
			t.Errorf("%s: a non-fresh page must explain itself", tc.name)
		}
		if got.Freshness.Cause != "" && !ValidFreshnessCause(got.Freshness.Cause) {
			t.Errorf("%s: cause %q is outside the closed set", tc.name, got.Freshness.Cause)
		}
	}
}

func TestDocumentationFactExplicitGenerationState(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		found         bool
		rowScope      string
		requested     string
		status        string
		wantState     FreshnessState
		wantCause     FreshnessCause
		wantIsActive  bool
		wantDetailHas string
	}{
		{name: "active", found: true, rowScope: "s", requested: "s", status: "active", wantIsActive: true},
		{name: "active without a scope filter", found: true, rowScope: "s", status: "active", wantIsActive: true},
		{name: "superseded", found: true, rowScope: "s", requested: "s", status: "superseded", wantState: FreshnessStale, wantDetailHas: "superseded"},
		{name: "completed", found: true, rowScope: "s", status: "completed", wantState: FreshnessStale, wantDetailHas: "completed"},
		{name: "failed", found: true, rowScope: "s", status: "failed", wantState: FreshnessStale, wantDetailHas: "failed"},
		{name: "pending", found: true, rowScope: "s", status: "pending", wantState: FreshnessBuilding, wantCause: FreshnessCausePendingRepoGeneration, wantDetailHas: "pending"},
		{name: "unknown generation", wantState: FreshnessUnavailable, wantDetailHas: "not a known scope generation"},
		{name: "generation of another scope", found: true, rowScope: "other", requested: "s", status: "active", wantState: FreshnessUnavailable, wantDetailHas: "not a known scope generation"},
		{name: "unrecognized status", found: true, rowScope: "s", status: "mystery", wantState: FreshnessUnavailable, wantDetailHas: "unrecognized"},
	} {
		got := DocumentationFactExplicitGenerationState("gen-x", tc.found, tc.rowScope, tc.requested, tc.status)
		if got.Freshness.State != tc.wantState || got.Freshness.Cause != tc.wantCause {
			t.Errorf("%s: freshness = %#v, want state %q cause %q", tc.name, got.Freshness, tc.wantState, tc.wantCause)
		}
		if got.Binding.GenerationID != "gen-x" || got.Binding.IsActive != tc.wantIsActive {
			t.Errorf("%s: binding = %#v, want gen-x active %v", tc.name, got.Binding, tc.wantIsActive)
		}
		if got.EmptyReason != "" {
			t.Errorf("%s: EmptyReason = %q, an explicit read never claims an empty reason", tc.name, got.EmptyReason)
		}
		if tc.wantDetailHas != "" && !strings.Contains(got.Freshness.Detail, tc.wantDetailHas) {
			t.Errorf("%s: detail = %q, want it to mention %q", tc.name, got.Freshness.Detail, tc.wantDetailHas)
		}
	}
}
