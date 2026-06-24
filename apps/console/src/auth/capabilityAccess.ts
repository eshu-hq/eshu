// capabilityAccess.ts — nav visibility gating based on session capabilities.
//
// Design intent: nav gating is UX-only. The server enforces authorization on
// every request. This module only hides nav items to avoid confusing users who
// lack a scope — it never creates a security boundary in the browser.
//
// Fail-open policy: if the capability catalog is unavailable or empty, ALL nav
// items are shown. Hiding nav based on a fetch error would be incorrect: we
// cannot distinguish "no capabilities" from "catalog fetch failed." An empty
// catalog is treated as "we don't know → show everything; server will deny."
// This is documented explicitly so future maintainers do not change the
// fail-open behavior without understanding the security model.
import type { BrowserSessionAuth } from "../api/client";
import type { CapabilityRow } from "../api/capabilityCatalog";

// ALWAYS_ALLOWED routes are shown regardless of scopes. They are either
// structural (login, home) or universally available (status, dashboard).
const ALWAYS_ALLOWED_ROUTES: ReadonlySet<string> = new Set([
  "/",
  "/status",
  "/dashboard"
]);

// NAV_ROUTE_TO_CAPABILITY maps each nav route to the capability key that gates
// it. Routes NOT in this map are fail-open (shown when catalog is present but
// no mapping exists).
const NAV_ROUTE_TO_CAPABILITY: ReadonlyMap<string, string> = new Map([
  ["/ask", "platform_graph.ask"],
  ["/impact", "platform_impact.impact_review"],
  ["/exposure", "platform_impact.exposure_path"],
  ["/changed-since", "platform_graph.changed_since"],
  ["/explorer", "platform_graph.graph_explorer"],
  ["/relationships", "platform_graph.relationships"],
  ["/service-story", "platform_graph.service_story"],
  ["/service-report", "platform_graph.service_report"],
  ["/nodes", "platform_graph.nodes"],
  ["/repositories", "platform_inventory.repository_list"],
  ["/catalog", "platform_inventory.service_catalog"],
  ["/findings", "platform_inventory.findings"],
  ["/images", "supply_chain.container_image_list"],
  ["/iac", "platform_inventory.iac_resources"],
  ["/replatforming", "platform_inventory.replatforming"],
  ["/vulnerabilities", "supply_chain.vulnerability_list"],
  ["/dead-code", "code_intelligence.dead_code"],
  ["/code-graph", "code_intelligence.code_graph"],
  ["/topology", "platform_graph.topology"],
  ["/cloud", "platform_inventory.cloud_resources"],
  ["/secrets-iam", "platform_inventory.secrets_iam"],
  ["/incidents", "platform_graph.incident_context"],
  ["/ci-cd/run-correlations", "supply_chain.ci_cd_run_correlations"],
  ["/cloud-drift", "platform_inventory.cloud_drift"],
  ["/observability", "platform_graph.observability"],
  ["/sbom", "supply_chain.sbom_attestations"],
  ["/dependencies", "supply_chain.dependency_list"],
  ["/capabilities", "platform_meta.capability_catalog"],
  ["/collector-readiness", "platform_meta.collector_readiness"],
  ["/surface-inventory", "platform_meta.surface_inventory"],
  ["/operations", "platform_meta.operations"],
  ["/freshness-causality", "platform_meta.freshness_causality"]
]);

// buildAllowedNavSet returns the set of route prefixes the current session may
// see. Called once per render cycle when session or catalog changes.
export function buildAllowedNavSet(
  auth: BrowserSessionAuth,
  catalogRows: readonly CapabilityRow[]
): ReadonlySet<string> {
  // Seed with always-allowed routes.
  const allowed = new Set<string>(ALWAYS_ALLOWED_ROUTES);

  // all_scopes sessions see every nav item unconditionally.
  if (auth.all_scopes) {
    for (const route of NAV_ROUTE_TO_CAPABILITY.keys()) {
      allowed.add(route);
    }
    return allowed;
  }

  // Fail-open: if catalog is empty we cannot distinguish "no capabilities
  // configured" from "catalog fetch failed." Show everything; server enforces.
  if (catalogRows.length === 0) {
    for (const route of NAV_ROUTE_TO_CAPABILITY.keys()) {
      allowed.add(route);
    }
    return allowed;
  }

  // Build a set of capability keys the session is allowed to use.
  const allowedScopes = new Set<string>(auth.allowed_scope_ids ?? []);

  for (const [route, capKey] of NAV_ROUTE_TO_CAPABILITY) {
    const row = catalogRows.find((r) => r.capability === capKey);
    if (row === undefined) {
      // No catalog entry for this route → fail-open (show it).
      allowed.add(route);
    } else if (allowedScopes.has(capKey)) {
      allowed.add(route);
    }
    // else: catalog has the entry but session lacks the scope → hide (UX only).
  }

  return allowed;
}

// canAccessNav returns true when the given nav route should be shown for the
// current session and catalog state. Fail-open for unmapped routes.
export function canAccessNav(
  route: string,
  auth: BrowserSessionAuth,
  catalogRows: readonly CapabilityRow[]
): boolean {
  const allowed = buildAllowedNavSet(auth, catalogRows);
  if (allowed.has(route)) return true;
  // Fail-open for routes not in the mapping (no catalog entry to deny on).
  if (!NAV_ROUTE_TO_CAPABILITY.has(route)) return true;
  return false;
}
