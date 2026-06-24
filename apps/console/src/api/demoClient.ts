import { EshuApiClient, type EshuFetcher } from "./client";
import {
  blastRadius,
  changeSurface,
  cicdCount,
  cicdInventory,
  cicdList,
  cloudInventory,
  dependencyList,
  demoApiBaseUrl,
  demoDefaults,
  demoDigest,
  demoRepositoriesWire,
  deploymentTrace,
  driftList,
  filterCloudResources,
  imageList,
  importCandidates,
  managementExplanation,
  sbomAttachments,
  sbomCount,
  sbomInventory,
  unmanagedFindings
} from "./demoFixtures";
import type { EshuEnvelope, EshuTruth } from "./envelope";
export { demoApiBaseUrl, demoDefaults, demoRepositories } from "./demoFixtures";

interface DemoResult {
  readonly capability: string;
  readonly data: unknown;
  readonly error?: EshuEnvelope<unknown>["error"];
}

export function createDemoApiClient(): EshuApiClient {
  return new EshuApiClient({
    baseUrl: demoApiBaseUrl,
    fetcher: demoFetcher,
    timeoutMs: 0
  });
}

const demoFetcher: EshuFetcher = async (input, init) => {
  const request = new Request(input, init);
  const url = new URL(request.url);
  const path = stripDemoBase(url.pathname);
  const body = await requestBody(request);
  const result = demoResponse(path, request.method, url.searchParams, body);
  if (result === null) {
    return Response.json(
      envelope(null, "demo.missing", {
        code: "demo_fixture_not_found",
        message: `Demo fixture does not cover ${request.method} ${path}`
      }),
      { status: 404 }
    );
  }
  return Response.json(envelope(result.data, result.capability, result.error ?? null));
};

