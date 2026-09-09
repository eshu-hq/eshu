// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
)

// Appender records validation-safe governance audit events. It carries the
// same single-method set as the query root's GovernanceAuditAppender
// (auth.go), so every existing constructor argument still satisfies it. The
// root keeps its own declaration: the audit family owns that spelling, and
// duplicating one method here would not keep the two in sync by itself --
// structural typing does, and the admin handlers only ever need Append.
type Appender interface {
	Append(context.Context, []governanceaudit.Event) error
}

// SharedActorIDHash is the stable, non-sensitive actor identity for shared
// admin-token actions, which carry no per-subject hash. It lets the
// governance audit record a shared_token actor without inventing or leaking
// a real identity. Moved verbatim from the admin replay file, where the
// replay, identity-mutation, and provider-mutation audit paths all read the
// same package variable so the three surfaces stamp one identity.
var SharedActorIDHash = func() string {
	sum := sha256.Sum256([]byte("eshu:shared-admin-token"))
	return "sha256:" + hex.EncodeToString(sum[:])
}()

// ActorClassForAuth maps an auth context to a governance audit actor class.
// Repointed from actorClassForAuth (internal/query/auth_audit.go): the mode
// constants come from queryauth and the class constants from
// governanceaudit, so the mapping cannot drift from either source.
func ActorClassForAuth(auth queryauth.AuthContext) governanceaudit.ActorClass {
	switch auth.Mode {
	case queryauth.AuthModeBrowserSession:
		return governanceaudit.ActorClassBrowserSession
	case queryauth.AuthModeScoped:
		return governanceaudit.ActorClassScopedToken
	case queryauth.AuthModeShared:
		return governanceaudit.ActorClassSharedToken
	}
	return governanceaudit.ActorClassAnonymous
}

// RecoveryActor maps an auth context to a governance audit actor class and
// identity hash. Moved from adminRecoveryActor (admin replay file): a cookie
// session keeps its browser_session class here as it does on a route denial;
// a shared admin token carries no per-subject hash, so it uses the stable
// synthetic identity rather than an empty one; any other caller with no
// subject hash downgrades to anonymous, because NormalizeEvent rejects an
// identity-bearing class without an actor identity.
func RecoveryActor(auth queryauth.AuthContext) (governanceaudit.ActorClass, string) {
	actorClass := ActorClassForAuth(auth)
	if auth.SubjectIDHash != "" {
		return actorClass, auth.SubjectIDHash
	}
	if actorClass == governanceaudit.ActorClassSharedToken {
		return actorClass, SharedActorIDHash
	}
	return governanceaudit.ActorClassAnonymous, ""
}

// IdentityHash hashes a local-identity value for audit and storage
// comparison. Repointed from localIdentityHash
// (internal/query/local_identity_handler_helpers.go): blank stays blank,
// otherwise sha256 hex with the sha256: prefix.
func IdentityHash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// IdentityPolicyRevision derives the policy-revision hash for a tenant and
// workspace. Repointed from localIdentityPolicyRevision
// (internal/query/local_identity_requests.go).
func IdentityPolicyRevision(tenantID string, workspaceID string) string {
	return IdentityHash(strings.TrimSpace(tenantID) + ":" + strings.TrimSpace(workspaceID))
}

// AuthWorkspaceID resolves the caller's workspace from the request's auth
// context. Repointed from authWorkspaceID
// (internal/query/local_identity_requests.go) via queryauth, which owns the
// context key and the normalization.
func AuthWorkspaceID(r *http.Request) string {
	auth, _ := queryauth.AuthContextFromContext(r.Context())
	return queryauth.NormalizeAuthContext(auth).WorkspaceID
}
