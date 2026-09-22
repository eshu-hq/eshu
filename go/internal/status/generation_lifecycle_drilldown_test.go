// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/generation"
)

func TestGenerationLifecycleFilterNormalizeClampsLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "zero defaults", limit: 0, want: generation.DefaultLifecycleLimit},
		{name: "negative defaults", limit: -5, want: generation.DefaultLifecycleLimit},
		{name: "within range preserved", limit: 75, want: 75},
		{name: "above cap clamped", limit: generation.MaxLifecycleLimit + 100, want: generation.MaxLifecycleLimit},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := generation.LifecycleFilter{Limit: tc.limit}.Normalize()
			if got.Limit != tc.want {
				t.Fatalf("Normalize().Limit = %d, want %d", got.Limit, tc.want)
			}
		})
	}
}

func TestGenerationLifecycleFilterNormalizeTrimsSelectors(t *testing.T) {
	t.Parallel()

	got := generation.LifecycleFilter{
		ScopeID:       "  scope-1 ",
		Repository:    " github.com/acme/app ",
		CollectorKind: " git ",
		SourceSystem:  " github ",
		GenerationID:  " gen-1 ",
		Status:        " active ",
	}.Normalize()

	if got.ScopeID != "scope-1" || got.Repository != "github.com/acme/app" ||
		got.CollectorKind != "git" || got.SourceSystem != "github" ||
		got.GenerationID != "gen-1" || got.Status != "active" {
		t.Fatalf("Normalize() did not trim selectors: %+v", got)
	}
}

func TestGenerationLifecycleFilterHasScopeSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		filter generation.LifecycleFilter
		want   bool
	}{
		{name: "scope id", filter: generation.LifecycleFilter{ScopeID: "scope-1"}, want: true},
		{name: "repository", filter: generation.LifecycleFilter{Repository: "repo"}, want: true},
		{name: "generation id", filter: generation.LifecycleFilter{GenerationID: "gen-1"}, want: true},
		{name: "collector only is broad", filter: generation.LifecycleFilter{CollectorKind: "git"}, want: false},
		{name: "status only is broad", filter: generation.LifecycleFilter{Status: "active"}, want: false},
		{name: "empty is broad", filter: generation.LifecycleFilter{}, want: false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.filter.HasScopeSelector(); got != tc.want {
				t.Fatalf("HasScopeSelector() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGenerationLifecycleTimestamp(t *testing.T) {
	t.Parallel()

	if got := generation.LifecycleTimestamp(time.Time{}); got != "" {
		t.Fatalf("zero time = %q, want empty", got)
	}
	ts := time.Date(2026, 6, 9, 12, 30, 0, 0, time.FixedZone("x", 3600))
	if got := generation.LifecycleTimestamp(ts); got != "2026-06-09T11:30:00Z" {
		t.Fatalf("formatted time = %q, want RFC3339 UTC", got)
	}
}