function demoResponse(
  path: string,
  method: string,
  params: URLSearchParams,
  body: unknown
): DemoResult | null {
  if (method === "GET" && path === "/api/v0/repositories") {
    return {
      capability: "repositories.list",
      data: {
        repositories: demoRepositoriesWire()
      }
    };
  }
  if (method === "GET" && path.endsWith("/stats")) {
    return {
      capability: "repositories.stats",
      data: {
        coverage: { source_backend: "demo_fixture" },
        entity_count: 42,
        entity_types: ["service", "workload", "terraform_resource"],
        file_count: 128,
        languages: ["TypeScript", "Go"],
        repository: { id: repoIdFromStatsPath(path), name: repoNameFromStatsPath(path) }
      }
    };
  }
  if (method === "GET" && path.endsWith("/story")) {
    return {
      capability: "repositories.story",
      data: {
        highlights: [
          "Demo fixture traces checkout code through CI, image, workload, and cloud resources."
        ],
        repository: { id: repoIdFromStoryPath(path), name: repoNameFromStoryPath(path) }
      }
    };
  }
  if (method === "GET" && path === "/api/v0/cloud/resources") {
    const resources = filterCloudResources(params);
    return {
      capability: "cloud.resources.list",
      data: {
        count: resources.length,
        limit: numberParam(params, "limit", 50),
        resources,
        truncated: false
      }
    };
  }
  if (method === "GET" && path === "/api/v0/cloud/inventory") {
    return {
      capability: "cloud.inventory.list",
      data: {
        count: cloudInventory.length,
        limit: numberParam(params, "limit", 50),
        resources: cloudInventory,
        truncated: false
      }
    };
  }
  if (method === "POST" && path === "/api/v0/cloud/runtime-drift/findings") {
    if (!isDemoCloudRuntimeRequest(body)) {
      return unsupported("cloud.runtime_drift.findings", "Demo drift fixtures only cover aws:demo-account in us-east-1.");
    }
    return {
      capability: "cloud.runtime_drift.findings",
      data: driftList("Provider-neutral demo drift for checkout cloud resources.")
    };
  }
  if (method === "POST" && path === "/api/v0/aws/runtime-drift/findings") {
    if (!isDemoAwsRuntimeRequest(body)) {
      return unsupported("aws.runtime_drift.findings", "Demo drift fixtures only cover aws:demo-account in us-east-1.");
    }
    return {
      capability: "aws.runtime_drift.findings",
      data: driftList("AWS demo drift for unmanaged checkout identity and load balancer resources.")
    };
  }
  if (method === "POST" && path === "/api/v0/iac/unmanaged-resources") {
    if (!isDemoAwsRuntimeRequest(body)) {
      return unsupported("iac.unmanaged_resources.list", "Demo unmanaged-resource fixtures only cover aws:demo-account in us-east-1.");
    }
    return {
      capability: "iac.unmanaged_resources.list",
      data: {
        findings: unmanagedFindings,
        limit: 50,
        offset: 0,
        story: "Demo fixture shows one unmanaged IAM role with read-only import context.",
        total_findings_count: unmanagedFindings.length,
        truncated: false
      }
    };
  }
  if (method === "POST" && path === "/api/v0/iac/terraform-import-plan/candidates") {
    if (!isDemoAwsRuntimeRequest(body)) {
      return unsupported("iac.terraform_import_plan.candidates", "Demo import-plan fixtures only cover aws:demo-account in us-east-1.");
    }
    return {
      capability: "iac.terraform_import_plan.candidates",
      data: {
        candidates: importCandidates,
        limit: 50,
        offset: 0,
        ready_count: 1,
        refused_count: 0,
        story: "Demo fixture import packet is ready for review only.",
        total_findings_count: importCandidates.length,
        truncated: false
      }
    };
  }
  if (method === "POST" && path === "/api/v0/iac/management-status/explain") {
    if (!isDemoAwsRuntimeRequest(body)) {
      return unsupported("iac.management_status.explain", "Demo management-status fixtures only cover aws:demo-account in us-east-1.");
    }
    return { capability: "iac.management_status.explain", data: managementExplanation };
  }
  if (method === "POST" && path === "/api/v0/impact/blast-radius") {
    if (!isDemoImpactRequest(body)) {
      return unsupported("impact.blast_radius", "Demo impact fixtures only cover checkout-service.");
    }
    return { capability: "impact.blast_radius", data: blastRadius };
  }
  if (method === "POST" && path === "/api/v0/impact/change-surface/investigate") {
    if (!isDemoImpactRequest(body)) {
      return unsupported("impact.change_surface.investigate", "Demo impact fixtures only cover checkout-service.");
    }
    return { capability: "impact.change_surface.investigate", data: changeSurface };
  }
  if (method === "POST" && path === "/api/v0/impact/trace-deployment-chain") {
    if (!isDemoImpactRequest(body)) {
      return unsupported("impact.deployment_chain.trace", "Demo deployment-trace fixtures only cover checkout-service.");
    }
    return { capability: "impact.deployment_chain.trace", data: deploymentTrace };
  }
  if (method === "GET" && path === "/api/v0/ci-cd/run-correlations/count") {
    if (!isDemoCicdQuery(params)) {
      return unsupported("ci_cd.run_correlations.count", "Demo CI/CD fixtures only cover repository:checkout-service in prod.");
    }
    return { capability: "ci_cd.run_correlations.count", data: cicdCount };
  }
  if (method === "GET" && path === "/api/v0/ci-cd/run-correlations/inventory") {
    if (!isDemoCicdQuery(params)) {
      return unsupported("ci_cd.run_correlations.inventory", "Demo CI/CD fixtures only cover repository:checkout-service in prod.");
    }
    return { capability: "ci_cd.run_correlations.inventory", data: cicdInventory };
  }
  if (method === "GET" && path === "/api/v0/ci-cd/run-correlations") {
    if (!isDemoCicdQuery(params)) {
      return unsupported("ci_cd.run_correlations.list", "Demo CI/CD fixtures only cover repository:checkout-service in prod.");
    }
    return { capability: "ci_cd.run_correlations.list", data: cicdList };
  }
  if (method === "GET" && path === "/api/v0/images") {
    return { capability: "platform_impact.container_image_list", data: imageList };
  }
  if (method === "GET" && path === "/api/v0/supply-chain/sbom-attestations/attachments/count") {
    return { capability: "supply_chain.sbom_attestation_attachments.aggregate", data: sbomCount };
  }
  if (method === "GET" && path === "/api/v0/supply-chain/sbom-attestations/attachments/inventory") {
    return { capability: "supply_chain.sbom_attestation_attachments.inventory", data: sbomInventory };
  }
  if (method === "GET" && path === "/api/v0/supply-chain/sbom-attestations/attachments") {
    return { capability: "supply_chain.sbom_attestation_attachments.list", data: sbomAttachments };
  }
  if (method === "GET" && path === "/api/v0/dependencies") {
    return { capability: "dependencies.list", data: dependencyList };
  }
  return null;
}

async function requestBody(request: Request): Promise<unknown> {
  if (request.method === "GET" || request.method === "HEAD") {
    return null;
  }
  const text = await request.text();
  if (text.trim().length === 0) {
    return null;
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return null;
  }
}

