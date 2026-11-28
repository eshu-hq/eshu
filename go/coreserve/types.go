// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coreserve

import (
	"github.com/eshu-hq/eshu/go/internal/query"
)

// Auth-safe provider and CoreServeDeps field-type aliases (v0.1.0): the
// store interfaces and value types an external binary needs to populate
// CoreServeDeps and CoreAuthWiring without importing package query
// directly. Identity aliases only: a Store implemented against these
// names satisfies the underlying query interfaces method-for-method.
type (
	// AuthProviderItem is one login provider safe to expose
	// pre-authentication (opaque config ID, generic label, protocol
	// kind — no secrets, domains, or metadata URLs).
	AuthProviderItem = query.AuthProviderItem
	// AuthProviderStore lists the configured interactive login
	// providers scoped to one tenant.
	AuthProviderStore = query.AuthProviderStore
	// SignInPolicyReadStore is the read surface for tenant sign-in
	// policy backing AuthProviderListHandler.Policy. Nil is safe
	// (local_login_offered defaults to true).
	SignInPolicyReadStore = query.SignInPolicyReadStore

	// GraphQuery is the concurrent-safe read-only graph traversal
	// surface backing CoreServeDeps.Graph.
	GraphQuery = query.GraphQuery
	// QueryProfile names the query runtime profile
	// (CoreServeDeps.Profile).
	QueryProfile = query.QueryProfile
	// ScopedTokenResolver chains the scoped-token resolvers
	// (CoreServeDeps.ScopedTokenResolver).
	ScopedTokenResolver = query.ScopedTokenResolver
	// BrowserSessionResolver resolves server-managed dashboard
	// sessions (CoreServeDeps.BrowserSessionResolver).
	BrowserSessionResolver = query.BrowserSessionResolver
	// GovernanceStatusConfig carries the hosted-governance posture
	// for authz decisions (CoreServeDeps.Governance).
	GovernanceStatusConfig = query.GovernanceStatusConfig
	// GovernanceAuditAppender appends read-authorization audit
	// events (CoreServeDeps.GovernanceAudit).
	GovernanceAuditAppender = query.GovernanceAuditAppender
	// GraphBackend names the configured graph backend
	// (CoreServeDeps.GraphBackend).
	GraphBackend = query.GraphBackend
	// CookieSecureMode selects the Secure attribute mode for
	// session cookies (CoreServeDeps.CookieSecure).
	CookieSecureMode = query.CookieSecureMode
	// MetricsTimeSeriesSource is the prebuilt Prometheus/Mimir
	// time-series source (CoreServeDeps.Metrics).
	MetricsTimeSeriesSource = query.MetricsTimeSeriesSource
)
