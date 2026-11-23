// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// GitHub-specific auth-provider discovery tests (issue #5166, F-5), split
// from auth_providers_test.go to keep that file under the 500-line cap.
// Reuses authProvidersFakeDB from that file (same package).

// TestListLoginProvidersHidesDBGitHubUntilRuntimeMounted proves the P1 fix:
// an active external_github DB row must NOT be advertised on
// /api/v0/auth/providers when the GitHub login runtime is not mounted
// (newGitHubLoginHandler returned nil → APIRouter.Mount skipped the github
// routes), because the "Continue with GitHub" button's
// /api/v0/auth/github/login target would 404. It must be advertised once the
// runtime is mounted. This mirrors the SAML runtime gate exactly.
func TestListLoginProvidersHidesDBGitHubUntilRuntimeMounted(t *testing.T) {
	t.Parallel()

	newStore := func(runtimeEnabled bool) *authProviderListStore {
		return &authProviderListStore{
			identity: pgstatus.NewIdentitySubjectStore(&authProvidersFakeDB{
				dbRows: []pgstatus.LoginProviderItem{
					{ProviderConfigID: "pc_db_only_github", ProviderKind: "external_github"},
				},
			}),
			githubRuntimeEnabled: runtimeEnabled,
		}
	}

	// Runtime not mounted: the DB-backed GitHub provider must be hidden.
	hidden, err := newStore(false).ListLoginProviders(context.Background(), "tenant_a")
	if err != nil {
		t.Fatalf("ListLoginProviders() error = %v", err)
	}
	if len(hidden) != 0 {
		t.Fatalf("ListLoginProviders() = %+v, want empty: a DB github provider must not be advertised when its runtime is unmounted (would 404)", hidden)
	}

	// Runtime mounted: the same DB-backed GitHub provider is now advertised.
	shown, err := newStore(true).ListLoginProviders(context.Background(), "tenant_a")
	if err != nil {
		t.Fatalf("ListLoginProviders() error = %v", err)
	}
	if len(shown) != 1 || shown[0].ProviderConfigID != "pc_db_only_github" || shown[0].ProviderKind != "github" {
		t.Fatalf("ListLoginProviders() = %+v, want exactly [pc_db_only_github kind=github] once the GitHub runtime is enabled", shown)
	}
}
