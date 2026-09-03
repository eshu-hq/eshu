// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// authMiddlewareWithAllowedReadAudit is authMiddleware plus allowedAudit, the
// F-9 (#5170) allowed-read governance-audit sink, and an explicit route
// policy. A nil allowedAudit keeps every existing caller's behavior
// byte-identical (see recordScopedReadAuthorized's nil guard in
// auth_audit.go); only the *AndAllowedReadAudit constructors pass a real
// value. Kept in its own file, alongside auth.go, so auth.go itself stays
// under the repo's 500-line file cap.
//
// policy used to be hardcoded to the fail-closed zero value here, which was
// invisible until #6450's residual item 1 closed and the scoped-bearer branch
// started reading it: cmd/mcp-server reaches authMiddlewareWithRoutePolicy
// only through this function, so a hardcoded zero value would have refused
// every all-scope bearer on every grant-bound MCP route in every deployment,
// local_no_policy included. It is a parameter so mcp-server can thread the
// same ESHU_GOVERNANCE_MODE-derived policy cmd/api threads.
func authMiddlewareWithAllowedReadAudit(
	token string,
	resolver ScopedTokenResolver,
	sessionResolver BrowserSessionResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
	policy BrowserSessionRoutePolicy,
	authEnforcementConfigured bool,
	oauthChallenge OAuthChallengePolicy,
	allowedAudit GovernanceAuditAppender,
) http.Handler {
	return authMiddlewareWithRoutePolicy(
		token,
		resolver,
		sessionResolver,
		next,
		audit,
		policy,
		authEnforcementConfigured,
		oauthChallenge,
		allowedAudit,
	)
}
