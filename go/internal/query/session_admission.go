// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth/session"
	"github.com/eshu-hq/eshu/go/internal/query/local"
)

// SessionResolverAdmissionContractVersion versions the #9669 D8
// admission-contract shape for the fail-closed session resolver
// (TBD5-resolver). Canonical session truth persists through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and calls
// this versioned port, which the core-owned Postgres adapter satisfies. A
// breaking change to the resolver shape fails the frozen method-set test
// until that list is deliberately updated; bump this version alongside
// (manually — the bump is convention, the frozen list is the enforcement)
// so dest consumers can pin the shape.
const SessionResolverAdmissionContractVersion = "v0"

// SessionResolverAdmission is the versioned admission-contract port the
// middleware depends on instead of a direct session-store handle. It
// aliases the narrow browser-session resolver so the boundary is
// identity-verified by the frozen contract test rather than a parallel
// hand-maintained shape; it will back the eshu-api extraction handoff for
// the session-cookie resolution path.
//
// Fail-closed by contract: a credential no pin resolves (ok=false) or a
// resolver failure denies with 401 through the middleware — it never
// invents an auth context. TestSessionResolverAdmissionFailClosed pins
// both denial halves on the production middleware path. The CSRF and
// external-proof-staleness sentinels keep their existing 403/401
// semantics alongside this pin.
//
// D-wave consumer: D8 (session resolver + session/token lifecycle, per the
// arbiter D-wave ruling on #9669), unblocking D9 login families and the D10
// Setup retirement. The satellite records this pin in a follow-up after
// fresh arbiter review; a satellite PR consuming an unminted pin is
// invalid.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type SessionResolverAdmission = BrowserSessionResolver

// BrowserSessionAdmissionContractVersion versions the #9669 D8
// admission-contract shape for the browser-session lifecycle and list
// surface (the BrowserSessions 4 routes plus the SessionList read).
// Canonical session truth persists through explicit core-owned admission
// contracts: eshu-api keeps the HTTP shape and calls this versioned port,
// which the core-owned Postgres adapter satisfies. A breaking change to
// the session shape fails the frozen method-set test until that list is
// deliberately updated; bump this version alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest consumers can
// pin the shape.
const BrowserSessionAdmissionContractVersion = "v0"

// BrowserSessionAdmission is the versioned admission-contract port the
// browser-session handlers depend on instead of direct store handles. It
// embeds the narrow session lifecycle store (create, revoke, workspace
// switch) and adds the metadata-only list read, so the boundary is
// identity-verified by the frozen contract test rather than a parallel
// hand-maintained shape; it will back the eshu-api extraction handoff for
// POST/GET/DELETE /api/v0/auth/browser-session, PATCH
// /api/v0/auth/browser-session/context, and GET /api/v0/auth/sessions.
//
// The list read stays metadata-only on purpose: rows never carry session
// hashes, CSRF hashes, or external auth secrets (the store computes the
// Current flag by hash comparison), so a dest consumer implementing only
// this contract can serve session reads but cannot steal a session or mint
// one outside the lifecycle methods.
//
// D-wave consumer: D8 (session resolver + session/token lifecycle, per the
// arbiter D-wave ruling on #9669). The satellite records this pin in a
// follow-up after fresh arbiter review; a satellite PR consuming an
// unminted pin is invalid.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type BrowserSessionAdmission interface {
	session.BrowserSessionStore
	// ListSessionsBySubject returns metadata-only rows for the subject. The
	// sessionHash parameter is used server-side to compute the Current
	// boolean; it is never selected or returned in the results. Pass ""
	// when no session cookie is available; no row will be marked current.
	//
	// limit caps the page size. The store may return up to limit+1 rows to
	// detect truncation. offset skips that many rows before the page.
	ListSessionsBySubject(ctx context.Context, subjectIDHash string, asOf time.Time, sessionHash string, limit int, offset int) ([]BrowserSessionListItem, error)
}

