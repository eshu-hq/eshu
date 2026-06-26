// mockApi.ts — Playwright route-based mock for the Eshu console API.
//
// Installed via page.route('**/eshu-api/**', ...) before the app boots.
// Returns realistic envelope-wrapped JSON so the console reaches "connected"
// state and every page shows live data (no demo fallback, no "unavailable").
// Data shapes mirror the demo fixtures in ../src/api/demoFixtures.ts.

import type { Page } from "playwright";

const truth = {
  basis: "e2e_mock",
  capability: "e2e_mock",
  freshness: { state: "fresh" as const },
  level: "exact" as const,
  profile: "e2e"
};

function envelope<T>(data: T, capability: string, error: unknown = null) {
  return { data, error, truth: { ...truth, capability } };
}

function tsParams(metric: string): number[] {
  const n = 48;
  return Array.from({ length: n }, (_, i) => 100 + Math.round(Math.sin(i / 5) * 30 + (metric.length * 7)));
}

// ──── Mock request router ────

export async function installMockApi(page: Page): Promise<void> {
  await page.route("**/eshu-api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();
    const params = url.searchParams;

    // ── Boot: session ──
    if (method === "GET" && path === "/api/v0/auth/browser-session") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          auth: {
            mode: "browser_session",
            all_scopes: true,
            permission_catalog_enforced: true,
            allowed_permission_features: [
              "identity_admin", "runtime_operations", "security_read",
              "cloud_read", "supply_chain_read", "graph_read", "ci_cd_read"
            ]
          },
          csrf_token: "mock-csrf-token",
          idle_expires_at: new Date(Date.now() + 86400000).toISOString(),
          absolute_expires_at: new Date(Date.now() + 86400000 * 7).toISOString()
        })
      });
    }

    // ── Boot: repositories ──
    if (method === "GET" && path === "/api/v0/repositories") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          repositories: [
            { id: "repository:checkout-service", name: "checkout-service", repo_slug: "sample/checkout-service", remote_url: "https://git.example.test/sample/checkout-service", is_dependency: false, group_key: "sample", group_source: "e2e_mock", group_truth: "exact", group_kind: "product", group_reason: "E2E mock fixture" },
            { id: "repository:payments-api", name: "payments-api", repo_slug: "sample/payments-api", remote_url: "https://git.example.test/sample/payments-api", is_dependency: false, group_key: "sample", group_source: "e2e_mock", group_truth: "exact", group_kind: "product", group_reason: "E2E mock fixture" },
            { id: "repository:ledger-service", name: "ledger-service", repo_slug: "sample/ledger-service", remote_url: "https://git.example.test/sample/ledger-service", is_dependency: false, group_key: "sample", group_source: "e2e_mock", group_truth: "exact", group_kind: "product", group_reason: "E2E mock fixture" },
            { id: "repository:lib-common", name: "lib-common", repo_slug: "sample/lib-common", remote_url: "https://git.example.test/sample/lib-common", is_dependency: false, group_key: "sample", group_source: "e2e_mock", group_truth: "exact", group_kind: "product", group_reason: "E2E mock fixture" },
            { id: "repository:infra-config", name: "infra-config", repo_slug: "sample/infra-config", remote_url: "https://git.example.test/sample/infra-config", is_dependency: false, group_key: "sample", group_source: "e2e_mock", group_truth: "exact", group_kind: "product", group_reason: "E2E mock fixture" },
            { id: "repository:frontend-app", name: "frontend-app", repo_slug: "sample/frontend-app", remote_url: "https://git.example.test/sample/frontend-app", is_dependency: false, group_key: "sample", group_source: "e2e_mock", group_truth: "exact", group_kind: "product", group_reason: "E2E mock fixture" }
          ]
        }, "repositories.list"))
      });
    }

    // ── Boot: catalog ──
    if (method === "GET" && path === "/api/v0/catalog") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          services: [
            { id: "checkout-service", name: "checkout-service", kind: "service", repo_name: "checkout-service", repo_id: "repository:checkout-service", environments: ["prod-us-east-1"], materialization_status: "materialized", tier: "tier-1", category: "service", domain: "payments", language: "TypeScript" },
            { id: "payments-api", name: "payments-api", kind: "service", repo_name: "payments-api", repo_id: "repository:payments-api", environments: ["prod-us-east-1"], materialization_status: "materialized", tier: "tier-1", category: "service", domain: "payments", language: "TypeScript" },
            { id: "ledger-service", name: "ledger-service", kind: "service", repo_name: "ledger-service", repo_id: "repository:ledger-service", environments: ["prod-us-east-1"], materialization_status: "materialized", tier: "tier-2", category: "service", domain: "finance", language: "Go" },
            { id: "lib-common", name: "lib-common", kind: "library", repo_name: "lib-common", repo_id: "repository:lib-common", environments: [], materialization_status: "materialized", tier: "library", category: "library", domain: "core-engineering", language: "TypeScript" }
          ],
          workloads: [],
          repositories: [
            { id: "repository:checkout-service", name: "checkout-service" },
            { id: "repository:payments-api", name: "payments-api" }
          ]
        }, "catalog.list"))
      });
    }

    // ── Boot: ecosystem overview ──
    if (method === "GET" && path === "/api/v0/ecosystem/overview") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          repo_count: 6, workload_count: 9, platform_count: 2, instance_count: 14
        }, "ecosystem.overview"))
      });
    }

    // ── Boot: index status ──
    if (method === "GET" && path === "/api/v0/index-status") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          status: "complete",
          repository_count: 6,
          queue: { outstanding: 12, pending: 0, in_flight: 2, dead_letter: 0, succeeded: 41280 },
          coordinator: { collector_instances: [] }
        })
      });
    }

    // ── Boot: ingesters ──
    if (method === "GET" && path === "/api/v0/status/ingesters") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope([
          { id: "git-primary", kind: "git", state: "healthy", facts: 41280, freshness: "fresh" },
          { id: "k8s-observer", kind: "kubernetes", state: "healthy", facts: 12040, freshness: "fresh" },
          { id: "vuln-intel", kind: "vulnerability_intelligence", state: "healthy", facts: 8810, freshness: "fresh" }
        ], "status.ingesters"))
      });
    }

    // ── Boot: language inventory ──
    if (method === "GET" && path === "/api/v0/repositories/language-inventory") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope([
          { language: "TypeScript", count: 3 },
          { language: "Go", count: 2 },
          { language: "Python", count: 1 }
        ], "repositories.language_inventory"))
      });
    }

    // ── Boot: SBOM count ──
    if (method === "GET" && path.includes("/supply-chain/sbom-attestations/attachments/count")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({ sbom_count: 2, attestation_count: 1 }, "sbom.count"))
      });
    }

    // ── Boot: dependencies ──
    if (method === "GET" && path === "/api/v0/dependencies") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope([
          { name: "express", version: "4.18.2", ecosystem: "npm", direct: true, repo_count: 3 },
          { name: "lodash", version: "4.17.21", ecosystem: "npm", direct: true, repo_count: 2 },
          { name: "axios", version: "1.6.0", ecosystem: "npm", direct: false, repo_count: 1 }
        ], "dependencies.list"))
      });
    }

    // ── Boot: images ──
    if (method === "GET" && path === "/api/v0/images") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          images: [
            { digest: "sha256:abc123", registry: "docker.io", repo: "sample/checkout-service", tag: "1.4.2", services: ["checkout-service"], vulns: { critical: 0, high: 1, medium: 3, low: 0 } }
          ]
        }, "images.list"))
      });
    }

    // ── Boot: IaC resources ──
    if (method === "GET" && path === "/api/v0/iac/resources") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope([
          { resource_type: "aws_lb", resource_id: "frontend-lb", repo_slug: "sample/infra-config", management: "managed" }
        ], "iac.resources"))
      });
    }

    // ── Boot: advisories ──
    if (method === "GET" && path === "/api/v0/supply-chain/advisories") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope([
          { id: "GHSA-xxxx-xxxx-xxxx", package: "express", severity: "high", cvss: 7.5, summary: "Prototype pollution" }
        ], "advisories.list"))
      });
    }

    // ── Boot: collector readiness ──
    if (method === "GET" && path === "/api/v0/status/collector-readiness") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope([
          { kind: "git", instance_id: "git-primary", mode: "primary", enabled: true, ready: true, last_observed_at: new Date().toISOString() },
          { kind: "kubernetes", instance_id: "k8s-observer", mode: "primary", enabled: true, ready: true, last_observed_at: new Date().toISOString() }
        ], "status.collector_readiness"))
      });
    }

    // ── Boot: timeseries metrics ──
    if (method === "GET" && path === "/api/v0/metrics/timeseries") {
      const metric = params.get("metric") ?? "unknown";
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          metric,
          values: tsParams(metric).map((v, i) => ({ ts: new Date(Date.now() - (47 - i) * 1800000).toISOString(), value: v }))
        }, "metrics.timeseries"))
      });
    }

    // ── Boot: dead code ──
    if (method === "POST" && path.includes("/code/dead-code")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope([
          { symbol: "legacyDiscount", file: "src/discounts.ts", kind: "function", repo: "sample/checkout-service", lines: 12 }
        ], "code.dead_code"))
      });
    }

    // ── Boot: vulnerability findings ──
    if (method === "GET" && path.includes("/supply-chain/impact/findings")) {
      const items = params.get("impact_status") === "affected_derived"
        ? [{ id: "GHSA-yyyy", package: "axios", severity: "medium", cvss: 5.3, kev: false, fixed_version: "1.7.0", services: ["checkout-service"] }]
        : [{ id: "CVE-2024-0001", package: "sample-lib", severity: "high", cvss: 8.1, kev: false, fixed_version: "2.0.1", services: ["checkout-service"] }];
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope(items, "supply_chain.findings"))
      });
    }

    // ── Boot: ArgoCD apps ──
    if (method === "POST" && path.includes("/infra/resources/search")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope([
          { id: "app:checkout-prod", name: "checkout-prod", kind: "argocd_application", repo_slug: "sample/infra-config", environment: "prod-us-east-1" }
        ], "infra.resources"))
      });
    }

    // ── Page-level: cloud resources ──
    if (method === "GET" && path === "/api/v0/cloud/resources") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          resources: [
            { cloud_resource_uid: "aws_lb.frontend", resource_type: "aws_lb", resource_name: "frontend", source_state: "active", provider: "aws", region: "us-east-1", account_id: "demo-account", managed: true, tags: {} },
            { cloud_resource_uid: "aws_s3_bucket.assets", resource_type: "aws_s3_bucket", resource_name: "checkout-assets", source_state: "active", provider: "aws", region: "us-east-1", account_id: "demo-account", managed: true, tags: {} }
          ],
          count: 2,
          limit: 50,
          truncated: false
        }, "cloud.resources"))
      });
    }

    // ── Page-level: cloud inventory ──
    if (method === "GET" && path === "/api/v0/cloud/inventory") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          resources: [
            { cloud_resource_uid: "aws_lb.frontend", resource_type: "aws_lb", evidence: { applied: true, declared: true, observed: true }, source_state: "exact" }
          ],
          count: 1, limit: 50, truncated: false
        }, "cloud.inventory"))
      });
    }

    // ── Page-level: entity resolution / service context ──
    if (method === "POST" && path.includes("/impact/entity-map")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          from: "checkout-service",
          resolution: {
            candidates: [{ id: "workload:checkout-service", labels: ["Workload"], name: "checkout-service" }]
          },
          evidence: {
            relationships: [
              { direction: "outgoing", entity_id: "workload:payments-api", entity_labels: ["Workload"], entity_name: "payments-api", relationship_type: "DEPENDS_ON" },
              { direction: "outgoing", entity_id: "service:ledger-service", entity_labels: ["Service"], entity_name: "ledger-service", relationship_type: "DEPENDS_ON" }
            ]
          }
        }, "impact.entity_map"))
      });
    }

    // ── Page-level: code relationships ──
    if (method === "POST" && path.includes("/code/relationships")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          from: "repository:checkout-service",
          relationships: [
            { type: "imports", count: 120, entities: [{ name: "express", kind: "package" }] },
            { type: "calls", count: 87, entities: [{ name: "processOrder", kind: "function" }] }
          ]
        }, "code.relationships"))
      });
    }

    // ── Page-level: catalog ──
    if (method === "POST" && path.includes("/impact/blast-radius")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          blast_radius: {
            services: ["checkout-service", "payments-api"],
            workloads: ["Deployment/checkout"],
            environments: ["prod-us-east-1"]
          }
        }, "impact.blast_radius"))
      });
    }

    // ── Page-level: relationships catalog ──
    if (method === "POST" && path.includes("/relationships/")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          relationships: [
            { verb: "imports", layer: "code", count: 1840, detail: "Module import edges" },
            { verb: "deploys_from", layer: "deploy", count: 28, detail: "Workload built from a repo" }
          ]
        }, "relationships.list"))
      });
    }

    // ── Page-level: vulnerabilities detail ──
    if (method === "GET" && path.includes("/supply-chain/vulnerabilities/")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          id: "CVE-2024-0001",
          package: "sample-lib",
          severity: "high",
          cvss: 8.1,
          summary: "Remote code execution in sample-lib",
          affected_services: ["checkout-service"],
          fixed_version: "2.0.1",
          kev: false
        }, "vulnerabilities.detail"))
      });
    }

    // ── Page-level: freshness causality ──
    if (method === "GET" && path === "/api/v0/status/freshness-causality") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          events: []
        }, "status.freshness"))
      });
    }

    // ── Page-level: changed since ──
    if (method === "GET" && path.includes("/freshness/changed-since")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          services: [
            { name: "checkout-service", changed: true, last_change: new Date().toISOString() }
          ]
        }, "freshness.changed_since"))
      });
    }

    // ── Page-level: CI/CD run correlations ──
    if (method === "GET" && path.includes("/ci-cd/run-correlations")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          count: 142,
          items: [
            { pipeline: "checkout-ci", run_id: "run-123", status: "success", service: "checkout-service", repo: "sample/checkout-service", started_at: new Date().toISOString() }
          ]
        }, "cicd.correlations"))
      });
    }

    // ── Page-level: cloud drift ──
    if (method === "POST" && path.includes("/runtime-drift/findings")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          findings: [
            { resource_id: "aws_iam_role.checkout_task", resource_type: "aws_iam_role", drift_type: "unmanaged", severity: "medium" }
          ]
        }, "cloud.drift_findings"))
      });
    }

    // ── Page-level: secrets IAM ──
    if (method === "GET" && path.includes("/secrets-iam/")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          posture_summary: { total: 10, high: 1, medium: 3, low: 6 },
          trust_chains: [],
          observations: []
        }, "secrets_iam.summary"))
      });
    }

    // ── Page-level: topology ──
    if (method === "GET" && path.includes("/services/") && path.includes("/context")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          name: "checkout-service",
          repo_name: "checkout-service",
          environments: ["prod-us-east-1"],
          entities: [
            { id: "service:checkout-service", kind: "service", name: "checkout-service" },
            { id: "deployment:checkout", kind: "deployment", name: "checkout" }
          ]
        }, "services.context"))
      });
    }

    // ── Page-level: capabilities ──
    if (method === "GET" && path === "/api/v0/capabilities") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope([
          { capability: "graph_read", description: "Read graph data", satisfied: true },
          { capability: "security_read", description: "Read security data", satisfied: true }
        ], "capabilities.list"))
      });
    }

    // ── Page-level: surface inventory ──
    if (method === "GET" && path === "/api/v0/surface-inventory") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          surfaces: [
            { name: "checkout-service", type: "service", repo: "sample/checkout-service", language: "TypeScript" }
          ]
        }, "surface_inventory.list"))
      });
    }

    // ── Page-level: observability ──
    if (method === "GET" && path.includes("/observability/")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          correlations: []
        }, "observability.coverage"))
      });
    }

    // ── Page-level: replatforming ──
    if (method === "POST" && path.includes("/replatforming/")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          rollups: []
        }, "replatforming.rollups"))
      });
    }

    // ── Page-level: incident context ──
    if (method === "GET" && path.includes("/incidents/")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          incident_id: "INC-12345",
          title: "Latency spike in checkout-service",
          status: "investigating",
          services: ["checkout-service"],
          timeline: []
        }, "incidents.context"))
      });
    }

    // ── Page-level: admin endpoints ──
    if (path.includes("/auth/admin/") || path.includes("/auth/profile") || path.includes("/auth/sessions") || path.includes("/auth/local/")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope(path.includes("audit") ? { events: [], summary: { total: 0 } } : [], "admin.read"))
      });
    }

    // ── Page-level: exposure path ──
    if (method === "POST" && path.includes("/impact/trace-exposure-path")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          target: "checkout-service",
          paths: [
            { path: ["internet", "aws_lb.frontend", "checkout-service"], severity: "high" }
          ]
        }, "impact.exposure_path"))
      });
    }

    // ── Page-level: repo source / stats / story ──
    if (path.includes("/repositories/") && (path.endsWith("/stats") || path.endsWith("/story") || path.endsWith("/context") || path.endsWith("/source"))) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          coverage: { source_backend: "e2e_mock" },
          entity_count: 42,
          entity_types: ["service", "workload"],
          story: { entries: [] },
          files: [{ path: "src/index.ts", language: "TypeScript", lines: 42 }]
        }, "repositories.detail"))
      });
    }

    // ── Page-level: dependency chains ──
    if (path.includes("/package-registry/dependency-chains")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope([], "dependencies.chains"))
      });
    }

    // ── Page-level: replatforming plans ──
    if (method === "POST" && path.includes("/replatforming/plans")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope([], "replatforming.plans"))
      });
    }

    // ── Page-level: work items evidence ──
    if (method === "POST" && path.includes("/evidence/work-items")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({ items: [] }, "evidence.work_items"))
      });
    }

    // ── Page-level: deployable unit packet ──
    if (method === "POST" && path.includes("/investigations/")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          entity_id: "checkout-service",
          summary: "Checkout service investigation",
          sections: []
        }, "investigations.packet"))
      });
    }

    // ── Page-level: changed-since generations ──
    if (path.includes("/freshness/generations")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope([], "freshness.generations"))
      });
    }

    // ── Page-level: ask endpoint ──
    if (path.includes("/ask") || path.includes("/answer-narration")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          answer: "This is a mock answer from the e2e test harness.",
          sources: []
        }, "ask.answer"))
      });
    }

    // ── Page-level: impact change surface ──
    if (method === "POST" && path.includes("/impact/change-surface")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          surfaces: []
        }, "impact.change_surface"))
      });
    }

    // ── Page-level: impact deployment chain ──
    if (method === "POST" && path.includes("/impact/trace-deployment-chain")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({
          chain: []
        }, "impact.deployment_chain"))
      });
    }

    // ── Page-level: iac unmanaged resources ──
    if (method === "POST" && path.includes("/iac/unmanaged-resources")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({ resources: [] }, "iac.unmanaged"))
      });
    }

    // ── Page-level: iac terraform import candidates ──
    if (method === "POST" && path.includes("/iac/terraform-import-plan/candidates")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({ candidates: [] }, "iac.terraform_import"))
      });
    }

    // ── Page-level: iac management explain ──
    if (method === "POST" && path.includes("/iac/management-status/explain")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({ explanation: "Managed by Terraform" }, "iac.management_explain"))
      });
    }

    // ── Page-level: code imports investigate ──
    if (method === "POST" && path.includes("/code/imports/investigate")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({ imports: [] }, "code.imports"))
      });
    }

    // ── Page-level: code topics investigate ──
    if (method === "POST" && path.includes("/code/topics/investigate")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({ topics: [] }, "code.topics"))
      });
    }

    // ── Page-level: entities resolve ──
    if (method === "GET" && path === "/api/v0/entities/resolve") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({ entities: [] }, "entities.resolve"))
      });
    }

    // ── Page-level: SBOM inventory/attachments ──
    if (path.includes("/supply-chain/sbom-attestations/attachments")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(envelope({ inventory: [], attachments: [] }, "sbom.attachments"))
      });
    }

    // ── Fallback for any unmatched /eshu-api call ──
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope(null, "e2e.fallback"))
    });
  });
}
