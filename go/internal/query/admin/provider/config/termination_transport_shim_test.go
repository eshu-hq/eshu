// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config_test

// D0 termination-transport shim (Refs eshu-hq/eshu#9669, part of #6707).
//
// Theory under test: a destination handler can terminate the
// AdminProviderConfigReads routes (no relay, no shared tables) by calling
// ONLY the frozen v0 ProviderConfigReadAdmission port over a versioned
// transport, with authz enforced atomically core-side on every port call.
// Until this evidence, "native" may claim tier-C dispatch only (arbiter
// D-wave ruling, decision 4).
//
// Mechanism proved here (cheapest shim, no serving-path change):
//
//  1. Core side: shimPortServer wraps a ReadStore (the frozen 3-method
//     v0 port) and exposes it as JSON over loopback HTTP. Every port call
//     re-validates the asserted caller bounds (all-scopes, tenant,
//     permission feature) BEFORE touching the store — authz and the read
//     are one atomic core-side step. Direct port calls without valid
//     bounds fail closed with zero store calls (see
//     TestShimPortFailsClosedWithoutBounds).
//  2. Dest side: shimDestReadHandler holds ONLY a base URL and an
//     *http.Client — no store, no *sql.DB, no queue handle. It owns the
//     route (dest-side caller gate, status mapping, projection, envelope)
//     and calls the port for data. Everything prefixed shimDest is the
//     dest-owned copy (stdlib only, by review): the byte-diff harness
//     below is the gate that keeps the copy honest, the same way
//     scripts/check-parity.sh gates the ported OpenAPI fragments.
//  3. Byte-diff harness: TestShimByteParityWithRelay drives the reference
//     in-tree config.ReadHandler and the shim across the same matrix and
//     requires identical status, Content-Type, and body bytes.
//  4. Latency: BenchmarkShimTerminatedListRead vs
//     BenchmarkReferenceListRead measures the touched path before/after.
//
// Trust assumption (explicit, not proved here): the port transport
// authenticates the destination caller (mTLS/sealed in production), so
// the asserted bounds the port re-validates are the destination's own
// authenticated claims. The shim proves fail-closed + atomicity + byte
// parity given that transport; D2 carries the production binding.
//
// Out of scope for the shim: the cmd/api providerConfigReadAdapter env
// synthesis (env-file OIDC/SAML inputs) sits OUTSIDE the frozen port —
// both harness sides share the same ReadStore, so env inputs travel as
// explicit construction inputs in D2, not through the port. Reads emit
// no governance audit events on either path (the read handlers call
// only audit.RequirePermissionFeature as a gate and audit.AddOptionalTime
// as a formatter — no append), so there is no audit write to keep
// atomic; idempotency is the GET repeat check in the matrix.

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin/provider/config"
)

// ---------------------------------------------------------------------------
// Port wire DTOs (shim-local versioned transport shapes, v0).
// ---------------------------------------------------------------------------

// shimDetailDTO is the port-level provider-config view on the wire: the
// same fields config.Detail carries, with explicit tags so the transport
// never leaks Go field names.
type shimDetailDTO struct {
	ProviderConfigID      string         `json:"provider_config_id"`
	ProviderKind          string         `json:"provider_kind"`
	Status                string         `json:"status"`
	ActiveRevisionID      string         `json:"active_revision_id"`
	Configuration         map[string]any `json:"configuration"`
	HasSecret             bool           `json:"has_secret"`
	SecretFingerprint     string         `json:"secret_fingerprint,omitempty"`
	SecretKeyID           string         `json:"secret_key_id,omitempty"`
	ShadowedByEnvironment bool           `json:"shadowed_by_environment"`
	ManagedBy             string         `json:"managed_by"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

// shimRevisionDTO is one port-level revision row on the wire. Optional
// times are pointers so an unset instant is absent, matching
// audit.AddOptionalTime on the reference path.
type shimRevisionDTO struct {
	RevisionID   string     `json:"revision_id"`
	Status       string     `json:"status"`
	HasSecret    bool       `json:"has_secret"`
	CreatedAt    time.Time  `json:"created_at"`
	ActivatedAt  *time.Time `json:"activated_at,omitempty"`
	SupersededAt *time.Time `json:"superseded_at,omitempty"`
}

// shimErrorEnvelope mirrors querycontract.ErrorEnvelope's serialized
// subset used on the deny path (no correlation id, profiles, or details
// on these routes).
type shimErrorEnvelope struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Capability string `json:"capability,omitempty"`
}

// shimPermissionDeniedBody mirrors the reference
// audit.WritePermissionDeniedEnvelope body byte-for-byte: data and truth
// serialize as null because the reference writes a bare ResponseEnvelope.
type shimPermissionDeniedBody struct {
	Data  any                `json:"data"`
	Truth any                `json:"truth"`
	Error *shimErrorEnvelope `json:"error"`
}

// User-visible contract strings copied literally from the reference path
// (config failure.go, reads.go, querycontract). They are caller-visible,
// so the port contract pins them; the byte-diff harness fails on drift.
const (
	shimStoreUnavailableMessage = "admin provider config read store is unavailable"
	shimScopeRequiredMessage    = "all-scope admin authentication is required"
	shimTenantRequiredMessage   = "admin tenant scope is required"
	shimIDRequiredMessage       = "provider_config_id is required"
	shimNotFoundMessage         = "provider config not found"
	shimListFailedMessage       = "failed to list provider configs"
	shimReadFailedMessage       = "failed to read provider config"
	shimRevisionsFailedMessage  = "failed to list provider config revisions"
	shimPermissionFeature       = "identity_admin"
	shimPermissionCapability    = "identity_admin.provider_configs"
	shimPermissionDeniedCode    = "permission_denied"
	shimPermissionDeniedMessage = "permission denied"
	shimProviderConfigListLimit = 500
	shimJSONContentType         = "application/json; charset=utf-8"
)

// ---------------------------------------------------------------------------
// Fixture store: map-backed frozen-port fake with call counters.
// ---------------------------------------------------------------------------

// shimFakeStore implements config.ReadStore over fixtures. Calls counts
// port invocations per method so the harness proves denied requests never
// reach the store (authz atomic with the read, core-side).
type shimFakeStore struct {
	details  map[string]map[string]config.Detail
	lists    map[string][]config.Detail
	revs     map[string]map[string][]config.RevisionItem
	forceErr error
	calls    map[string]int
}

func (f *shimFakeStore) note(method string) {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[method]++
}

func (f *shimFakeStore) GetProviderConfigDetail(ctx context.Context, providerConfigID, tenantID string) (config.Detail, bool, error) {
	f.note("get")
	if err := ctx.Err(); err != nil {
		return config.Detail{}, false, err
	}
	if f.forceErr != nil {
		return config.Detail{}, false, f.forceErr
	}
	byTenant, ok := f.details[tenantID]
	if !ok {
		return config.Detail{}, false, nil
	}
	d, ok := byTenant[providerConfigID]
	return d, ok, nil
}

func (f *shimFakeStore) ListProviderConfigDetails(ctx context.Context, tenantID string) ([]config.Detail, error) {
	f.note("list")
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	return f.lists[tenantID], nil
}

func (f *shimFakeStore) ListProviderConfigRevisions(ctx context.Context, providerConfigID, tenantID string) ([]config.RevisionItem, error) {
	f.note("revisions")
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	byTenant, ok := f.revs[tenantID]
	if !ok {
		return nil, nil
	}
	return byTenant[providerConfigID], nil
}
