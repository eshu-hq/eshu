// mockApiPages.ts — Page-level API mock handlers for Playwright e2e tests.
//
// Called from mockApi.ts after boot-critical endpoints are handled.
// Each handler returns `true` if it matched and fulfilled the route,
// so the caller (mockApi.ts) knows not to apply the fallback.
// Overflow handlers (impact, IAC, code, repo, freshness, ask, SBOM) live in
// mockApiPagesExt.ts to stay within the 500-line file limit.

import type { Route } from "playwright";
import { envelope } from "./mockApi.ts";
import { handlePageRouteExt } from "./mockApiPagesExt.ts";

export async function handlePageRoute(
  route: Route,
  path: string,
  method: string,
  _params: URLSearchParams,
): Promise<boolean> {
  // ── Page-level: cloud resources ──
  if (method === "GET" && path === "/api/v0/cloud/resources") {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            resources: [
              {
                cloud_resource_uid: "aws_lb.frontend",
                resource_type: "aws_lb",
                resource_name: "frontend",
                source_state: "active",
                provider: "aws",
                region: "us-east-1",
                account_id: "demo-account",
                managed: true,
                tags: {},
              },
              {
                cloud_resource_uid: "aws_s3_bucket.assets",
                resource_type: "aws_s3_bucket",
                resource_name: "checkout-assets",
                source_state: "active",
                provider: "aws",
                region: "us-east-1",
                account_id: "demo-account",
                managed: true,
                tags: {},
              },
            ],
            count: 2,
            limit: 50,
            truncated: false,
          },
          "cloud.resources",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: cloud inventory ──
  if (method === "GET" && path === "/api/v0/cloud/inventory") {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            resources: [
              {
                cloud_resource_uid: "aws_lb.frontend",
                resource_type: "aws_lb",
                evidence: { applied: true, declared: true, observed: true },
                source_state: "exact",
              },
            ],
            count: 1,
            limit: 50,
            truncated: false,
          },
          "cloud.inventory",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: entity resolution / service context ──
  if (method === "POST" && path.includes("/impact/entity-map")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            from: "checkout-service",
            resolution: {
              candidates: [
                { id: "workload:checkout-service", labels: ["Workload"], name: "checkout-service" },
              ],
            },
            evidence: {
              relationships: [
                {
                  direction: "outgoing",
                  entity_id: "workload:payments-api",
                  entity_labels: ["Workload"],
                  entity_name: "payments-api",
                  relationship_type: "DEPENDS_ON",
                },
                {
                  direction: "outgoing",
                  entity_id: "service:ledger-service",
                  entity_labels: ["Service"],
                  entity_name: "ledger-service",
                  relationship_type: "DEPENDS_ON",
                },
              ],
            },
          },
          "impact.entity_map",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: code relationships ──
  if (method === "POST" && path.includes("/code/relationships")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            from: "repository:checkout-service",
            relationships: [
              { type: "imports", count: 120, entities: [{ name: "express", kind: "package" }] },
              { type: "calls", count: 87, entities: [{ name: "processOrder", kind: "function" }] },
            ],
          },
          "code.relationships",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: blast radius ──
  if (method === "POST" && path.includes("/impact/blast-radius")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            blast_radius: {
              services: ["checkout-service", "payments-api"],
              workloads: ["Deployment/checkout"],
              environments: ["prod-us-east-1"],
            },
          },
          "impact.blast_radius",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: relationships catalog ──
  if (method === "POST" && path.includes("/relationships/catalog")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            verbs: [
              {
                verb: "IMPORTS",
                layer: "code",
                count: 1840,
                evidence: "code analysis",
                detail: "File imports a module",
              },
              {
                verb: "DEPENDS_ON",
                layer: "deploy",
                count: 280,
                evidence: "infrastructure tooling",
                detail: "Service depends on another",
                source_tools: { terraform: 215, helm: 65 },
              },
              {
                verb: "DEPLOYS_FROM",
                layer: "deploy",
                count: 28,
                evidence: "infrastructure tooling",
                detail: "Workload built from a repo",
                source_tools: { helm: 28 },
              },
            ],
            verb_count: 3,
            total_edges: 2148,
            layer_count: 2,
          },
          "relationships.catalog",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: relationships edges ──
  if (method === "POST" && path.includes("/relationships/edges")) {
    // postDataJSON() is synchronous in Playwright (returns null | Serializable,
    // not a Promise), so it must be guarded with a try/catch, not `.catch()`.
    let body: Record<string, unknown> = {};
    try {
      body = (route.request().postDataJSON() ?? {}) as Record<string, unknown>;
    } catch {
      body = {};
    }
    const sourceTool = typeof body?.source_tool === "string" ? body.source_tool : "";
    const baseEdges = [
      {
        source_id: "file:server/index.ts",
        source_name: "server/index.ts",
        target_id: "pkg:express",
        target_name: "express",
        evidence: "import statement",
      },
      {
        source_id: "file:src/auth.ts",
        source_name: "src/auth.ts",
        target_id: "pkg:jsonwebtoken",
        target_name: "jsonwebtoken",
        evidence: "import statement",
        source_tool: "terraform",
      },
    ];
    const edges = sourceTool ? baseEdges.filter((e) => e.source_tool === sourceTool) : baseEdges;
    const responseData: Record<string, unknown> = {
      verb: body?.verb ?? "IMPORTS",
      layer: "code",
      evidence: "code analysis",
      detail: "File imports a module",
      edges,
      truncated: false,
      limit: 50,
    };
    if (sourceTool) responseData.source_tool = sourceTool;
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope(responseData, "relationships.edges")),
    });
    return true;
  }

  // ── Page-level: vulnerabilities detail ──
  if (method === "GET" && path.includes("/supply-chain/vulnerabilities/")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            id: "CVE-2024-0001",
            package: "sample-lib",
            severity: "high",
            cvss: 8.1,
            summary: "Remote code execution in sample-lib",
            affected_services: ["checkout-service"],
            fixed_version: "2.0.1",
            kev: false,
          },
          "vulnerabilities.detail",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: freshness causality ──
  if (method === "GET" && path === "/api/v0/status/freshness-causality") {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({ events: [] }, "status.freshness")),
    });
    return true;
  }

  // ── Page-level: changed since ──
  if (method === "GET" && path.includes("/freshness/changed-since")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            services: [
              { name: "checkout-service", changed: true, last_change: new Date().toISOString() },
            ],
          },
          "freshness.changed_since",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: CI/CD run correlations ──
  if (method === "GET" && path.includes("/ci-cd/run-correlations")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            count: 142,
            items: [
              {
                pipeline: "checkout-ci",
                run_id: "run-123",
                status: "success",
                service: "checkout-service",
                repo: "sample/checkout-service",
                started_at: new Date().toISOString(),
              },
            ],
          },
          "cicd.correlations",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: cloud drift ──
  if (method === "POST" && path.includes("/runtime-drift/findings")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            findings: [
              {
                resource_id: "aws_iam_role.checkout_task",
                resource_type: "aws_iam_role",
                drift_type: "unmanaged",
                severity: "medium",
              },
            ],
          },
          "cloud.drift_findings",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: secrets IAM ──
  if (method === "GET" && path.includes("/secrets-iam/")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            posture_summary: { total: 10, high: 1, medium: 3, low: 6 },
            trust_chains: [],
            observations: [],
          },
          "secrets_iam.summary",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: topology ──
  if (method === "GET" && path.includes("/services/") && path.includes("/context")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            name: "checkout-service",
            repo_name: "checkout-service",
            environments: ["prod-us-east-1"],
            entities: [
              { id: "service:checkout-service", kind: "service", name: "checkout-service" },
              { id: "deployment:checkout", kind: "deployment", name: "checkout" },
            ],
          },
          "services.context",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: capabilities ──
  if (method === "GET" && path === "/api/v0/capabilities") {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          [
            { capability: "graph_read", description: "Read graph data", satisfied: true },
            { capability: "security_read", description: "Read security data", satisfied: true },
          ],
          "capabilities.list",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: surface inventory ──
  if (method === "GET" && path === "/api/v0/surface-inventory") {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            surfaces: [
              {
                name: "checkout-service",
                type: "service",
                repo: "sample/checkout-service",
                language: "TypeScript",
              },
            ],
          },
          "surface_inventory.list",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: observability ──
  if (method === "GET" && path.includes("/observability/")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({ correlations: [] }, "observability.coverage")),
    });
    return true;
  }

  // ── Page-level: replatforming ──
  if (method === "POST" && path.includes("/replatforming/")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({ rollups: [] }, "replatforming.rollups")),
    });
    return true;
  }

  // Delegate overflow handlers (incidents, admin, impact paths, IAC, code,
  // repo, freshness, ask, SBOM) to the companion module.
  return handlePageRouteExt(route, path, method);
}
