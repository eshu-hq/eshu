// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// Shared doubles for the Wave-5 rider seven-leg suite. Each counting
// store wraps a fixed canned answer and records how many times its
// re-declared port method runs, so the auth (zero-contact) and parity
// (exactly-one-contact) legs prove which lane the bytes came through.
// They implement the v0 pins by construction (compile-asserted
// below), never the wider store packages.

// countingSignInPolicyStore records GetSignInPolicy calls.
type countingSignInPolicyStore struct {
	calls    int
	tenants  []string
	require  bool
	tenantID string
}

var _ SignInPolicyReadStore = (*countingSignInPolicyStore)(nil)

func (c *countingSignInPolicyStore) GetSignInPolicy(_ context.Context, tenantID string) (auth.SignInPolicy, error) {
	c.calls++
	c.tenants = append(c.tenants, tenantID)
	return auth.SignInPolicy{TenantID: c.tenantID, RequireSSO: c.require}, nil
}

func (c *countingSignInPolicyStore) contacts() int { return c.calls }

// countingAuthProviderStore records ListLoginProviders calls.
type countingAuthProviderStore struct {
	calls int
}

var _ AuthProviderStore = (*countingAuthProviderStore)(nil)

func (c *countingAuthProviderStore) ListLoginProviders(_ context.Context, _ string) ([]AuthProviderItem, error) {
	c.calls++
	return []AuthProviderItem{{ProviderConfigID: "canned", ProviderKind: "oidc"}}, nil
}

func (c *countingAuthProviderStore) contacts() int { return c.calls }
