import { loadCICDRunCorrelationReview } from "./cicdRunCorrelations";
import { EshuApiClient } from "./client";
import { inspectionRequest } from "../test/inspectionRequest";

describe("ci/cd run correlation adapter", () => {
  it("loads cheap rollups plus a bounded anchored row page", async () => {
    const calls: string[] = [];
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
        const request = inspectionRequest(input, init);
        const url = new URL(request.url);
        calls.push(`${url.pathname}${url.search}`);
        if (url.pathname.endsWith("/count")) {
          return Response.json({
            data: countPayload(),
            error: null,
            truth: truthEnvelope("ci_cd.run_correlations.aggregate")
          });
        }
        if (url.pathname.endsWith("/inventory")) {
          return Response.json({
            data: inventoryPayload(),
            error: null,
            truth: truthEnvelope("ci_cd.run_correlations.aggregate")
          });
        }
        return Response.json({
          data: listPayload(),
          error: null,
          truth: truthEnvelope("ci_cd.run_correlations.list")
        });
      }
    });

    const result = await loadCICDRunCorrelationReview(client, {
      commitSha: "abc123",
      inventoryLimit: 20,
      limit: 250,
      outcome: "exact",
      repositoryId: "repo-api"
    });

    expect(calls).toEqual([
      "/api/v0/ci-cd/run-correlations/count?repository_id=repo-api&commit_sha=abc123&outcome=exact",
      "/api/v0/ci-cd/run-correlations/inventory?group_by=outcome&repository_id=repo-api&commit_sha=abc123&outcome=exact&limit=20",
      "/api/v0/ci-cd/run-correlations?repository_id=repo-api&commit_sha=abc123&outcome=exact&limit=200"
    ]);
    expect(result.count.status).toBe("ready");
    expect(result.inventory.status).toBe("ready");
    expect(result.list.status).toBe("ready");
    if (result.list.status !== "ready" || result.count.status !== "ready" || result.inventory.status !== "ready") {
      throw new Error("expected ready CI/CD review sections");
    }
    expect(result.count.data.totalCorrelations).toBe(42);
    expect(result.inventory.data.buckets.map((bucket) => bucket.value)).toEqual(["exact", "derived"]);
    expect(result.list.data.correlations[0]?.runId).toBe("12345");
    expect(result.list.data.evidenceSummary.missingEvidence).toContain("ci_run_to_image_artifact_evidence_missing");
    expect(result.list.truth?.capability).toBe("ci_cd.run_correlations.list");
  });

  it("skips the expensive row list until an anchor is present", async () => {
    const calls: string[] = [];
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
        const request = inspectionRequest(input, init);
        const url = new URL(request.url);
        calls.push(`${url.pathname}${url.search}`);
        return Response.json({
          data: url.pathname.endsWith("/count") ? countPayload() : inventoryPayload(),
          error: null,
          truth: truthEnvelope("ci_cd.run_correlations.aggregate")
        });
      }
    });

    const result = await loadCICDRunCorrelationReview(client, {});

    expect(calls).toEqual([
      "/api/v0/ci-cd/run-correlations/count",
      "/api/v0/ci-cd/run-correlations/inventory?group_by=outcome&limit=25"
    ]);
    expect(result.list.status).toBe("skipped");
    expect(result.list.status === "skipped" ? result.list.reason : "").toContain("anchor");
  });

  it("surfaces unavailable list envelopes without hiding aggregate truth", async () => {
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
        const request = inspectionRequest(input, init);
        const url = new URL(request.url);
        if (url.pathname.endsWith("/count")) {
          return Response.json({
            data: countPayload(),
            error: null,
            truth: truthEnvelope("ci_cd.run_correlations.aggregate")
          });
        }
        if (url.pathname.endsWith("/inventory")) {
          return Response.json({
            data: inventoryPayload(),
            error: null,
            truth: truthEnvelope("ci_cd.run_correlations.aggregate")
          });
        }
        return Response.json({
          data: null,
          error: {
            code: "read_model_unavailable",
            message: "CI/CD run correlation read model is unavailable"
          },
          truth: null
        });
      }
    });

    const result = await loadCICDRunCorrelationReview(client, { repositoryId: "repo-api" });

    expect(result.count.status).toBe("ready");
    expect(result.inventory.status).toBe("ready");
    expect(result.list.status).toBe("unavailable");
    expect(result.list.status === "unavailable" ? result.list.error : "").toContain("read_model_unavailable");
  });
});

function truthEnvelope(capability: string) {
  return {
    basis: "semantic_facts",
    capability,
    freshness: { state: "fresh" },
    level: "derived",
    profile: "local_authoritative"
  };
}

function countPayload(): Record<string, unknown> {
  return {
    by_environment: { prod: 30, stage: 12 },
    by_outcome: { derived: 10, exact: 32 },
    by_provider: { github_actions: 42 },
    scope: { repository_id: "repo-api" },
    total_correlations: 42
  };
}

function inventoryPayload(): Record<string, unknown> {
  return {
    buckets: [
      { count: 32, dimension: "outcome", value: "exact" },
      { count: 10, dimension: "outcome", value: "derived" }
    ],
    count: 2,
    group_by: "outcome",
    limit: 20,
    next_offset: null,
    offset: 0,
    scope: { repository_id: "repo-api" },
    truncated: false
  };
}

function listPayload(): Record<string, unknown> {
  return {
    correlations: [{
      artifact_digest: "sha256:abc",
      canonical_target: "checkout-api",
      canonical_writes: 1,
      commit_sha: "abc123",
      correlation_id: "correlation-1",
      correlation_kind: "workflow_artifact",
      environment: "prod",
      evidence_fact_ids: ["fact-run", "fact-artifact"],
      image_ref: "registry.example.test/team/api:prod",
      outcome: "exact",
      provider: "github_actions",
      provenance_only: false,
      reason: "workflow artifact digest matched image identity",
      repository_id: "repo-api",
      run_id: "12345"
    }],
    count: 1,
    evidence_summary: {
      live_run_correlations: { count: 1, state: "present", truncated: false },
      missing_evidence: ["ci_run_to_image_artifact_evidence_missing"],
      run_artifact_evidence: {
        ambiguous_count: 0,
        artifact_digest_count: 1,
        count: 1,
        image_ref_count: 1,
        state: "present"
      },
      static_workflow_artifacts: {
        count: 2,
        evidence_class: "workflow_image_ref",
        image_ref_count: 1,
        paths: [".github/workflows/deploy.yml"],
        state: "present",
        truncated: false
      }
    },
    limit: 200,
    truncated: false
  };
}
