// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

func TestLoadReducerExpectedSourceLocalProjectors(t *testing.T) {
	t.Parallel()

	got := loadReducerExpectedSourceLocalProjectors(func(k string) string {
		if k == reducerExpectedSourceLocalProjectorsEnv {
			return "878"
		}
		return ""
	})
	if got != 878 {
		t.Fatalf("loadReducerExpectedSourceLocalProjectors() = %d, want 878", got)
	}
}

func TestLoadReducerExpectedSourceLocalProjectorsIgnoresInvalidValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "0", "-1", "nope"} {
		got := loadReducerExpectedSourceLocalProjectors(func(k string) string {
			if k == reducerExpectedSourceLocalProjectorsEnv {
				return raw
			}
			return ""
		})
		if got != 0 {
			t.Fatalf("loadReducerExpectedSourceLocalProjectors(%q) = %d, want 0", raw, got)
		}
	}
}

func TestLoadReducerSemanticEntityClaimLimitDefaultsDisabledForNornicDB(t *testing.T) {
	t.Parallel()

	got := loadReducerSemanticEntityClaimLimit(func(string) string { return "" }, runtimecfg.GraphBackendNornicDB)
	if got != 0 {
		t.Fatalf("loadReducerSemanticEntityClaimLimit() = %d, want 0", got)
	}
}

func TestLoadReducerSemanticEntityClaimLimitDefaultsDisabledForNeo4j(t *testing.T) {
	t.Parallel()

	got := loadReducerSemanticEntityClaimLimit(func(string) string { return "" }, runtimecfg.GraphBackendNeo4j)
	if got != 0 {
		t.Fatalf("loadReducerSemanticEntityClaimLimit() = %d, want 0", got)
	}
}

func TestLoadReducerSemanticEntityClaimLimitReadsOverride(t *testing.T) {
	t.Parallel()

	got := loadReducerSemanticEntityClaimLimit(func(k string) string {
		if k == reducerSemanticEntityClaimLimitEnv {
			return "4"
		}
		return ""
	}, runtimecfg.GraphBackendNornicDB)
	if got != 4 {
		t.Fatalf("loadReducerSemanticEntityClaimLimit() = %d, want 4", got)
	}
}

func TestLoadReducerSemanticEntityClaimLimitIgnoresInvalidOverride(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "0", "-1", "nope"} {
		got := loadReducerSemanticEntityClaimLimit(func(k string) string {
			if k == reducerSemanticEntityClaimLimitEnv {
				return raw
			}
			return ""
		}, runtimecfg.GraphBackendNornicDB)
		if got != 0 {
			t.Fatalf("loadReducerSemanticEntityClaimLimit(%q) = %d, want 0", raw, got)
		}
	}
}
