// mockApiPages.ts — Page-level API mock handlers for Playwright e2e tests.
//
// Called from mockApi.ts after boot-critical endpoints are handled.
// Each handler returns `true` if it matched and fulfilled the route,
// so the caller (mockApi.ts) knows not to apply the fallback.

import type { Route } from "playwright";
import { envelope } from "./mockApi.ts";

export async function handlePageRoute(
  route: Route,
  path: string,
  method: string,
  _params: URLSearchParams
): Promise<boolean> {
  // ── Page-level: cloud resources ──
  if (method === "GET" && path === "/api/v0/cloud/resources") {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      resources: [
        { cloud_resource_uid: "aws_lb.frontend", resource_type: "aws_lb", resource_name: "frontend", source_state: "active", provider: "aws", region: "us-east-1", account_id: "demo-account", managed: true, tags: {} },
        { cloud_resource_uid: "aws_s3_bucket.assets", resource_type: "aws_s3_bucket", resource_name: "checkout-assets", source_state: "active", provider: "aws", region: "us-east-1", account_id: "demo-account", managed: true, tags: {} }
      ], count: 2, limit: 50, truncated: false
    }, "cloud.resources")) });
    return true;
  }

  // ── Page-level: cloud inventory ──
  if (method === "GET" && path === "/api/v0/cloud/inventory") {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      resources: [{ cloud_resource_uid: "aws_lb.frontend", resource_type: "aws_lb", evidence: { applied: true, declared: true, observed: true }, source_state: "exact" }],
      count: 1, limit: 50, truncated: false
    }, "cloud.inventory")) });
    return true;
  }

  // ── Page-level: entity resolution / service context ──
  if (method === "POST" && path.includes("/impact/entity-map")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      from: "checkout-service",
      resolution: { candidates: [{ id: "workload:checkout-service", labels: ["Workload"], name: "checkout-service" }] },
      evidence: { relationships: [
        { direction: "outgoing", entity_id: "workload:payments-api", entity_labels: ["Workload"], entity_name: "payments-api", relationship_type: "DEPENDS_ON" },
        { direction: "outgoing", entity_id: "service:ledger-service", entity_labels: ["Service"], entity_name: "ledger-service", relationship_type: "DEPENDS_ON" }
      ]}
    }, "impact.entity_map")) });
    return true;
  }

  // ── Page-level: code relationships ──
  if (method === "POST" && path.includes("/code/relationships")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      from: "repository:checkout-service",
      relationships: [
        { type: "imports", count: 120, entities: [{ name: "express", kind: "package" }] },
        { type: "calls", count: 87, entities: [{ name: "processOrder", kind: "function" }] }
      ]
    }, "code.relationships")) });
    return true;
  }

  // ── Page-level: blast radius ──
  if (method === "POST" && path.includes("/impact/blast-radius")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      blast_radius: { services: ["checkout-service", "payments-api"], workloads: ["Deployment/checkout"], environments: ["prod-us-east-1"] }
    }, "impact.blast_radius")) });
    return true;
  }

  // ── Page-level: relationships catalog ──
  if (method === "POST" && path.includes("/relationships/")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      relationships: [
        { verb: "imports", layer: "code", count: 1840, detail: "Module import edges" },
        { verb: "deploys_from", layer: "deploy", count: 28, detail: "Workload built from a repo" }
      ]
    }, "relationships.list")) });
    return true;
  }

  // ── Page-level: vulnerabilities detail ──
  if (method === "GET" && path.includes("/supply-chain/vulnerabilities/")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      id: "CVE-2024-0001", package: "sample-lib", severity: "high", cvss: 8.1,
      summary: "Remote code execution in sample-lib", affected_services: ["checkout-service"], fixed_version: "2.0.1", kev: false
    }, "vulnerabilities.detail")) });
    return true;
  }

  // ── Page-level: freshness causality ──
  if (method === "GET" && path === "/api/v0/status/freshness-causality") {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ events: [] }, "status.freshness")) });
    return true;
  }

  // ── Page-level: changed since ──
  if (method === "GET" && path.includes("/freshness/changed-since")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      services: [{ name: "checkout-service", changed: true, last_change: new Date().toISOString() }]
    }, "freshness.changed_since")) });
    return true;
  }

  // ── Page-level: CI/CD run correlations ──
  if (method === "GET" && path.includes("/ci-cd/run-correlations")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      count: 142, items: [{ pipeline: "checkout-ci", run_id: "run-123", status: "success", service: "checkout-service", repo: "sample/checkout-service", started_at: new Date().toISOString() }]
    }, "cicd.correlations")) });
    return true;
  }

  // ── Page-level: cloud drift ──
  if (method === "POST" && path.includes("/runtime-drift/findings")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      findings: [{ resource_id: "aws_iam_role.checkout_task", resource_type: "aws_iam_role", drift_type: "unmanaged", severity: "medium" }]
    }, "cloud.drift_findings")) });
    return true;
  }

  // ── Page-level: secrets IAM ──
  if (method === "GET" && path.includes("/secrets-iam/")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      posture_summary: { total: 10, high: 1, medium: 3, low: 6 }, trust_chains: [], observations: []
    }, "secrets_iam.summary")) });
    return true;
  }

  // ── Page-level: topology ──
  if (method === "GET" && path.includes("/services/") && path.includes("/context")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      name: "checkout-service", repo_name: "checkout-service", environments: ["prod-us-east-1"],
      entities: [{ id: "service:checkout-service", kind: "service", name: "checkout-service" }, { id: "deployment:checkout", kind: "deployment", name: "checkout" }]
    }, "services.context")) });
    return true;
  }

  // ── Page-level: capabilities ──
  if (method === "GET" && path === "/api/v0/capabilities") {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope([
      { capability: "graph_read", description: "Read graph data", satisfied: true },
      { capability: "security_read", description: "Read security data", satisfied: true }
    ], "capabilities.list")) });
    return true;
  }

  // ── Page-level: surface inventory ──
  if (method === "GET" && path === "/api/v0/surface-inventory") {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      surfaces: [{ name: "checkout-service", type: "service", repo: "sample/checkout-service", language: "TypeScript" }]
    }, "surface_inventory.list")) });
    return true;
  }

  // ── Page-level: observability ──
  if (method === "GET" && path.includes("/observability/")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ correlations: [] }, "observability.coverage")) });
    return true;
  }

  // ── Page-level: replatforming ──
  if (method === "POST" && path.includes("/replatforming/")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ rollups: [] }, "replatforming.rollups")) });
    return true;
  }

  // ── Page-level: incident context ──
  if (method === "GET" && path.includes("/incidents/")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      incident_id: "INC-12345", title: "Latency spike in checkout-service", status: "investigating", services: ["checkout-service"], timeline: []
    }, "incidents.context")) });
    return true;
  }

  // ── Page-level: admin endpoints ──
  if (path.includes("/auth/admin/") || path.includes("/auth/profile") || path.includes("/auth/sessions") || path.includes("/auth/local/")) {
    const data = path.includes("audit") ? { events: [], summary: { total: 0 } } : [];
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope(data, "admin.read")) });
    return true;
  }

  // ── Page-level: exposure path ──
  if (method === "POST" && path.includes("/impact/trace-exposure-path")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      target: "checkout-service", paths: [{ path: ["internet", "aws_lb.frontend", "checkout-service"], severity: "high" }]
    }, "impact.exposure_path")) });
    return true;
  }

  // ── Page-level: repo source / stats / story ──
  if (path.includes("/repositories/") && (path.endsWith("/stats") || path.endsWith("/story") || path.endsWith("/context") || path.endsWith("/source"))) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      coverage: { source_backend: "e2e_mock" }, entity_count: 42, entity_types: ["service", "workload"], story: { entries: [] }, files: [{ path: "src/index.ts", language: "TypeScript", lines: 42 }]
    }, "repositories.detail")) });
    return true;
  }

  // ── Page-level: dependency chains ──
  if (path.includes("/package-registry/dependency-chains")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope([], "dependencies.chains")) });
    return true;
  }

  // ── Page-level: replatforming plans ──
  if (method === "POST" && path.includes("/replatforming/plans")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope([], "replatforming.plans")) });
    return true;
  }

  // ── Page-level: work items evidence ──
  if (method === "POST" && path.includes("/evidence/work-items")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ items: [] }, "evidence.work_items")) });
    return true;
  }

  // ── Page-level: deployable unit packet ──
  if (method === "POST" && path.includes("/investigations/")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      entity_id: "checkout-service", summary: "Checkout service investigation", sections: []
    }, "investigations.packet")) });
    return true;
  }

  // ── Page-level: changed-since generations ──
  if (path.includes("/freshness/generations")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope([], "freshness.generations")) });
    return true;
  }

  // ── Page-level: ask endpoint ──
  if (path.includes("/ask") || path.includes("/answer-narration")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({
      answer: "This is a mock answer from the e2e test harness.", sources: []
    }, "ask.answer")) });
    return true;
  }

  // ── Page-level: impact change surface ──
  if (method === "POST" && path.includes("/impact/change-surface")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ surfaces: [] }, "impact.change_surface")) });
    return true;
  }

  // ── Page-level: impact deployment chain ──
  if (method === "POST" && path.includes("/impact/trace-deployment-chain")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ chain: [] }, "impact.deployment_chain")) });
    return true;
  }

  // ── Page-level: iac unmanaged resources ──
  if (method === "POST" && path.includes("/iac/unmanaged-resources")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ resources: [] }, "iac.unmanaged")) });
    return true;
  }

  // ── Page-level: iac terraform import candidates ──
  if (method === "POST" && path.includes("/iac/terraform-import-plan/candidates")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ candidates: [] }, "iac.terraform_import")) });
    return true;
  }

  // ── Page-level: iac management explain ──
  if (method === "POST" && path.includes("/iac/management-status/explain")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ explanation: "Managed by Terraform" }, "iac.management_explain")) });
    return true;
  }

  // ── Page-level: code imports investigate ──
  if (method === "POST" && path.includes("/code/imports/investigate")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ imports: [] }, "code.imports")) });
    return true;
  }

  // ── Page-level: code topics investigate ──
  if (method === "POST" && path.includes("/code/topics/investigate")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ topics: [] }, "code.topics")) });
    return true;
  }

  // ── Page-level: entities resolve ──
  if (method === "GET" && path === "/api/v0/entities/resolve") {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ entities: [] }, "entities.resolve")) });
    return true;
  }

  // ── Page-level: SBOM inventory/attachments ──
  if (path.includes("/supply-chain/sbom-attestations/attachments")) {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ inventory: [], attachments: [] }, "sbom.attachments")) });
    return true;
  }

  return false;
}
