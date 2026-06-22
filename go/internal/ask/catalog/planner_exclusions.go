package catalog

// plannerExcludedSurfaces is the curated set of implemented surfaces that must
// never appear in Ask Eshu's answer-planning catalog. Most entries are
// side-effecting admin/recovery routes; browser-session routes are session
// control surfaces that either mutate server-side session state or expose only
// caller-local auth metadata, so they are not retrieval paths for answering
// repository, graph, runtime, or cloud questions. The local-credential, OIDC,
// and SAML auth routes are login, account-recovery, and SSO-handshake surfaces
// that authenticate callers or redirect browsers rather than return repository,
// graph, runtime, or cloud facts, so none of them are retrieval paths either.
//
// Completeness is enforced by tests: every name here must be an implemented
// api_route in the inventory (no stale entries), every name here must be absent
// from the parsed catalog, and every implemented surface must be either a
// catalog entry or listed here (nothing silently vanishes).
func plannerExcludedSurfaces() map[string]struct{} {
	return map[string]struct{}{
		"DELETE /api/v0/auth/browser-session":        {}, // revokes the caller's browser session
		"GET /api/v0/auth/browser-session":           {}, // reads caller-local session metadata only
		"PATCH /api/v0/auth/browser-session/context": {}, // switches the caller's tenant/workspace context
		"POST /api/v0/admin/backfill":                {}, // RequestBackfill enqueues backfill work
		"POST /api/v0/admin/dead-letter":             {}, // DeadLetterWorkItems dead-letters queued work
		"POST /api/v0/admin/refinalize":              {}, // re-enqueues projector work for the given scope
		"POST /api/v0/admin/reindex":                 {}, // RequestReindex enqueues ingester reindex work
		"POST /api/v0/admin/replay":                  {}, // ReplayFailed re-processes failed work items
		"POST /api/v0/admin/skip":                    {}, // skips queued work items
		"POST /api/v0/auth/browser-session":          {}, // creates a caller browser session and cookies
		// Local-credential auth routes: login, bootstrap, break-glass recovery,
		// invitations, and per-user account administration. All authenticate or
		// mutate accounts rather than return facts.
		"POST /api/v0/auth/local/bootstrap":                 {}, // bootstraps the initial local admin identity
		"POST /api/v0/auth/local/break-glass":               {}, // issues a break-glass recovery challenge
		"POST /api/v0/auth/local/break-glass/session":       {}, // exchanges a break-glass challenge for a session
		"POST /api/v0/auth/local/invitations":               {}, // creates a local-account invitation
		"POST /api/v0/auth/local/invitations/accept":        {}, // accepts a local-account invitation
		"POST /api/v0/auth/local/login":                     {}, // authenticates a local credential
		"POST /api/v0/auth/local/users/{user_id}/disable":   {}, // disables a local user account
		"POST /api/v0/auth/local/users/{user_id}/mfa-reset": {}, // resets a local user's MFA enrollment
		"POST /api/v0/auth/local/users/{user_id}/password":  {}, // rotates a local user's password
		// OIDC SSO handshake routes: redirect the browser and consume the IdP
		// callback; neither returns repository, graph, runtime, or cloud facts.
		"GET /api/v0/auth/oidc/callback": {}, // consumes the OIDC IdP callback
		"GET /api/v0/auth/oidc/login":    {}, // redirects the browser to the OIDC IdP
		// SAML SSO handshake routes: per-provider login redirect, metadata
		// document, and assertion-consumer endpoint. All are SSO plumbing.
		"GET /api/v0/auth/saml/providers/{provider_id}/login":    {}, // redirects to the SAML IdP
		"GET /api/v0/auth/saml/providers/{provider_id}/metadata": {}, // serves SP SAML metadata
		"POST /api/v0/auth/saml/providers/{provider_id}/acs":     {}, // consumes the SAML assertion (ACS)
	}
}

// isPlannerExcludedSurface reports whether a surface name is deliberately kept
// out of Ask Eshu's retrieval catalog.
func isPlannerExcludedSurface(name string) bool {
	_, ok := plannerExcludedSurfaces()[name]
	return ok
}
