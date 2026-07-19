// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// This file holds the self-service authorization helpers for the generated
// API-token create/revoke/rotate handlers (issue #5164). They are split out of
// local_identity_api_tokens.go to keep that file under the repo's 500-line cap.
// The security contract they implement is exercised by
// local_identity_api_tokens_selfservice_test.go.

// authorizeTokenMutation admits a caller to a token create/revoke/rotate route
// (issue #5164). An all-scope caller keeps the pre-existing unrestricted admin
// path — including the shared-operator context, which legitimately carries no
// SubjectIDHash. Any other caller is a non-admin self-service actor and must be
// an authenticated browser/scoped subject (SubjectIDHash present); the handler
// then constrains the write to that subject's own tokens. An unauthenticated or
// subject-less non-admin caller is rejected with 401 before any store call.
func (h *LocalIdentityHandler) authorizeTokenMutation(w http.ResponseWriter, r *http.Request) (AuthContext, bool) {
	auth, ok := AuthContextFromContext(r.Context())
	if !ok {
		unauthorizedResponse(w, r)
		return AuthContext{}, false
	}
	auth = normalizeAuthContext(auth)
	if auth.AllScopes {
		return auth, true
	}
	if auth.SubjectIDHash == "" {
		unauthorizedResponse(w, r)
		return AuthContext{}, false
	}
	return auth, true
}

// selfServiceTokenOwner returns the subject hash a revoke/rotate must be scoped
// to, or "" for an all-scope admin whose mutation stays unrestricted. Keeping
// this a single helper guarantees revoke and rotate compute ownership scope
// identically (issue #5164).
func selfServiceTokenOwner(auth AuthContext) string {
	if auth.AllScopes {
		return ""
	}
	return auth.SubjectIDHash
}

// writeSelfServiceTokenNotFound converts an owner-scoped store miss into a
// non-disclosing 404 (issue #5164). It fires only for a self-service caller
// (ownerSubjectIDHash set) when the store reports the token is not one the
// caller owns; an all-scope admin (empty owner hash) is never routed here, so
// the admin path keeps its existing error mapping. Returning 404 for both
// "does not exist" and "exists but not yours" prevents a caller from probing
// another subject's token IDs by error-code differences.
func (h *LocalIdentityHandler) writeSelfServiceTokenNotFound(w http.ResponseWriter, ownerSubjectIDHash string, err error) bool {
	if ownerSubjectIDHash == "" || !errors.Is(err, ErrLocalIdentityAPITokenNotFound) {
		return false
	}
	WriteError(w, http.StatusNotFound, "api token not found")
	return true
}

// enforceSelfServiceTokenCreateScope constrains a non-admin create request to a
// PERSONAL token bound to the caller's OWN subject (issue #5164). A service
// principal is never the caller's own identity, and a browser session cannot
// legitimately name another user's internal user_id, so both are refused with
// 403. The caller's own user_id is resolved from the session subject via the
// same store call self-service TOTP enrollment uses; an unresolvable subject
// fails closed with 403 rather than falling through to a blank owner. On
// success it rewrites the request to the caller's own personal token so the
// downstream create record cannot carry a foreign target.
func (h *LocalIdentityHandler) enforceSelfServiceTokenCreateScope(
	w http.ResponseWriter,
	r *http.Request,
	req *localIdentityAPITokenCreateRequest,
	auth AuthContext,
) bool {
	class := localIdentityDefault(req.TokenClass, localIdentityAPITokenClassPersonal)
	if class != localIdentityAPITokenClassPersonal || strings.TrimSpace(req.ServicePrincipalID) != "" {
		h.auditLocalIdentity(r, governanceaudit.EventTypeTokenLifecycle, governanceaudit.DecisionDenied, "api_token_self_service_scope_denied", "")
		WriteError(w, http.StatusForbidden, "self-service api token creation is limited to your own personal token")
		return false
	}
	userID, found, err := h.Store.ResolveLocalIdentityUserID(r.Context(), auth.SubjectIDHash)
	if err != nil {
		slog.ErrorContext(r.Context(), "resolve self-service api token user id failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to create local identity api token")
		return false
	}
	if !found {
		h.auditLocalIdentity(r, governanceaudit.EventTypeTokenLifecycle, governanceaudit.DecisionDenied, "api_token_self_service_scope_denied", "")
		WriteError(w, http.StatusForbidden, "self-service api token creation requires a resolvable local identity")
		return false
	}
	if supplied := strings.TrimSpace(req.UserID); supplied != "" && supplied != userID {
		h.auditLocalIdentity(r, governanceaudit.EventTypeTokenLifecycle, governanceaudit.DecisionDenied, "api_token_self_service_scope_denied", "")
		WriteError(w, http.StatusForbidden, "self-service api token creation cannot target another user")
		return false
	}
	req.TokenClass = localIdentityAPITokenClassPersonal
	req.UserID = userID
	req.ServicePrincipalID = ""
	return true
}
