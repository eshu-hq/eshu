// capabilityAccess.ts — nav visibility gating based on session permission families.
//
// Design intent: nav gating is UX-only. The server enforces authorization on
// every request. This module only hides nav items to avoid confusing users who
// lack access — it never creates a security boundary in the browser.
//
// Fail-open policy: when auth is absent, all_scopes is true, or the server
// has not enabled the permission catalog (permission_catalog_enforced=false),
// ALL nav items are shown. The server will deny unauthorized requests; we must
// not hide nav based on a state we cannot verify client-side.
//
// Real permission families from go/internal/capabilitycatalog/authz.go:
//   identity_admin, roles_grants, tokens, repository_content, service_runtime,
//   cloud_iac, secrets_iam, supply_chain, docs_semantic, ask_search,
//   operations_status, audit_export, admin_recovery
//
// NOTE (#3684): browser-session (cookie) callers do not yet persist
// permission_catalog_enforced / allowed_permission_features, so cookie sessions
// currently fail open here (all nav shown). This gate activates for the console
// once session-backed feature persistence lands in #3684. The signal is already
// honored for scoped Bearer-token callers today.
import type { BrowserSessionAuth } from "../api/client";

// ALWAYS_ALLOWED routes are shown regardless of permission families. They are
// structural (home) or universally accessible (status, dashboard).
const ALWAYS_ALLOWED_ROUTES: ReadonlySet<string> = new Set([
  "/",
  "/dashboard",
  "/status"
]);

// NAV_ROUTE_TO_FAMILY maps each nav route to the real permission family that
// gates it. Routes NOT in this map are ALWAYS_ALLOWED (fail-open). Family
// names are the canonical strings from go/internal/capabilitycatalog/authz.go.
const NAV_ROUTE_TO_FAMILY: ReadonlyMap<string, string> = new Map([
  // ask_search: natural language search over the graph
  ["/ask", "ask_search"],
  // repository_content: source code, file trees, code graph, dead code
  ["/repositories", "repository_content"],
  ["/code-graph", "repository_content"],
  ["/dead-code", "repository_content"],
  ["/nodes", "repository_content"],
  ["/relationships", "repository_content"],
  ["/explorer", "repository_content"],
  // service_runtime: catalog, service story/report, incident context
  ["/catalog", "service_runtime"],
  ["/service-story", "service_runtime"],
  ["/service-report", "service_runtime"],
  ["/impact", "service_runtime"],
  ["/exposure", "service_runtime"],
  ["/changed-since", "service_runtime"],
  ["/incidents", "service_runtime"],
  ["/topology", "service_runtime"],
  // cloud_iac: IaC resources, cloud resources, cloud drift, replatforming
  ["/iac", "cloud_iac"],
  ["/cloud", "cloud_iac"],
  ["/cloud-drift", "cloud_iac"],
  ["/replatforming", "cloud_iac"],
  // secrets_iam: secrets and IAM surface
  ["/secrets-iam", "secrets_iam"],
  // supply_chain: SBOM, vulnerabilities, images, dependencies, CI/CD
  ["/sbom", "supply_chain"],
  ["/vulnerabilities", "supply_chain"],
  ["/images", "supply_chain"],
  ["/dependencies", "supply_chain"],
  ["/ci-cd/run-correlations", "supply_chain"],
  ["/findings", "supply_chain"],
  // docs_semantic: documentation surface
  ["/surface-inventory", "docs_semantic"],
  // operations_status: observability, operations, freshness
  ["/observability", "operations_status"],
  ["/operations", "operations_status"],
  ["/freshness-causality", "operations_status"],
  // admin_recovery: capability catalog, collector readiness
  ["/capabilities", "admin_recovery"],
  ["/collector-readiness", "admin_recovery"]
]);

// buildAllowedNavSet returns the set of route prefixes the current session may
// see. Fail-open in every ambiguous case so users never lose access due to a
// missing catalog or auth field.
export function buildAllowedNavSet(
  auth: BrowserSessionAuth | null | undefined
): ReadonlySet<string> {
  const allRoutes = new Set<string>([
    ...ALWAYS_ALLOWED_ROUTES,
    ...NAV_ROUTE_TO_FAMILY.keys()
  ]);

  // No session, all_scopes, or catalog not enforced → show everything.
  if (
    auth == null ||
    auth.all_scopes ||
    !auth.permission_catalog_enforced
  ) {
    return allRoutes;
  }

  // Catalog is enforced: filter by allowed permission families.
  const allowedFamilies = new Set<string>(auth.allowed_permission_features ?? []);
  const allowed = new Set<string>(ALWAYS_ALLOWED_ROUTES);

  for (const [route, family] of NAV_ROUTE_TO_FAMILY) {
    if (allowedFamilies.has(family)) {
      allowed.add(route);
    }
    // else: family not granted → hide route (UX only; server enforces).
  }

  return allowed;
}

// canAccessNav returns true when the given nav route should be shown for the
// current session. Fail-open for routes not in the mapping.
export function canAccessNav(
  route: string,
  auth: BrowserSessionAuth | null | undefined
): boolean {
  if (ALWAYS_ALLOWED_ROUTES.has(route)) return true;
  if (!NAV_ROUTE_TO_FAMILY.has(route)) return true; // unmapped → fail-open
  return buildAllowedNavSet(auth).has(route);
}
