// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// scopedTokenAdvertisedRoutes is the structured, hand-maintained marker
// ledger of every HTTP API route intended to support scoped-token and
// browser-session tenant-filtered access. Each key is exactly the
// "METHOD /path" surface name the generated surface inventory reports for
// that route (capabilitycatalog.LoadSurfaceInventory, category api_route),
// matching the format cmd/capability-inventory's enumerateAPIRoutes derives
// from the served OpenAPI spec.
//
// This is the #5154 gate: the #5150 review retro found that
// GET /api/v0/repositories/{repo_id}/freshness shipped fully wired --
// handler, OpenAPI description, HTTP-API reference docs all promised
// scoped-token support -- while scopedHTTPRouteSupportsTenantFilter (see
// auth_scoped_routes.go) had no matching entry, so every scoped and
// browser-session caller got a middleware 403 before the handler's own grant
// filtering ever ran. Two prior hand audits and a full cold review missed
// it; only a PR review caught it.
//
// The *actual* source of truth for "advertised" is not this ledger: it is
// one of two mutually exclusive OpenAPI markers declared in each route's own
// openapi_paths_*.go operation entry, the same JSON object as the route's
// prose "Scoped tokens receive ..." description --
// "x-scoped-token-support": true for a route a scoped BEARER TOKEN actually
// works against, or "x-browser-session-only": true for a route that clears
// scopedHTTPRouteSupportsTenantFilter but whose handler hard-requires an
// actual browser-session cookie and rejects any bearer token (see
// openAPIScopedTokenSupportRoutes and openAPIBrowserSessionOnlyRoutes in
// auth_scoped_routes_completeness_test.go for both markers' full contracts,
// including the codex PR #5185 review finding that motivated the split: the
// browser-session-identity routes -- GET/DELETE /api/v0/auth/browser-session,
// PATCH /api/v0/auth/browser-session/context, GET /api/v0/auth/sessions --
// originally all carried the token-support marker even though their
// handlers reject a scoped bearer, which would have lied to OpenAPI
// consumers and to this gate). A cold-review pass that compared only this
// ledger against scopedHTTPRouteSupportsTenantFilter would have missed the
// verbatim #5150 recurrence: a route that advertises scoped support in
// prose while never gaining a ledger entry, a matcher, or a marker would
// pass a ledger-only gate silently. This ledger is instead a secondary,
// human-curated cross-check kept in lockstep with the marker union (an
// editorial "yes, this route is meant to be tenant-scoped, one way or the
// other" declaration, the same way latestGenerationCTEQueries --
// go/internal/storage/postgres/ingestion_latest_generation_cte_test.go --
// hand-lists every production query that must satisfy a property rather
// than grepping for one).
//
// TestScopedTokenAllowlistCompleteness (auth_scoped_routes_completeness_test.go)
// fails when a route carries both markers at once, when the marker union
// and scopedHTTPRouteSupportsTenantFilter disagree in either direction, and
// separately when the marker union and this ledger disagree in either
// direction. TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware
// sources its route set from the "x-scoped-token-support" marker only (not
// this ledger) and proves every one of those routes actually clears a real
// AuthMiddlewareWithScopedTokens round trip under a scoped bearer token,
// rather than relying on a per-route bare-mux handler test (the #5150
// false-green pattern for that specific failure shape) or a hand-authored
// regression test someone forgot to add.
// TestScopedBearerTokenRejectedByBrowserSessionOnlyRoutes is its inverse for
// "x-browser-session-only" routes: it mounts the real production handler
// (not a stub) and proves a scoped bearer token never gets a 2xx.
//
// To add a new scoped route: wire its matcher into
// scopedHTTPRouteSupportsTenantFilter, add the marker that matches the
// handler's actual auth.Mode requirement ("x-scoped-token-support": true if
// a scoped bearer token works, "x-browser-session-only": true if the
// handler requires an actual browser-session cookie) to its operation entry
// in the relevant openapi_paths_*.go file, and add its "METHOD /path"
// surface name here. Missing any one of the three, or picking the wrong
// marker for the handler's real auth.Mode requirement, fails
// TestScopedTokenAllowlistCompleteness, TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware,
// or TestScopedBearerTokenRejectedByBrowserSessionOnlyRoutes. Removing a
// route without deleting its entry here fails the completeness test's
// staleness check.
//
// Each entry also records WHY that route is on the allowlist, as a
// scopedRouteClass (auth_browser_session_route_policy.go). The class is what
// browserSessionRouteDenialReason and scopedBearerRouteDenialReason both
// consult once an all-scope caller arrives, cookie session or bearer token: a
// route whose handler binds the caller's repository or scope grant has
// nothing left to bind for an all-scope caller, so it stays behind the
// BrowserSessionRoutePolicy mode check, while an identity-bound or
// tenant-data-free route has no caller grant to make inert and is admitted
// on its own. Every entry carries its class explicitly, with no zero-value
// shorthand, so a reviewer reads the reason on the same line as the route.
// The zero value is scopedRouteGrantBound, so a route added here without a
// class fails closed rather than opening itself to all-scope callers.
var scopedTokenAdvertisedRoutes = map[string]scopedRouteClass{
	"DELETE /api/v0/auth/admin/idp-group-mappings/{mapping_ref}":                    scopedRouteIdentityBound,
	"DELETE /api/v0/auth/browser-session":                                           scopedRouteIdentityBound,
	"GET /api/v0/auth/admin/api-tokens":                                             scopedRouteIdentityBound,
	"GET /api/v0/auth/admin/audit/events":                                           scopedRouteIdentityBound,
	"GET /api/v0/auth/admin/audit/summary":                                          scopedRouteIdentityBound,
	"GET /api/v0/auth/admin/idp-group-mappings":                                     scopedRouteIdentityBound,
	"GET /api/v0/auth/admin/idp-providers":                                          scopedRouteIdentityBound,
	"GET /api/v0/auth/admin/provider-configs":                                       scopedRouteIdentityBound,
	"GET /api/v0/auth/admin/provider-configs/{provider_config_id}":                  scopedRouteIdentityBound,
	"GET /api/v0/auth/admin/provider-configs/{provider_config_id}/revisions":        scopedRouteIdentityBound,
	"GET /api/v0/auth/admin/role-assignments":                                       scopedRouteIdentityBound,
	"GET /api/v0/auth/admin/roles":                                                  scopedRouteIdentityBound,
	"GET /api/v0/auth/admin/sign-in-policy":                                         scopedRouteIdentityBound,
	"GET /api/v0/auth/browser-session":                                              scopedRouteIdentityBound,
	"GET /api/v0/auth/local/api-tokens":                                             scopedRouteIdentityBound,
	"GET /api/v0/auth/local/invitations":                                            scopedRouteIdentityBound,
	"GET /api/v0/auth/profile":                                                      scopedRouteIdentityBound,
	"GET /api/v0/auth/sessions":                                                     scopedRouteIdentityBound,
	"GET /api/v0/capabilities":                                                      scopedRouteTenantDataFree,
	"GET /api/v0/ci-cd/run-correlations":                                            scopedRouteGrantBound,
	"GET /api/v0/ci-cd/run-correlations/count":                                      scopedRouteGrantBound,
	"GET /api/v0/ci-cd/run-correlations/inventory":                                  scopedRouteGrantBound,
	"GET /api/v0/cloud/inventory":                                                   scopedRouteGrantBound,
	"GET /api/v0/cloud/resources":                                                   scopedRouteGrantBound,
	"GET /api/v0/collector-extraction-readiness":                                    scopedRouteTenantDataFree,
	"GET /api/v0/collector-extraction-readiness/{family}":                           scopedRouteTenantDataFree,
	"GET /api/v0/codeowners/ownership":                                              scopedRouteGrantBound,
	"GET /api/v0/collector-readiness":                                               scopedRouteDeploymentScoped,
	"GET /api/v0/component-extensions":                                              scopedRouteDeploymentScoped,
	"GET /api/v0/component-extensions/{component_id}/diagnostics":                   scopedRouteDeploymentScoped,
	"GET /api/v0/documentation/evidence-packets/{packet_id}/freshness":              scopedRouteGrantBound,
	"GET /api/v0/documentation/facts":                                               scopedRouteGrantBound,
	"GET /api/v0/documentation/findings":                                            scopedRouteGrantBound,
	"GET /api/v0/documentation/findings/count":                                      scopedRouteGrantBound,
	"GET /api/v0/documentation/findings/inventory":                                  scopedRouteGrantBound,
	"GET /api/v0/documentation/findings/{finding_id}/evidence-packet":               scopedRouteGrantBound,
	"GET /api/v0/ecosystem/overview":                                                scopedRouteGrantBound,
	"GET /api/v0/entities/{entity_id}/context":                                      scopedRouteGrantBound,
	"GET /api/v0/evidence/admission-decisions":                                      scopedRouteGrantBound,
	"GET /api/v0/evidence/relationships/{resolved_id}":                              scopedRouteGrantBound,
	"GET /api/v0/fact-schema-versions":                                              scopedRouteTenantDataFree,
	"GET /api/v0/fact-schema-versions/{fact_kind}":                                  scopedRouteTenantDataFree,
	"GET /api/v0/freshness/changed-since":                                           scopedRouteGrantBound,
	"GET /api/v0/freshness/generations":                                             scopedRouteGrantBound,
	"GET /api/v0/iac/resources":                                                     scopedRouteGrantBound,
	"GET /api/v0/incidents/{incident_id}/context":                                   scopedRouteGrantBound,
	"POST /api/v0/compare/environments":                                             scopedRouteGrantBound,
	"POST /api/v0/impact/blast-radius":                                              scopedRouteGrantBound,
	"POST /api/v0/impact/change-surface":                                            scopedRouteGrantBound,
	"POST /api/v0/impact/change-surface/investigate":                                scopedRouteGrantBound,
	"POST /api/v0/impact/contracts":                                                 scopedRouteGrantBound,
	"POST /api/v0/impact/deployment-config-influence":                               scopedRouteGrantBound,
	"POST /api/v0/impact/developer-change-plan":                                     scopedRouteGrantBound,
	"POST /api/v0/impact/pre-change":                                                scopedRouteGrantBound,
	"POST /api/v0/impact/resource-investigation":                                    scopedRouteGrantBound,
	"POST /api/v0/impact/trace-deployment-chain":                                    scopedRouteGrantBound,
	"GET /api/v0/infra/resources/count":                                             scopedRouteGrantBound,
	"GET /api/v0/infra/resources/inventory":                                         scopedRouteGrantBound,
	"GET /api/v0/investigation-workflows":                                           scopedRouteTenantDataFree,
	"GET /api/v0/investigations/deployable-unit/packet":                             scopedRouteGrantBound,
	"GET /api/v0/investigations/drift/packet":                                       scopedRouteGrantBound,
	"GET /api/v0/investigations/services/{service_name}":                            scopedRouteGrantBound,
	"GET /api/v0/investigations/supply-chain/impact/packet":                         scopedRouteGrantBound,
	"GET /api/v0/kubernetes/correlations":                                           scopedRouteGrantBound,
	"GET /api/v0/observability/coverage/correlations":                               scopedRouteGrantBound,
	"GET /api/v0/package-registry/correlations":                                     scopedRouteGrantBound,
	"GET /api/v0/package-registry/dependencies":                                     scopedRouteGrantBound,
	"GET /api/v0/package-registry/dependency-chains":                                scopedRouteGrantBound,
	"GET /api/v0/package-registry/packages":                                         scopedRouteGrantBound,
	"GET /api/v0/package-registry/packages/count":                                   scopedRouteGrantBound,
	"GET /api/v0/package-registry/packages/inventory":                               scopedRouteGrantBound,
	"GET /api/v0/package-registry/versions":                                         scopedRouteGrantBound,
	"GET /api/v0/query-playbooks":                                                   scopedRouteTenantDataFree,
	"GET /api/v0/repositories":                                                      scopedRouteGrantBound,
	"GET /api/v0/repositories/by-language":                                          scopedRouteGrantBound,
	"GET /api/v0/repositories/language-inventory":                                   scopedRouteGrantBound,
	"GET /api/v0/repositories/{repo_id}/context":                                    scopedRouteGrantBound,
	"GET /api/v0/repositories/{repo_id}/coverage":                                   scopedRouteGrantBound,
	"GET /api/v0/repositories/{repo_id}/freshness":                                  scopedRouteGrantBound,
	"GET /api/v0/repositories/{repo_id}/stats":                                      scopedRouteGrantBound,
	"GET /api/v0/repositories/{repo_id}/story":                                      scopedRouteGrantBound,
	"GET /api/v0/repositories/{repo_id}/tree":                                       scopedRouteGrantBound,
	"GET /api/v0/replatforming/selectors":                                           scopedRouteGrantBound,
	"GET /api/v0/secrets-iam/identity-trust-chains":                                 scopedRouteGrantBound,
	"GET /api/v0/secrets-iam/posture-gaps":                                          scopedRouteGrantBound,
	"GET /api/v0/secrets-iam/posture-summary":                                       scopedRouteGrantBound,
	"GET /api/v0/secrets-iam/privilege-posture-observations":                        scopedRouteGrantBound,
	"GET /api/v0/secrets-iam/secret-access-paths":                                   scopedRouteGrantBound,
	"GET /api/v0/semantic/code-hints":                                               scopedRouteGrantBound,
	"GET /api/v0/semantic/documentation-observations":                               scopedRouteGrantBound,
	"GET /api/v0/service-catalog/correlations":                                      scopedRouteGrantBound,
	"GET /api/v0/services/{service_name}/context":                                   scopedRouteGrantBound,
	"GET /api/v0/services/{service_name}/intelligence-report":                       scopedRouteGrantBound,
	"GET /api/v0/services/{service_name}/story":                                     scopedRouteGrantBound,
	"GET /api/v0/status/answer-narration":                                           scopedRouteDeploymentScoped,
	"GET /api/v0/status/collector-readiness":                                        scopedRouteDeploymentScoped,
	"GET /api/v0/status/collectors":                                                 scopedRouteDeploymentScoped,
	"GET /api/v0/status/freshness-causality":                                        scopedRouteDeploymentScoped,
	"GET /api/v0/status/governance":                                                 scopedRouteDeploymentScoped,
	"GET /api/v0/status/hosted-readiness":                                           scopedRouteDeploymentScoped,
	"GET /api/v0/status/ingesters":                                                  scopedRouteDeploymentScoped,
	"GET /api/v0/status/ingesters/{ingester}":                                       scopedRouteDeploymentScoped,
	"GET /api/v0/status/operations":                                                 scopedRouteGrantBound,
	"GET /api/v0/status/operator-control-plane":                                     scopedRouteDeploymentScoped,
	"GET /api/v0/status/semantic-extraction":                                        scopedRouteDeploymentScoped,
	"GET /api/v0/supply-chain/advisories/evidence":                                  scopedRouteGrantBound,
	"GET /api/v0/supply-chain/container-images/identities":                          scopedRouteGrantBound,
	"GET /api/v0/supply-chain/container-images/identities/count":                    scopedRouteGrantBound,
	"GET /api/v0/supply-chain/container-images/identities/inventory":                scopedRouteGrantBound,
	"GET /api/v0/supply-chain/impact/explain":                                       scopedRouteGrantBound,
	"GET /api/v0/supply-chain/impact/findings":                                      scopedRouteGrantBound,
	"GET /api/v0/supply-chain/impact/findings/count":                                scopedRouteGrantBound,
	"GET /api/v0/supply-chain/impact/inventory":                                     scopedRouteGrantBound,
	"GET /api/v0/supply-chain/sbom-attestations/attachments":                        scopedRouteGrantBound,
	"GET /api/v0/supply-chain/sbom-attestations/attachments/count":                  scopedRouteGrantBound,
	"GET /api/v0/supply-chain/sbom-attestations/attachments/inventory":              scopedRouteGrantBound,
	"GET /api/v0/supply-chain/security-alerts/reconciliations":                      scopedRouteGrantBound,
	"GET /api/v0/supply-chain/security-alerts/reconciliations/count":                scopedRouteGrantBound,
	"GET /api/v0/supply-chain/security-alerts/reconciliations/inventory":            scopedRouteGrantBound,
	"GET /api/v0/supply-chain/vulnerability-scanner/contract":                       scopedRouteTenantDataFree,
	"GET /api/v0/surface-inventory":                                                 scopedRouteTenantDataFree,
	"GET /api/v0/work-items/evidence":                                               scopedRouteGrantBound,
	"GET /api/v0/workloads/{workload_id}/context":                                   scopedRouteGrantBound,
	"GET /api/v0/workloads/{workload_id}/story":                                     scopedRouteGrantBound,
	"PATCH /api/v0/auth/admin/sign-in-policy":                                       scopedRouteIdentityBound,
	"PATCH /api/v0/auth/browser-session/context":                                    scopedRouteIdentityBound,
	"POST /api/v0/admin/dead-letters/query":                                         scopedRouteGrantBound,
	"POST /api/v0/admin/input-invalid-facts/query":                                  scopedRouteGrantBound,
	"POST /api/v0/ask":                                                              scopedRouteTransitive,
	"POST /api/v0/auth/admin/idp-group-mappings":                                    scopedRouteIdentityBound,
	"POST /api/v0/auth/admin/provider-configs":                                      scopedRouteIdentityBound,
	"POST /api/v0/auth/admin/provider-configs/{provider_config_id}":                 scopedRouteIdentityBound,
	"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/disable":         scopedRouteIdentityBound,
	"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/enable":          scopedRouteIdentityBound,
	"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/revert":          scopedRouteIdentityBound,
	"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/test-connection": scopedRouteIdentityBound,
	"POST /api/v0/auth/admin/role-assignments":                                      scopedRouteIdentityBound,
	"POST /api/v0/auth/admin/role-assignments/revoke":                               scopedRouteIdentityBound,
	"POST /api/v0/auth/browser-session":                                             scopedRouteIdentityBound,
	"POST /api/v0/auth/local/api-tokens":                                            scopedRouteIdentityBound,
	"POST /api/v0/auth/local/api-tokens/{token_id}/revoke":                          scopedRouteIdentityBound,
	"POST /api/v0/auth/local/api-tokens/{token_id}/rotate":                          scopedRouteIdentityBound,
	"POST /api/v0/auth/local/invitations/{invite_id}/revoke":                        scopedRouteIdentityBound,
	"POST /api/v0/auth/local/mfa/totp/begin":                                        scopedRouteIdentityBound,
	"POST /api/v0/auth/local/mfa/totp/confirm":                                      scopedRouteIdentityBound,
	"POST /api/v0/aws/runtime-drift/findings":                                       scopedRouteGrantBound,
	"POST /api/v0/cloud/runtime-drift/findings":                                     scopedRouteGrantBound,
	"POST /api/v0/terraform/config-state-drift/findings":                            scopedRouteGrantBound,
	"POST /api/v0/code/flow/cfg-summary":                                            scopedRouteGrantBound,
	"POST /api/v0/code/flow/pdg-summary":                                            scopedRouteGrantBound,
	"POST /api/v0/code/flow/reaching-def":                                           scopedRouteGrantBound,
	"POST /api/v0/code/flow/taint-path":                                             scopedRouteGrantBound,
	"POST /api/v0/code/routes/callers":                                              scopedRouteGrantBound,
	"POST /api/v0/code/search":                                                      scopedRouteGrantBound,
	"POST /api/v0/code/complexity":                                                  scopedRouteGrantBound,
	"POST /api/v0/code/quality/inspect":                                             scopedRouteGrantBound,
	"POST /api/v0/code/call-graph/metrics":                                          scopedRouteGrantBound,
	"POST /api/v0/code/dead-code":                                                   scopedRouteGrantBound,
	"POST /api/v0/code/dead-code/cross-repo":                                        scopedRouteGrantBound,
	"POST /api/v0/code/dead-code/investigate":                                       scopedRouteGrantBound,
	"POST /api/v0/code/topics/investigate":                                          scopedRouteGrantBound,
	"POST /api/v0/code/security/secrets/investigate":                                scopedRouteGrantBound,
	"POST /api/v0/code/structure/inventory":                                         scopedRouteGrantBound,
	"POST /api/v0/code/symbols/search":                                              scopedRouteGrantBound,
	"POST /api/v0/code/language-query":                                              scopedRouteGrantBound,
	"POST /api/v0/content/entities/read":                                            scopedRouteGrantBound,
	"POST /api/v0/content/entities/search":                                          scopedRouteGrantBound,
	"POST /api/v0/content/files/lines":                                              scopedRouteGrantBound,
	"POST /api/v0/content/files/read":                                               scopedRouteGrantBound,
	"POST /api/v0/content/files/search":                                             scopedRouteGrantBound,
	"POST /api/v0/ecosystem/graph-summary":                                          scopedRouteGrantBound,
	"POST /api/v0/entities/resolve":                                                 scopedRouteGrantBound,
	"POST /api/v0/evidence/citations":                                               scopedRouteGrantBound,
	"POST /api/v0/iac/dead":                                                         scopedRouteGrantBound,
	"POST /api/v0/iac/management-status":                                            scopedRouteGrantBound,
	"POST /api/v0/iac/management-status/explain":                                    scopedRouteGrantBound,
	"POST /api/v0/iac/terraform-import-plan/candidates":                             scopedRouteGrantBound,
	"POST /api/v0/iac/unmanaged-resources":                                          scopedRouteGrantBound,
	"POST /api/v0/infra/relationships":                                              scopedRouteGrantBound,
	"POST /api/v0/infra/resources/search":                                           scopedRouteGrantBound,
	"POST /api/v0/investigation-workflows/resolve":                                  scopedRouteTenantDataFree,
	"POST /api/v0/query-playbooks/resolve":                                          scopedRouteTenantDataFree,
	"POST /api/v0/relationships/edges":                                              scopedRouteGrantBound,
	"POST /api/v0/replatforming/ownership-packets":                                  scopedRouteGrantBound,
	"POST /api/v0/replatforming/plans":                                              scopedRouteGrantBound,
	"POST /api/v0/replatforming/rollups":                                            scopedRouteGrantBound,
	"POST /api/v0/search/semantic":                                                  scopedRouteGrantBound,
	"POST /api/v0/visualizations/derive":                                            scopedRouteTenantDataFree,
}
