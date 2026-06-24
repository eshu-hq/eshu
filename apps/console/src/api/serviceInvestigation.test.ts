import { describe, expect, it, vi } from "vitest";

import { EshuApiHttpError, type EshuApiClient } from "./client";
import type { EshuEnvelope } from "./envelope";
import {
  loadServiceInvestigation,
  normalizeServiceInvestigation,
  type ServiceInvestigationResponse
} from "./serviceInvestigation";

function investigationResponse(): ServiceInvestigationResponse {
  return {
    coverage_summary: { state: "partial", reason: "only deploy evidence indexed", repository_count: 3, repositories_with_evidence_count: 1, truncated: true },
    evidence_families_found: ["deployment", "source"],
    investigation_findings: [{ family: "deployment", evidence_path: "deploy/ecs.tf", summary: "ECS service declared" }],
    recommended_next_calls: [
      { tool: "get_service_story", arguments: { workload_id: "payments" }, reason: "open the dependency graph" },
      { tool: "trace_deployment_chain", arguments: { service_id: "svc-1" }, reason: "trace the deploy chain" }
    ],
    repositories_with_evidence: [{ repo_name: "payments", roles: ["service"], evidence_families: ["deployment"] }],
    service_story_path: "/api/v0/services/payments/story",
    service_context_path: "/api/v0/services/payments/context"
  };
}

describe("normalizeServiceInvestigation", () => {
  it("maps coverage, families, findings, next calls, and repositories", () => {
    const investigation = normalizeServiceInvestigation(investigationResponse());
    expect(investigation.coverage.state).toBe("partial");
    expect(investigation.coverage.truncated).toBe(true);
    expect(investigation.evidenceFamilies).toEqual(["deployment", "source"]);
    expect(investigation.findings[0]?.path).toBe("deploy/ecs.tf");
    expect(investigation.nextCalls).toHaveLength(2);
    expect(investigation.repositories[0]?.name).toBe("payments");
  });
});

describe("loadServiceInvestigation", () => {
  it("fetches and normalizes the investigation packet with its paths", async () => {
    const paths: string[] = [];
    const client = {
      get: vi.fn(async (path: string): Promise<EshuEnvelope<ServiceInvestigationResponse>> => {
        paths.push(path);
        return {
          data: investigationResponse(),
          error: null,
          truth: { capability: "service.investigation.read", freshness: { state: "stale" }, level: "derived", profile: "local_authoritative" }
        };
      })
    } as unknown as EshuApiClient;

    const result = await loadServiceInvestigation(client, "payments");
    expect(paths).toEqual(["/api/v0/investigations/services/payments"]);
    expect(result.error).toBeNull();
    expect(result.investigation.coverage.state).toBe("partial");
    expect(result.storyPath).toBe("/api/v0/services/payments/story");
    expect(result.contextPath).toBe("/api/v0/services/payments/context");
    expect(result.truth?.freshness.state).toBe("stale");
  });

  it("encodes the service name", async () => {
    const paths: string[] = [];
    const client = {
      get: vi.fn(async (path: string) => {
        paths.push(path);
        return { data: investigationResponse(), error: null, truth: null };
      })
    } as unknown as EshuApiClient;
    await loadServiceInvestigation(client, "team/payments");
    expect(paths[0]).toBe("/api/v0/investigations/services/team%2Fpayments");
  });

  it("surfaces a 200 error envelope with an empty investigation and no stale content", async () => {
    const client = {
      get: vi.fn(async () => ({
        data: null,
        error: { code: "not_found", message: "service not found" },
        truth: null
      }))
    } as unknown as EshuApiClient;

    const result = await loadServiceInvestigation(client, "ghost");
    expect(result.error?.code).toBe("not_found");
    expect(result.investigation.findings).toHaveLength(0);
    expect(result.investigation.evidenceFamilies).toHaveLength(0);
  });

  it("converts a thrown EshuApiHttpError (non-2xx) into an error result", async () => {
    const client = {
      get: vi.fn(async () => {
        throw new EshuApiHttpError(404, { code: "not_found", message: "service not found" });
      })
    } as unknown as EshuApiClient;

    const result = await loadServiceInvestigation(client, "ghost");
    expect(result.error?.code).toBe("not_found");
    expect(result.investigation.findings).toHaveLength(0);
  });

  it("falls back to an http error when a thrown failure carries no structured error", async () => {
    const client = {
      get: vi.fn(async () => {
        throw new EshuApiHttpError(503);
      })
    } as unknown as EshuApiClient;

    const result = await loadServiceInvestigation(client, "payments");
    expect(result.error?.code).toBe("http_503");
  });
});
