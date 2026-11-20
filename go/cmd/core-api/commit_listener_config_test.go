// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
)

// TestCommitListenerScopeKindIsPresenceOnly pins the startup scope-kind
// contract: an unlisted spelling passes config parsing with the raw kind
// carried through. Membership is enforced per-request by the admission
// match, not at startup: there is no closed kind list in-repo
// (IngestionScope.Validate admits any non-blank kind), so startup cannot
// distinguish a typo from a kind added after this binary was built.
func TestCommitListenerScopeKindIsPresenceOnly(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"ESHU_COMMIT_LISTEN":          "127.0.0.1:8089",
		"ESHU_COMMIT_MANIFEST_PATH":   "/manifest.yaml",
		"ESHU_COMMIT_CLAIM_PLANE_URL": "http://claim-plane:8080",
		"ESHU_COMMIT_SOURCE_SYSTEM":   "git",
		"ESHU_COMMIT_SCOPE_KIND":      "repositry",
		"ESHU_COMMIT_INSTANCE_ID":     "scorecard-instance",
	}
	cfg, err := commitListenerConfigFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("commitListenerConfigFromEnv(unlisted scope kind) error = %v, want nil (presence-only)", err)
	}
	if cfg == nil {
		t.Fatal("commitListenerConfigFromEnv(unlisted scope kind) = nil, want cfg")
	}
	if got := string(cfg.scopeKind); got != "repositry" {
		t.Fatalf("scopeKind = %q, want %q (carried through for per-request admission)", got, "repositry")
	}
}

// TestCommitListenerMissingCompanionsOrderIsDeterministic is the F4 RED
// test: the missing-variable error must list companions in a fixed order.
// The companions are collected by ranging over a map, so the order is
// random per call. Fifty iterations make a nondeterministic order fail.
func TestCommitListenerMissingCompanionsOrderIsDeterministic(t *testing.T) {
	t.Parallel()

	env := map[string]string{"ESHU_COMMIT_LISTEN": "127.0.0.1:8089"}
	const want = "commit listener ESHU_COMMIT_LISTEN requires " +
		"ESHU_COMMIT_CLAIM_PLANE_URL, ESHU_COMMIT_INSTANCE_ID, " +
		"ESHU_COMMIT_MANIFEST_PATH, ESHU_COMMIT_SCOPE_KIND, " +
		"ESHU_COMMIT_SOURCE_SYSTEM to be set"
	for i := 0; i < 50; i++ {
		_, err := commitListenerConfigFromEnv(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("commitListenerConfigFromEnv() error = nil, want fail-closed")
		}
		if got := err.Error(); got != want {
			t.Fatalf("iteration %d: missing-companions error = %q, want %q", i, got, want)
		}
	}
}
