// mockApiPagesExt.ts — Overflow handlers for page-level API mocks.
//
// Called from mockApiPages.ts after the primary page handlers are checked.
// Covers: impact, IAC, code, repo, freshness, admin, ask, SBOM, and misc.

import type { Route } from "playwright";
import { envelope } from "./mockApi.ts";

export async function handlePageRouteExt(
  route: Route,
  path: string,
  method: string,
): Promise<boolean> {
  // ── Page-level: incident context ──
  if (method === "GET" && path.includes("/incidents/")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            incident_id: "INC-12345",
            title: "Latency spike in checkout-service",
            status: "investigating",
            services: ["checkout-service"],
            timeline: [],
          },
          "incidents.context",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: admin endpoints ──
  if (
    path.includes("/auth/admin/") ||
    path.includes("/auth/profile") ||
    path.includes("/auth/sessions") ||
    path.includes("/auth/local/")
  ) {
    const data = path.includes("audit") ? { events: [], summary: { total: 0 } } : [];
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope(data, "admin.read")),
    });
    return true;
  }

  // ── Page-level: exposure path ──
  if (method === "POST" && path.includes("/impact/trace-exposure-path")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            target: "checkout-service",
            paths: [
              { path: ["internet", "aws_lb.frontend", "checkout-service"], severity: "high" },
            ],
          },
          "impact.exposure_path",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: repo source / stats / story ──
  if (
    path.includes("/repositories/") &&
    (path.endsWith("/stats") ||
      path.endsWith("/story") ||
      path.endsWith("/context") ||
      path.endsWith("/source"))
  ) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            coverage: { source_backend: "e2e_mock" },
            entity_count: 42,
            entity_types: ["service", "workload"],
            story: { entries: [] },
            files: [{ path: "src/index.ts", language: "TypeScript", lines: 42 }],
          },
          "repositories.detail",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: dependency chains ──
  if (path.includes("/package-registry/dependency-chains")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope([], "dependencies.chains")),
    });
    return true;
  }

  // ── Page-level: replatforming plans ──
  if (method === "POST" && path.includes("/replatforming/plans")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope([], "replatforming.plans")),
    });
    return true;
  }

  // ── Page-level: work items evidence ──
  if (method === "POST" && path.includes("/evidence/work-items")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({ items: [] }, "evidence.work_items")),
    });
    return true;
  }

  // ── Page-level: deployable unit packet ──
  if (method === "POST" && path.includes("/investigations/")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            entity_id: "checkout-service",
            summary: "Checkout service investigation",
            sections: [],
          },
          "investigations.packet",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: changed-since generations ──
  if (path.includes("/freshness/generations")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope([], "freshness.generations")),
    });
    return true;
  }

  // ── Page-level: ask endpoint ──
  if (path.includes("/ask") || path.includes("/answer-narration")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            answer: "This is a mock answer from the e2e test harness.",
            sources: [],
          },
          "ask.answer",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: impact change surface ──
  if (method === "POST" && path.includes("/impact/change-surface")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({ surfaces: [] }, "impact.change_surface")),
    });
    return true;
  }

  // ── Page-level: impact deployment chain ──
  if (method === "POST" && path.includes("/impact/trace-deployment-chain")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({ chain: [] }, "impact.deployment_chain")),
    });
    return true;
  }

  // ── Page-level: iac unmanaged resources ──
  if (method === "POST" && path.includes("/iac/unmanaged-resources")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({ resources: [] }, "iac.unmanaged")),
    });
    return true;
  }

  // ── Page-level: iac terraform import candidates ──
  if (method === "POST" && path.includes("/iac/terraform-import-plan/candidates")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({ candidates: [] }, "iac.terraform_import")),
    });
    return true;
  }

  // ── Page-level: iac management explain ──
  if (method === "POST" && path.includes("/iac/management-status/explain")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope({ explanation: "Managed by Terraform" }, "iac.management_explain"),
      ),
    });
    return true;
  }

  // ── Page-level: code imports investigate ──
  if (method === "POST" && path.includes("/code/imports/investigate")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({ imports: [] }, "code.imports")),
    });
    return true;
  }

  // ── Page-level: code topics investigate ──
  if (method === "POST" && path.includes("/code/topics/investigate")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({ topics: [] }, "code.topics")),
    });
    return true;
  }

  // ── Page-level: entities resolve ──
  if (method === "GET" && path === "/api/v0/entities/resolve") {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({ entities: [] }, "entities.resolve")),
    });
    return true;
  }

  // ── Page-level: semantic search ──
  if (method === "POST" && path.includes("/search/semantic")) {
    // postDataJSON() is synchronous (returns null|Serializable, not a
    // Promise) — no .catch() to chain, so guard with try/catch instead.
    let body: Record<string, unknown> = {};
    try {
      body = (route.request().postDataJSON() ?? {}) as Record<string, unknown>;
    } catch {
      body = {};
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            query: typeof body?.query === "string" ? body.query : "",
            repo_id: typeof body?.repo_id === "string" ? body.repo_id : "",
            mode: "hybrid",
            search_mode: "hybrid",
            limit: 20,
            timeout_ms: 8000,
            results: [
              {
                rank: 1,
                score: 0.87,
                search_method: "hybrid",
                document: {
                  id: "doc-1",
                  repo_id: "acme/checkout-service",
                  source_kind: "repository_file",
                  title: "retry.go",
                  path: "internal/checkout/retry.go",
                  context_text: "Implements exponential backoff for checkout retries.",
                  labels: ["language:go"],
                  updated_at: "2026-06-01T00:00:00Z",
                },
                truth_scope: { level: "derived", basis: "content_index" },
                freshness: { state: "fresh" },
              },
            ],
            truncated: false,
            indexed_document_count: 512,
            corpus_limit: 1000,
            corpus_may_be_truncated: false,
            retrieval_state: "ready",
            facets: { languages: { go: 1 } },
          },
          "search.semantic",
        ),
      ),
    });
    return true;
  }

  // ── Page-level: SBOM inventory/attachments ──
  if (path.includes("/supply-chain/sbom-attestations/attachments")) {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({ inventory: [], attachments: [] }, "sbom.attachments")),
    });
    return true;
  }

  return false;
}
