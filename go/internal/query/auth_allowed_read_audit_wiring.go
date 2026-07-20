// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// authMiddlewareWithAllowedReadAudit is authMiddleware plus allowedAudit, the
// F-9 (#5170) allowed-read governance-audit sink. A nil allowedAudit keeps
// every existing caller's behavior byte-identical (see
// recordScopedReadAuthorized's nil guard in auth_audit.go); only
// AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementOAuthChallengeAndAllowedReadAudit
// passes a real value. Kept in its own file, alongside auth.go, so auth.go
// itself stays under the repo's 500-line file cap.
func authMiddlewareWithAllowedReadAudit(
	token string,
	resolver ScopedTokenResolver,
	sessionResolver BrowserSessionResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
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
		BrowserSessionRoutePolicy{},
		authEnforcementConfigured,
		oauthChallenge,
		allowedAudit,
	)
}
