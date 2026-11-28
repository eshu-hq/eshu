// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coreserve

import (
	"github.com/eshu-hq/eshu/go/internal/query"
)

// Auth-family handler aliases (v0.1.0): every CoreAuthWiring field type,
// re-exported as identity aliases so external binaries name the struct
// fields they wire. No handler logic lives here; construction and store
// wiring stay in the serving binary, which alone may touch secret-sealed
// material.
type (
	// LocalIdentityHandler serves the local-identity auth endpoints.
	LocalIdentityHandler = query.LocalIdentityHandler
	// BrowserSessionHandler serves browser-session auth endpoints.
	BrowserSessionHandler = query.BrowserSessionHandler
	// BrowserSessionListHandler serves the browser-session list endpoint.
	BrowserSessionListHandler = query.BrowserSessionListHandler
	// AdminIdentityReadHandler serves admin identity reads.
	AdminIdentityReadHandler = query.AdminIdentityReadHandler
	// AdminIdentityMutationHandler serves admin identity mutations.
	AdminIdentityMutationHandler = query.AdminIdentityMutationHandler
	// ProfileHandler serves the profile endpoint.
	ProfileHandler = query.ProfileHandler
	// SignInPolicyReadHandler serves sign-in policy reads.
	SignInPolicyReadHandler = query.SignInPolicyReadHandler
	// SignInPolicyMutationHandler serves sign-in policy mutations.
	SignInPolicyMutationHandler = query.SignInPolicyMutationHandler
	// SetupHandler serves the first-run setup wizard posture.
	SetupHandler = query.SetupHandler
	// OIDCLoginHandler serves the OIDC login endpoints.
	OIDCLoginHandler = query.OIDCLoginHandler
	// SAMLHandler serves the SAML login endpoints.
	SAMLHandler = query.SAMLHandler
	// GitHubLoginHandler serves the GitHub login endpoints.
	GitHubLoginHandler = query.GitHubLoginHandler
	// AuthProviderListHandler serves GET /api/v0/auth/providers.
	AuthProviderListHandler = query.AuthProviderListHandler
	// AdminProviderConfigReadHandler serves admin provider-config reads.
	AdminProviderConfigReadHandler = query.AdminProviderConfigReadHandler
	// AdminProviderConfigMutationHandler serves admin provider-config mutations.
	AdminProviderConfigMutationHandler = query.AdminProviderConfigMutationHandler
)
