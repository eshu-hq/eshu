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
// scoped-token support -- while scopedHTTPRouteSupportsTenantFilder (see
// auth_scoped_routes.go) had no matching entry, so every scoped and
// browser-session caller got a middleware 403 before the handler's own grant
// filtering ever ran. Two prior hand audits and a full cold review missed
// it; only a PR review caught it.
//
// This ledger is deliberately independent of scopedHTTPRouteSupportsTenantFilter's
// implementation: it is populated by a developer declaring "this route is
// meant to support scoped access" (an editorial decision, not a derived
// fact), the same way latestGenerationCTEQueries
// (go/internal/storage/postgres/ingestion_latest_generation_cte_test.go)
// hand-lists every production query that must satisfy a property rather than
// grepping for one. TestScopedTokenAllowlistCompleteness
// (auth_scoped_routes_completeness_test.go) then fails when this ledger and
// scopedHTTPRouteSupportsTenantFilter disagree in either direction, and
// TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware
// proves every entry actually clears a real AuthMiddlewareWithScopedTokens
// round trip rather than relying on a per-route bare-mux handler test (the
// #5150 false-green pattern) or a hand-authored regression test someone
// forgot to add.
//
// To add a new scoped route: wire its matcher into
// scopedHTTPRouteSupportsTenantFilter as usual, then add its
// "METHOD /path" surface name here. Forgetting either half fails
// TestScopedTokenAllowlistCompleteness. Removing a route without deleting
// its entry here fails the same test's staleness check.
var scopedTokenAdvertisedRoutes = map[string]struct{}{
	"DELETE /api/v0/auth/admin/idp-group-mappings/{mapping_ref}":                    {},
	"DELETE /api/v0/auth/browser-session":                                           {},
	"GET /api/v0/auth/admin/api-tokens":                                             {},
	"GET /api/v0/auth/admin/audit/events":                                           {},
	"GET /api/v0/auth/admin/audit/summary":                                          {},
	"GET /api/v0/auth/admin/idp-group-mappings":                                     {},
	"GET /api/v0/auth/admin/idp-providers":                                          {},
	"GET /api/v0/auth/admin/provider-configs":                                       {},
	"GET /api/v0/auth/admin/provider-configs/{provider_config_id}":                  {},
	"GET /api/v0/auth/admin/provider-configs/{provider_config_id}/revisions":        {},
	"GET /api/v0/auth/admin/role-assignments":                                       {},
	"GET /api/v0/auth/admin/roles":                                                  {},
	"GET /api/v0/auth/admin/sign-in-policy":                                         {},
	"GET /api/v0/auth/browser-session":                                              {},
	"GET /api/v0/auth/local/api-tokens":                                             {},
	"GET /api/v0/auth/local/invitations":                                            {},
	"GET /api/v0/auth/profile":                                                      {},
	"GET /api/v0/auth/sessions":                                                     {},
	"GET /api/v0/capabilities":                                                      {},
	"GET /api/v0/ci-cd/run-correlations":                                            {},
	"GET /api/v0/ci-cd/run-correlations/count":                                      {},
	"GET /api/v0/ci-cd/run-correlations/inventory":                                  {},
	"GET /api/v0/collector-extraction-readiness":                                    {},
	"GET /api/v0/collector-extraction-readiness/{family}":                           {},
	"GET /api/v0/collector-readiness":                                               {},
	"GET /api/v0/component-extensions":                                              {},
	"GET /api/v0/component-extensions/{component_id}/diagnostics":                   {},
	"GET /api/v0/documentation/evidence-packets/{packet_id}/freshness":              {},
	"GET /api/v0/documentation/facts":                                               {},
	"GET /api/v0/documentation/findings":                                            {},
	"GET /api/v0/documentation/findings/count":                                      {},
	"GET /api/v0/documentation/findings/inventory":                                  {},
	"GET /api/v0/documentation/findings/{finding_id}/evidence-packet":               {},
	"GET /api/v0/entities/{entity_id}/context":                                      {},
	"GET /api/v0/evidence/admission-decisions":                                      {},
	"GET /api/v0/fact-schema-versions":                                              {},
	"GET /api/v0/fact-schema-versions/{fact_kind}":                                  {},
	"GET /api/v0/iac/resources":                                                     {},
	"GET /api/v0/incidents/{incident_id}/context":                                   {},
	"GET /api/v0/infra/resources/count":                                             {},
	"GET /api/v0/infra/resources/inventory":                                         {},
	"GET /api/v0/investigation-workflows":                                           {},
	"GET /api/v0/investigations/deployable-unit/packet":                             {},
	"GET /api/v0/investigations/services/{service_name}":                            {},
	"GET /api/v0/investigations/supply-chain/impact/packet":                         {},
	"GET /api/v0/package-registry/correlations":                                     {},
	"GET /api/v0/package-registry/dependency-chains":                                {},
	"GET /api/v0/query-playbooks":                                                   {},
	"GET /api/v0/repositories":                                                      {},
	"GET /api/v0/repositories/{repo_id}/freshness":                                  {},
	"GET /api/v0/secrets-iam/identity-trust-chains":                                 {},
	"GET /api/v0/secrets-iam/posture-gaps":                                          {},
	"GET /api/v0/secrets-iam/posture-summary":                                       {},
	"GET /api/v0/secrets-iam/privilege-posture-observations":                        {},
	"GET /api/v0/secrets-iam/secret-access-paths":                                   {},
	"GET /api/v0/semantic/code-hints":                                               {},
	"GET /api/v0/semantic/documentation-observations":                               {},
	"GET /api/v0/service-catalog/correlations":                                      {},
	"GET /api/v0/services/{service_name}/context":                                   {},
	"GET /api/v0/services/{service_name}/intelligence-report":                       {},
	"GET /api/v0/services/{service_name}/story":                                     {},
	"GET /api/v0/status/answer-narration":                                           {},
	"GET /api/v0/status/collector-readiness":                                        {},
	"GET /api/v0/status/collectors":                                                 {},
	"GET /api/v0/status/freshness-causality":                                        {},
	"GET /api/v0/status/governance":                                                 {},
	"GET /api/v0/status/hosted-readiness":                                           {},
	"GET /api/v0/status/ingesters":                                                  {},
	"GET /api/v0/status/ingesters/{ingester}":                                       {},
	"GET /api/v0/status/operations":                                                 {},
	"GET /api/v0/status/operator-control-plane":                                     {},
	"GET /api/v0/status/semantic-extraction":                                        {},
	"GET /api/v0/supply-chain/advisories/evidence":                                  {},
	"GET /api/v0/supply-chain/container-images/identities":                          {},
	"GET /api/v0/supply-chain/container-images/identities/count":                    {},
	"GET /api/v0/supply-chain/container-images/identities/inventory":                {},
	"GET /api/v0/supply-chain/impact/findings":                                      {},
	"GET /api/v0/supply-chain/impact/findings/count":                                {},
	"GET /api/v0/supply-chain/impact/inventory":                                     {},
	"GET /api/v0/supply-chain/sbom-attestations/attachments":                        {},
	"GET /api/v0/supply-chain/sbom-attestations/attachments/count":                  {},
	"GET /api/v0/supply-chain/sbom-attestations/attachments/inventory":              {},
	"GET /api/v0/supply-chain/security-alerts/reconciliations":                      {},
	"GET /api/v0/supply-chain/security-alerts/reconciliations/count":                {},
	"GET /api/v0/supply-chain/security-alerts/reconciliations/inventory":            {},
	"GET /api/v0/supply-chain/vulnerability-scanner/contract":                       {},
	"GET /api/v0/surface-inventory":                                                 {},
	"GET /api/v0/work-items/evidence":                                               {},
	"GET /api/v0/workloads/{workload_id}/context":                                   {},
	"GET /api/v0/workloads/{workload_id}/story":                                     {},
	"PATCH /api/v0/auth/admin/sign-in-policy":                                       {},
	"PATCH /api/v0/auth/browser-session/context":                                    {},
	"POST /api/v0/admin/dead-letters/query":                                         {},
	"POST /api/v0/ask":                                                              {},
	"POST /api/v0/auth/admin/idp-group-mappings":                                    {},
	"POST /api/v0/auth/admin/provider-configs":                                      {},
	"POST /api/v0/auth/admin/provider-configs/{provider_config_id}":                 {},
	"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/disable":         {},
	"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/enable":          {},
	"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/revert":          {},
	"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/test-connection": {},
	"POST /api/v0/auth/admin/role-assignments":                                      {},
	"POST /api/v0/auth/admin/role-assignments/revoke":                               {},
	"POST /api/v0/auth/browser-session":                                             {},
	"POST /api/v0/auth/local/api-tokens":                                            {},
	"POST /api/v0/auth/local/api-tokens/{token_id}/revoke":                          {},
	"POST /api/v0/auth/local/api-tokens/{token_id}/rotate":                          {},
	"POST /api/v0/auth/local/invitations/{invite_id}/revoke":                        {},
	"POST /api/v0/auth/local/mfa/totp/begin":                                        {},
	"POST /api/v0/auth/local/mfa/totp/confirm":                                      {},
	"POST /api/v0/code/flow/cfg-summary":                                            {},
	"POST /api/v0/code/flow/pdg-summary":                                            {},
	"POST /api/v0/code/flow/reaching-def":                                           {},
	"POST /api/v0/code/flow/taint-path":                                             {},
	"POST /api/v0/code/routes/callers":                                              {},
	"POST /api/v0/code/search":                                                      {},
	"POST /api/v0/content/entities/read":                                            {},
	"POST /api/v0/content/entities/search":                                          {},
	"POST /api/v0/content/files/lines":                                              {},
	"POST /api/v0/content/files/read":                                               {},
	"POST /api/v0/content/files/search":                                             {},
	"POST /api/v0/entities/resolve":                                                 {},
	"POST /api/v0/evidence/citations":                                               {},
	"POST /api/v0/infra/relationships":                                              {},
	"POST /api/v0/infra/resources/search":                                           {},
	"POST /api/v0/investigation-workflows/resolve":                                  {},
	"POST /api/v0/query-playbooks/resolve":                                          {},
	"POST /api/v0/search/semantic":                                                  {},
}