// LocalIdentityAdmissionContractVersion versions the #9669 D8
// admission-contract shape for the local-identity lifecycle and profile
// surface (the LocalIdentity 16 routes plus the Profile MFA-status read).
// Canonical identity truth persists through explicit core-owned admission
// contracts: eshu-api keeps the HTTP shape and calls this versioned port,
// which the core-owned Postgres adapter satisfies. A breaking change to
// the identity shape fails the frozen method-set test until that list is
// deliberately updated; bump this version alongside (manually — the bump
// is convention, the frozen list is the enforcement) so dest consumers can
// pin the shape.
const LocalIdentityAdmissionContractVersion = "v0"

// LocalIdentityAdmission is the versioned admission-contract port the
// local-identity and profile handlers depend on instead of a direct store
// handle. It aliases the narrow identity profile lister so the boundary is
// identity-verified by the frozen contract test rather than a parallel
// hand-maintained shape; it will back the eshu-api extraction handoff for
// the local bootstrap/login/invitation/password/MFA/break-glass/API-token/
// TOTP routes and GET /api/v0/auth/profile.
//
// Inbound params are mixed: raw caller-supplied proofs for verification
// (Password, CurrentPassword, MFATOTPCode, SecretPlaintext, Code)
// alongside values the handlers pre-hash before the store
// (SubjectIDHash, PasswordHash, InviteCodeHash, MFARecoveryCodeHash,
// BreakGlassCodeHash, RecoveryCodeHashes). MFACredentialHandle is a
// stored enrollment attribute persisted to secret_credential_handle,
// not a verification proof; outbound results stay hash/metadata-only
// and never include credential handles. Login client secrets and SAML
// keys are NOT in this contract: they stay on the D9 login-family
// pins, per the NEVER list in the arbiter D-wave ruling on #9669.
//
// D-wave consumer: D8 (session resolver + session/token lifecycle, per the
// arbiter D-wave ruling on #9669). The satellite records this pin in a
// follow-up after fresh arbiter review; a satellite PR consuming an
// unminted pin is invalid.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type LocalIdentityAdmission = local.IdentityProfileLister

// APITokenAdmissionContractVersion versions the #9669 D8
// admission-contract shape for the generated API-token lifecycle (create,
// list, revoke, rotate). Canonical token truth persists through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and calls
// this versioned port, which the core-owned Postgres adapter satisfies. A
// breaking change to the token shape fails the frozen method-set test
// until that list is deliberately updated; bump this version alongside
// (manually — the bump is convention, the frozen list is the enforcement)
// so dest consumers can pin the shape.
const APITokenAdmissionContractVersion = "v0"

// APITokenStore is the narrow generated-token surface behind the
// API-token reads and mutations: it creates hash-only tokens, lists
// metadata-only rows, and revokes or rotates by token ID with atomic
// ownership scoping. It deliberately overlaps LocalIdentityAdmission (the
// same four methods appear on both), mirroring how the D1
// TerminalFailureReader overlaps its Acknowledger: a dest consumer
// implementing only this contract can serve the four token routes but
// cannot authenticate a login, reset a password, or touch MFA through it.
type APITokenStore interface {
	CreateLocalIdentityAPIToken(context.Context, local.IdentityAPITokenCreate) error
	RevokeLocalIdentityAPIToken(context.Context, local.IdentityAPITokenRevoke) error
	RotateLocalIdentityAPIToken(context.Context, local.IdentityAPITokenRotate) error
	// ListAPITokensBySubject returns metadata-only token rows owned by the
	// subject identified by subjectIDHash. The result never includes
	// token_hash.
	ListAPITokensBySubject(ctx context.Context, subjectIDHash string, asOf time.Time) ([]local.IdentityAPITokenListItem, error)
}

// APITokenAdmission is the versioned admission-contract port the API-token
// handlers depend on instead of a direct store handle. It aliases the
// narrow token store so the boundary is identity-verified by the frozen
// contract test rather than a parallel hand-maintained shape; it will back
// the eshu-api extraction handoff for GET/POST /api/v0/auth/local/api-tokens
// and POST .../{token_id}/revoke and .../{token_id}/rotate.
//
// D-wave consumer: D8 (session resolver + session/token lifecycle, per the
// arbiter D-wave ruling on #9669). The satellite records this pin in a
// follow-up after fresh arbiter review; a satellite PR consuming an
// unminted pin is invalid.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type APITokenAdmission = APITokenStore
