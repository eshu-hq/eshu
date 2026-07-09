// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
		"DELETE /api/v0/auth/admin/idp-group-mappings/{mapping_ref}":                    {}, // tombstones an IdP group->role mapping (admin mutation)
		"DELETE /api/v0/auth/browser-session":                                           {}, // revokes the caller's browser session
		"GET /api/v0/auth/admin/api-tokens":                                             {}, // lists the tenant's generated API token metadata (admin)
		"GET /api/v0/auth/admin/audit/events":                                           {}, // lists the tenant's governance audit events (admin)
		"GET /api/v0/auth/admin/audit/summary":                                          {}, // aggregate governance audit counts (admin)
		"GET /api/v0/auth/admin/idp-group-mappings":                                     {}, // lists the tenant's IdP group->role mappings (admin)
		"GET /api/v0/auth/admin/idp-providers":                                          {}, // lists the tenant's configured IdP providers (admin)
		"GET /api/v0/auth/admin/provider-configs":                                       {}, // lists the tenant's DB-backed/env identity provider configs (admin)
		"GET /api/v0/auth/admin/provider-configs/{provider_config_id}":                  {}, // reads one identity provider config's metadata (admin)
		"GET /api/v0/auth/admin/provider-configs/{provider_config_id}/revisions":        {}, // lists one identity provider config's revision history (admin)
		"GET /api/v0/auth/admin/role-assignments":                                       {}, // lists the tenant's membership-role assignments (admin)
		"GET /api/v0/auth/admin/roles":                                                  {}, // lists the tenant's roles and grants (admin)
		"GET /api/v0/auth/admin/sign-in-policy":                                         {}, // reads the tenant's full sign-in policy (admin)
		"GET /api/v0/auth/browser-session":                                              {}, // reads caller-local session metadata only
		"GET /api/v0/auth/local/api-tokens":                                             {}, // lists the caller's own API token metadata
		"GET /api/v0/auth/local/invitations":                                            {}, // lists the tenant's invitations metadata (admin)
		"GET /api/v0/auth/profile":                                                      {}, // reads the caller's own identity profile
		"GET /api/v0/auth/providers":                                                    {}, // lists configured login providers for the tenant (pre-auth discovery)
		"GET /api/v0/auth/sessions":                                                     {}, // lists the caller's own browser sessions
		"GET /api/v0/auth/setup-state":                                                  {}, // reports first-run setup wizard needs_setup/bootstrap_mode (pre-auth)
		"GET /api/v0/auth/sign-in-policy":                                               {}, // public require_sso hint for the login page (pre-auth)
		"PATCH /api/v0/auth/admin/sign-in-policy":                                       {}, // updates the tenant's sign-in policy (admin mutation)
		"PATCH /api/v0/auth/browser-session/context":                                    {}, // switches the caller's tenant/workspace context
		"POST /api/v0/admin/backfill":                                                   {}, // RequestBackfill enqueues backfill work
		"POST /api/v0/admin/dead-letter":                                                {}, // DeadLetterWorkItems dead-letters queued work
		"POST /api/v0/admin/recover-generations":                                        {}, // re-drives wedged generation scopes through recovery
		"POST /api/v0/admin/refinalize":                                                 {}, // re-enqueues projector work for the given scope
		"POST /api/v0/admin/reindex":                                                    {}, // RequestReindex enqueues ingester reindex work
		"POST /api/v0/admin/replay":                                                     {}, // ReplayFailed re-processes failed work items
		"POST /api/v0/admin/skip":                                                       {}, // skips queued work items
		"POST /api/v0/auth/admin/idp-group-mappings":                                    {}, // creates an IdP group->role mapping (admin mutation)
		"POST /api/v0/auth/admin/provider-configs":                                      {}, // creates a DB-backed identity provider config (admin mutation)
		"POST /api/v0/auth/admin/provider-configs/{provider_config_id}":                 {}, // creates a new active revision for a provider config (admin mutation)
		"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/disable":         {}, // disables a provider config (admin mutation)
		"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/enable":          {}, // enables a provider config after test-connection (admin mutation)
		"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/revert":          {}, // reverts a provider config to a prior revision (admin mutation)
		"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/test-connection": {}, // tests a provider config's stored connection material (admin mutation)
		"POST /api/v0/auth/admin/role-assignments":                                      {}, // grants a membership-role assignment (admin mutation)
		"POST /api/v0/auth/admin/role-assignments/revoke":                               {}, // revokes a membership-role assignment (admin mutation)
		"POST /api/v0/auth/browser-session":                                             {}, // creates a caller browser session and cookies
		// Local-credential auth routes: login, bootstrap, break-glass recovery,
		// invitations, and per-user account administration. All authenticate or
		// mutate accounts rather than return facts.
		"POST /api/v0/auth/local/api-tokens":                     {}, // mints a local API token credential
		"POST /api/v0/auth/local/api-tokens/{token_id}/revoke":   {}, // revokes a local API token credential
		"POST /api/v0/auth/local/api-tokens/{token_id}/rotate":   {}, // rotates a local API token credential
		"POST /api/v0/auth/local/bootstrap":                      {}, // bootstraps the initial local admin identity
		"POST /api/v0/auth/local/break-glass":                    {}, // issues a break-glass recovery challenge
		"POST /api/v0/auth/local/break-glass/session":            {}, // exchanges a break-glass challenge for a session
		"POST /api/v0/auth/local/invitations":                    {}, // creates a local-account invitation
		"POST /api/v0/auth/local/invitations/accept":             {}, // accepts a local-account invitation
		"POST /api/v0/auth/local/invitations/{invite_id}/revoke": {}, // revokes a local-account invitation (admin mutation)
		"POST /api/v0/auth/local/login":                          {}, // authenticates a local credential
		"POST /api/v0/auth/local/users/{user_id}/disable":        {}, // disables a local user account
		"POST /api/v0/auth/local/users/{user_id}/mfa-reset":      {}, // resets a local user's MFA enrollment
		"POST /api/v0/auth/local/users/{user_id}/password":       {}, // rotates a local user's password
		// First-run setup wizard routes (#4965): guided claim->admin->MFA flow
		// that authenticates and provisions the first admin identity rather
		// than returning repository, graph, runtime, or cloud facts.
		"POST /api/v0/auth/setup/admin": {}, // wizard step 2: sets the admin password
		"POST /api/v0/auth/setup/claim": {}, // wizard step 1: verifies the bootstrap credential
		"POST /api/v0/auth/setup/mfa":   {}, // wizard step 3: enrolls MFA and issues a session
		// OIDC SSO handshake routes: redirect the browser and consume the IdP
		// callback; neither returns repository, graph, runtime, or cloud facts.
		"GET /api/v0/auth/oidc/callback": {}, // consumes the OIDC IdP callback
		"GET /api/v0/auth/oidc/login":    {}, // redirects the browser to the OIDC IdP
		// SAML SSO handshake routes: per-provider login redirect, metadata
		// document, and assertion-consumer endpoint. All are SSO plumbing.
		"GET /api/v0/auth/saml/providers/{provider_id}/login":    {}, // redirects to the SAML IdP
		"GET /api/v0/auth/saml/providers/{provider_id}/metadata": {}, // serves SP SAML metadata
		"POST /api/v0/auth/saml/providers/{provider_id}/acs":     {}, // consumes the SAML assertion (ACS)
		// First-run setup wizard routes (#4965): guided claim->admin->MFA flow
		// that authenticates and provisions the first admin identity rather
		// than returning repository, graph, runtime, or cloud facts.
		"POST /api/v0/auth/setup/admin": {}, // wizard step 2: sets the admin password
		"POST /api/v0/auth/setup/claim": {}, // wizard step 1: verifies the bootstrap credential
		"POST /api/v0/auth/setup/mfa":   {}, // wizard step 3: enrolls MFA and issues a session
	}
}

// isPlannerExcludedSurface reports whether a surface name is deliberately kept
// out of Ask Eshu's retrieval catalog.
func isPlannerExcludedSurface(name string) bool {
	_, ok := plannerExcludedSurfaces()[name]
	return ok
}