function unsupported(capability: string, message: string): DemoResult {
  return {
    capability,
    data: null,
    error: {
      code: "demo_fixture_scope_not_covered",
      message
    }
  };
}

function isDemoCloudRuntimeRequest(body: unknown): boolean {
  const record = objectBody(body);
  return record !== null &&
    field(record, "account_id") === demoDefaults.cloudDrift.accountId &&
    field(record, "provider") === demoDefaults.cloudDrift.provider &&
    field(record, "scope_id") === demoDefaults.cloudDrift.scopeId &&
    optionalFieldMatches(record, "region", demoDefaults.cloudDrift.region);
}

function isDemoAwsRuntimeRequest(body: unknown): boolean {
  const record = objectBody(body);
  return record !== null &&
    field(record, "account_id") === demoDefaults.cloudDrift.accountId &&
    field(record, "region") === demoDefaults.cloudDrift.region &&
    field(record, "scope_id") === demoDefaults.cloudDrift.scopeId &&
    optionalFieldMatches(record, "provider", demoDefaults.cloudDrift.provider);
}

function isDemoImpactRequest(body: unknown): boolean {
  const record = objectBody(body);
  if (record === null) {
    return false;
  }
  if (!optionalFieldMatches(record, "environment", demoDefaults.impact.environment)) {
    return false;
  }
  if (!optionalFieldMatches(record, "repo_id", demoDefaults.cicd.repositoryId)) {
    return false;
  }
  const candidates = [
    field(record, "service_name"),
    field(record, "target"),
    field(record, "resource_id"),
    field(record, "query"),
    field(record, "topic")
  ].filter((value) => value.length > 0);
  return candidates.length > 0 && candidates.every((value) => value === demoDefaults.impact.target);
}

function isDemoCicdQuery(params: URLSearchParams): boolean {
  return params.get("repository_id") === demoDefaults.cicd.repositoryId &&
    params.get("environment") === demoDefaults.cicd.environment &&
    optionalParamMatches(params, "provider", "github_actions") &&
    optionalParamMatches(params, "outcome", "exact") &&
    optionalParamMatches(params, "provider_run_id", "1234") &&
    optionalParamMatches(params, "run_id", "1234") &&
    optionalParamMatches(params, "artifact_digest", demoDigest) &&
    optionalParamMatches(params, "commit_sha", "abc123def456") &&
    optionalParamMatches(params, "image_ref", `registry.example/sample/checkout@${demoDigest}`);
}

function envelope<TData>(
  data: TData,
  capability: string,
  error: EshuEnvelope<TData>["error"] = null
): EshuEnvelope<TData> {
  return {
    data: error === null ? data : null,
    error,
    truth: error === null ? demoTruth(capability) : null
  };
}

function demoTruth(capability: string): EshuTruth {
  return {
    basis: "demo_fixture",
    capability,
    freshness: { state: "fresh" },
    level: "exact",
    profile: "demo_fixture",
    reason: "Prospect demo fixture corpus; not live workspace data."
  };
}

function stripDemoBase(pathname: string): string {
  return pathname.startsWith(demoApiBaseUrl.slice(0, -1))
    ? pathname.slice(demoApiBaseUrl.length - 1)
    : pathname;
}

function numberParam(params: URLSearchParams, name: string, fallback: number): number {
  const value = Number(params.get(name));
  return Number.isFinite(value) && value > 0 ? value : fallback;
}

function objectBody(value: unknown): Record<string, unknown> | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return null;
  }
  return value as Record<string, unknown>;
}

function field(record: Record<string, unknown>, name: string): string {
  const value = record[name];
  return typeof value === "string" ? value.trim() : "";
}

function optionalFieldMatches(record: Record<string, unknown>, name: string, expected: string): boolean {
  const value = field(record, name);
  return value.length === 0 || value === expected;
}

function optionalParamMatches(params: URLSearchParams, name: string, expected: string): boolean {
  const value = params.get(name)?.trim() ?? "";
  return value.length === 0 || value === expected;
}

function repoIdFromStatsPath(path: string): string {
  return decodeURIComponent(path.replace("/api/v0/repositories/", "").replace("/stats", ""));
}

function repoNameFromStatsPath(path: string): string {
  return repoIdFromStatsPath(path).replace("repository:", "");
}

function repoIdFromStoryPath(path: string): string {
  return decodeURIComponent(path.replace("/api/v0/repositories/", "").replace("/story", ""));
}

function repoNameFromStoryPath(path: string): string {
  return repoIdFromStoryPath(path).replace("repository:", "");
}
