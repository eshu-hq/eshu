// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestIdentityCacheMaxBytes(t *testing.T) {
	t.Parallel()

	// Unset → default (0 = use default)
	if got := identityCacheMaxBytes(func(string) string { return "" }); got != 0 {
		t.Fatalf("identityCacheMaxBytes(unset) = %d, want 0 (use default)", got)
	}

	// Negative → disable
	if got := identityCacheMaxBytes(func(string) string { return "-1" }); got != -1 {
		t.Fatalf("identityCacheMaxBytes(-1) = %d, want -1 (disable)", got)
	}

	// Zero → use default
	if got := identityCacheMaxBytes(func(string) string { return "0" }); got != 0 {
		t.Fatalf("identityCacheMaxBytes(0) = %d, want 0 (use default)", got)
	}

	// Positive override
	if got := identityCacheMaxBytes(func(string) string { return "1048576" }); got != 1048576 {
		t.Fatalf("identityCacheMaxBytes(1048576) = %d, want 1048576", got)
	}

	// Invalid → use default
	if got := identityCacheMaxBytes(func(string) string { return "not-a-number" }); got != 0 {
		t.Fatalf("identityCacheMaxBytes(invalid) = %d, want 0 (use default)", got)
	}
}

func TestBuildReducerServiceWiresIdentityCacheEnvDisable(t *testing.T) {
	t.Parallel()

	// ESHU_IDENTITY_CACHE_MAX_BYTES=-1 → cache disabled, service still builds.
	_, err := buildReducerService(
		context.Background(),
		&fakeReducerDB{},
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(&fakeReducerDB{}),
		stubCypherReader{},
		stubCypherReader{},
		func(key string) string {
			if key == "ESHU_IDENTITY_CACHE_MAX_BYTES" {
				return "-1"
			}
			return ""
		},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil (cache disabled via env)", err)
	}
}
