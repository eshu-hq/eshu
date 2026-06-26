// mockApi.ts — Playwright route-based mock for Eshu console API boot endpoints.
//
// Installed via page.route('**/eshu-api/**', ...) before the app boots.
// Handles boot-critical API endpoints inline; delegates page-level endpoints
// to mockApiPages.ts. Data shapes mirror the demo fixtures.
// Kept under 500 lines by delegating page handlers to a separate module.

import type { Page } from "playwright";
import { handlePageRoute } from "./mockApiPages.ts";

export const truth = {
  basis: "e2e_mock",
  capability: "e2e_mock",
  freshness: { state: "fresh" as const },
  level: "exact" as const,
  profile: "e2e"
};

export function envelope<T>(data: T, capability: string, error: unknown = null) {
  return { data, error, truth: { ...truth, capability } };
}

function tsParams(metric: string): number[] {
  const n = 48;
  return Array.from({ length: n }, (_, i) => 100 + Math.round(Math.sin(i / 5) * 30 + (metric.length * 7)));
}

export async function installMockApi(page: Page): Promise<void> {
  await page.route("**/eshu-api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();
    const params = url.searchParams;

    // ── Boot: session ──
    if (method === "GET" && path === "/api/v0/auth/browser-session") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({
        auth: { mode: "browser_session", all_scopes: true, permission_catalog_enforced: true, allowed_permission_features: ["identity_admin", "runtime_operations", "security_read", "cloud_read", "supply_chain_read", "graph_read", "ci_cd_read"] },
        csrf_token: "mock-csrf-token",
        idle_expires_at: new Date(Date.now() + 86400000).toISOString(),
        absolute_expires_at: new Date(Date.now() + 86400000 * 7).toISOString()
      }) });
    }

    // ── Boot: repositories ──
    if (method === "GET" && path === "/api/v0/repositories") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
        repositories: [
          { id: "repository:checkout-service", name: "checkout-service", repo_slug: "sample/checkout-service", remote_url: "https://git.example.test/sample/checkout-service", is_dependency: false, group_key: "sample", group_source: "e2e_mock", group_truth: "exact", group_kind: "product", group_reason: "E2E mock fixture" },
          { id: "repository:payments-api", name: "payments-api", repo_slug: "sample/payments-api", remote_url: "https://git.example.test/sample/payments-api", is_dependency: false, group_key: "sample", group_source: "e2e_mock", group_truth: "exact", group_kind: "product", group_reason: "E2E mock fixture" },
          { id: "repository:ledger-service", name: "ledger-service", repo_slug: "sample/ledger-service", remote_url: "https://git.example.test/sample/ledger-service", is_dependency: false, group_key: "sample", group_source: "e2e_mock", group_truth: "exact", group_kind: "product", group_reason: "E2E mock fixture" },
          { id: "repository:lib-common", name: "lib-common", repo_slug: "sample/lib-common", remote_url: "https://git.example.test/sample/lib-common", is_dependency: false, group_key: "sample", group_source: "e2e_mock", group_truth: "exact", group_kind: "product", group_reason: "E2E mock fixture" },
          { id: "repository:infra-config", name: "infra-config", repo_slug: "sample/infra-config", remote_url: "https://git.example.test/sample/infra-config", is_dependency: false, group_key: "sample", group_source: "e2e_mock", group_truth: "exact", group_kind: "product", group_reason: "E2E mock fixture" },
          { id: "repository:frontend-app", name: "frontend-app", repo_slug: "sample/frontend-app", remote_url: "https://git.example.test/sample/frontend-app", is_dependency: false, group_key: "sample", group_source: "e2e_mock", group_truth: "exact", group_kind: "product", group_reason: "E2E mock fixture" }
        ]
      }, "repositories.list")) });
    }

    // ── Boot: catalog ──
    if (method === "GET" && path === "/api/v0/catalog") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
        services: [
          { id: "checkout-service", name: "checkout-service", kind: "service", repo_name: "checkout-service", repo_id: "repository:checkout-service", environments: ["prod-us-east-1"], materialization_status: "materialized", tier: "tier-1", category: "service", domain: "payments", language: "TypeScript" },
          { id: "payments-api", name: "payments-api", kind: "service", repo_name: "payments-api", repo_id: "repository:payments-api", environments: ["prod-us-east-1"], materialization_status: "materialized", tier: "tier-1", category: "service", domain: "payments", language: "TypeScript" },
          { id: "ledger-service", name: "ledger-service", kind: "service", repo_name: "ledger-service", repo_id: "repository:ledger-service", environments: ["prod-us-east-1"], materialization_status: "materialized", tier: "tier-2", category: "service", domain: "finance", language: "Go" },
          { id: "lib-common", name: "lib-common", kind: "library", repo_name: "lib-common", repo_id: "repository:lib-common", environments: [], materialization_status: "materialized", tier: "library", category: "library", domain: "core-engineering", language: "TypeScript" }
        ],
        workloads: [],
        repositories: [{ id: "repository:checkout-service", name: "checkout-service" }, { id: "repository:payments-api", name: "payments-api" }]
      }, "catalog.list")) });
    }

    // ── Boot: ecosystem overview ──
    if (method === "GET" && path === "/api/v0/ecosystem/overview") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ repo_count: 6, workload_count: 9, platform_count: 2, instance_count: 14 }, "ecosystem.overview")) });
    }

    // ── Boot: index status ──
    if (method === "GET" && path === "/api/v0/index-status") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ status: "complete", repository_count: 6, queue: { outstanding: 12, pending: 0, in_flight: 2, dead_letter: 0, succeeded: 41280 }, coordinator: { collector_instances: [] } }) });
    }

    // ── Boot: ingesters ──
    if (method === "GET" && path === "/api/v0/status/ingesters") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope([{ id: "git-primary", kind: "git", state: "healthy", facts: 41280, freshness: "fresh" }, { id: "k8s-observer", kind: "kubernetes", state: "healthy", facts: 12040, freshness: "fresh" }, { id: "vuln-intel", kind: "vulnerability_intelligence", state: "healthy", facts: 8810, freshness: "fresh" }], "status.ingesters")) });
    }

    // ── Boot: language inventory ──
    if (method === "GET" && path === "/api/v0/repositories/language-inventory") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope([{ language: "TypeScript", count: 3 }, { language: "Go", count: 2 }, { language: "Python", count: 1 }], "repositories.language_inventory")) });
    }

    // ── Boot: SBOM count ──
    if (method === "GET" && path.includes("/supply-chain/sbom-attestations/attachments/count")) {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ sbom_count: 2, attestation_count: 1 }, "sbom.count")) });
    }

    // ── Boot: dependencies ──
    if (method === "GET" && path === "/api/v0/dependencies") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope([{ name: "express", version: "4.18.2", ecosystem: "npm", direct: true, repo_count: 3 }, { name: "lodash", version: "4.17.21", ecosystem: "npm", direct: true, repo_count: 2 }, { name: "axios", version: "1.6.0", ecosystem: "npm", direct: false, repo_count: 1 }], "dependencies.list")) });
    }

    // ── Boot: images ──
    if (method === "GET" && path === "/api/v0/images") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ images: [{ digest: "sha256:abc123", registry: "docker.io", repo: "sample/checkout-service", tag: "1.4.2", services: ["checkout-service"], vulns: { critical: 0, high: 1, medium: 3, low: 0 } }] }, "images.list")) });
    }

    // ── Boot: IaC resources ──
    if (method === "GET" && path === "/api/v0/iac/resources") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope([{ resource_type: "aws_lb", resource_id: "frontend-lb", repo_slug: "sample/infra-config", management: "managed" }], "iac.resources")) });
    }

    // ── Boot: advisories ──
    if (method === "GET" && path === "/api/v0/supply-chain/advisories") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope([{ id: "GHSA-xxxx-xxxx-xxxx", package: "express", severity: "high", cvss: 7.5, summary: "Prototype pollution" }], "advisories.list")) });
    }

    // ── Boot: collector readiness ──
    if (method === "GET" && path === "/api/v0/status/collector-readiness") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope([{ kind: "git", instance_id: "git-primary", mode: "primary", enabled: true, ready: true, last_observed_at: new Date().toISOString() }, { kind: "kubernetes", instance_id: "k8s-observer", mode: "primary", enabled: true, ready: true, last_observed_at: new Date().toISOString() }], "status.collector_readiness")) });
    }

    // ── Boot: timeseries metrics ──
    if (method === "GET" && path === "/api/v0/metrics/timeseries") {
      const metric = params.get("metric") ?? "unknown";
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ metric, values: tsParams(metric).map((v, i) => ({ ts: new Date(Date.now() - (47 - i) * 1800000).toISOString(), value: v })) }, "metrics.timeseries")) });
    }

    // ── Boot: dead code ──
    if (method === "POST" && path.includes("/code/dead-code")) {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope([{ symbol: "legacyDiscount", file: "src/discounts.ts", kind: "function", repo: "sample/checkout-service", lines: 12 }], "code.dead_code")) });
    }

    // ── Boot: vulnerability findings ──
    if (method === "GET" && path.includes("/supply-chain/impact/findings")) {
      const items = params.get("impact_status") === "affected_derived" ? [{ id: "GHSA-yyyy", package: "axios", severity: "medium", cvss: 5.3, kev: false, fixed_version: "1.7.0", services: ["checkout-service"] }] : [{ id: "CVE-2024-0001", package: "sample-lib", severity: "high", cvss: 8.1, kev: false, fixed_version: "2.0.1", services: ["checkout-service"] }];
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope(items, "supply_chain.findings")) });
    }

    // ── Boot: ArgoCD apps ──
    if (method === "POST" && path.includes("/infra/resources/search")) {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope([{ id: "app:checkout-prod", name: "checkout-prod", kind: "argocd_application", repo_slug: "sample/infra-config", environment: "prod-us-east-1" }], "infra.resources")) });
    }

    // Delegate remaining (page-level) endpoints to the separate module.
    if (await handlePageRoute(route, path, method, params)) {
      return;
    }

    // ── Fallback for any unmatched /eshu-api call ──
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope(null, "e2e.fallback")) });
  });
}
