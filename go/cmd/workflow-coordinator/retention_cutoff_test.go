// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestGenerationRetentionSoftCutoffFollowsTheReducerRule proves the coordinator reads the
// variable the reducer prunes by, and treats a bad value as the reducer does: the policy
// default, never a value that would change the warning's meaning.
func TestGenerationRetentionSoftCutoffFollowsTheReducerRule(t *testing.T) {
	t.Parallel()

	fallback := postgres.DefaultGenerationRetentionPolicy().MaxSupersededAge
	for _, tc := range []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "unset uses the policy default", value: "", want: fallback},
		{name: "a set value wins", value: "72h", want: 72 * time.Hour},
		{name: "surrounding space is ignored", value: " 72h ", want: 72 * time.Hour},
		{name: "unparsable uses the policy default", value: "soon", want: fallback},
		{name: "zero uses the policy default", value: "0s", want: fallback},
		{name: "negative uses the policy default", value: "-1h", want: fallback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := generationRetentionSoftCutoff(func(name string) string {
				if name == "ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE" {
					return tc.value
				}
				return ""
			})
			if got != tc.want {
				t.Fatalf("cutoff = %v, want %v", got, tc.want)
			}
		})
	}
}
