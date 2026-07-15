// api/demoRouter.ts
// The demo-mode request dispatch table: the full if-chain that maps a demo
// request (method + path + params/body) to its response fixture. Split out
// of demoClient.ts and dynamically imported from demoClient.ts's fetcher
// (issue #5139) so the dispatch table itself -- capability names, path
// literals, scope-guard rejection wiring for every surface -- only lands in
// a session that actually issues a demo request, rather than taxing the
// eagerly loaded main bundle (scripts/console-bundle-budget.mjs) for every
// demo session regardless of which surfaces it visits. demoFetcher in
// demoClient.ts still handles the /freshness fast path and the shared
// envelope/demoTruth helpers eagerly, since those are needed for every demo
// response including this router's own results.
import { demoRepositoriesWire } from "./demoFixtures";
import type { EshuEnvelope } from "./envelope";

export interface DemoResult {
  readonly capability: string;
  readonly data: unknown;
  readonly error?: EshuEnvelope<unknown>["error"];
}

// demoResponse dispatches a demo request to its response fixture. Each
// surface's fixture module (cloud/IaC, impact, CI/CD, supply-chain,
// operations) is loaded via a dynamic import() scoped to the branch that
// needs it -- so a demo session that never visits a given page never pays
// for its fixture bytes, on top of never paying for this router itself
// until the first demo request (issue #5139).
export async function demoResponse(
  path: string,
  method: string,
  params: URLSearchParams,
  body: unknown,
): Promise<DemoResult | null> {
  if (method === "GET" && path === "/api/v0/repositories") {
    return {
      capability: "repositories.list",
      data: {
        repositories: demoRepositoriesWire(),
      },
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
        repository: { id: repoIdFromStatsPath(path), name: repoNameFromStatsPath(path) },
      },
    };
  }
  if (method === "GET" && path.endsWith("/story")) {
    return {
      capability: "repositories.story",
      data: {
        highlights: [
          "Demo fixture traces checkout code through CI, image, workload, and cloud resources.",
        ],
        repository: { id: repoIdFromStoryPath(path), name: repoNameFromStoryPath(path) },
      },
    };
  }
  if (method === "GET" && path === "/api/v0/cloud/resources") {
    const { filterCloudResources } = await import("./demoCloudFixtures");
    const resources = filterCloudResources(params);
    return {
      capability: "cloud.resources.list",
      data: {
        count: resources.length,
        limit: numberParam(params, "limit", 50),
        resources,
        truncated: false,
      },
    };
  }
  if (method === "GET" && path === "/api/v0/cloud/inventory") {
    const { cloudInventory } = await import("./demoCloudFixtures");
    return {
      capability: "cloud.inventory.list",
      data: {
        count: cloudInventory.length,
        limit: numberParam(params, "limit", 50),
        resources: cloudInventory,
        truncated: false,
      },
    };
  }
  if (method === "POST" && path === "/api/v0/cloud/runtime-drift/findings") {
    const { cloudDriftResponse, driftScopeMessage, isDemoCloudRuntimeRequest } =
      await import("./demoCloudFixtures");
    if (!isDemoCloudRuntimeRequest(body)) {
      return unsupported("cloud.runtime_drift.findings", driftScopeMessage);
    }
    return { capability: "cloud.runtime_drift.findings", data: cloudDriftResponse() };
  }
  if (method === "POST" && path === "/api/v0/aws/runtime-drift/findings") {
    const { awsDriftResponse, driftScopeMessage, isDemoAwsRuntimeRequest } =
      await import("./demoCloudFixtures");
    if (!isDemoAwsRuntimeRequest(body)) {
      return unsupported("aws.runtime_drift.findings", driftScopeMessage);
    }
    return { capability: "aws.runtime_drift.findings", data: awsDriftResponse() };
  }
  if (method === "POST" && path === "/api/v0/iac/unmanaged-resources") {
    const { isDemoAwsRuntimeRequest, unmanagedResourcesResponse, unmanagedResourcesScopeMessage } =
      await import("./demoCloudFixtures");
    if (!isDemoAwsRuntimeRequest(body)) {
      return unsupported("iac.unmanaged_resources.list", unmanagedResourcesScopeMessage);
    }
    return { capability: "iac.unmanaged_resources.list", data: unmanagedResourcesResponse() };
  }
  if (method === "POST" && path === "/api/v0/iac/terraform-import-plan/candidates") {
    const { importPlanCandidatesResponse, importPlanScopeMessage, isDemoAwsRuntimeRequest } =
      await import("./demoCloudFixtures");
    if (!isDemoAwsRuntimeRequest(body)) {
      return unsupported("iac.terraform_import_plan.candidates", importPlanScopeMessage);
    }
    return {
      capability: "iac.terraform_import_plan.candidates",
      data: importPlanCandidatesResponse(),
    };
  }
  if (method === "POST" && path === "/api/v0/iac/management-status/explain") {
    const { isDemoAwsRuntimeRequest, managementExplanation, managementStatusScopeMessage } =
      await import("./demoCloudFixtures");
    if (!isDemoAwsRuntimeRequest(body)) {
      return unsupported("iac.management_status.explain", managementStatusScopeMessage);
    }
    return { capability: "iac.management_status.explain", data: managementExplanation };
  }
  if (method === "POST" && path === "/api/v0/impact/blast-radius") {
    const { blastRadius, impactScopeMessage, isDemoImpactRequest } =
      await import("./demoImpactFixtures");
    if (!isDemoImpactRequest(body)) {
      return unsupported("impact.blast_radius", impactScopeMessage);
    }
    return { capability: "impact.blast_radius", data: blastRadius };
  }
  if (method === "POST" && path === "/api/v0/impact/change-surface/investigate") {
    const { changeSurface, impactScopeMessage, isDemoImpactRequest } =
      await import("./demoImpactFixtures");
    if (!isDemoImpactRequest(body)) {
      return unsupported("impact.change_surface.investigate", impactScopeMessage);
    }
    return { capability: "impact.change_surface.investigate", data: changeSurface };
  }
  if (method === "POST" && path === "/api/v0/impact/trace-deployment-chain") {
    const { deploymentTrace, deploymentTraceScopeMessage, isDemoImpactRequest } =
      await import("./demoImpactFixtures");
    if (!isDemoImpactRequest(body)) {
      return unsupported("impact.deployment_chain.trace", deploymentTraceScopeMessage);
    }
    return { capability: "impact.deployment_chain.trace", data: deploymentTrace };
  }
  if (method === "GET" && path === "/api/v0/ci-cd/run-correlations/count") {
    const { cicdCount, cicdScopeMessage, isDemoCicdQuery } = await import("./demoCicdFixtures");
    if (!isDemoCicdQuery(params)) {
      return unsupported("ci_cd.run_correlations.count", cicdScopeMessage);
    }
    return { capability: "ci_cd.run_correlations.count", data: cicdCount };
  }
  if (method === "GET" && path === "/api/v0/ci-cd/run-correlations/inventory") {
    const { cicdInventory, cicdScopeMessage, isDemoCicdQuery } = await import("./demoCicdFixtures");
    if (!isDemoCicdQuery(params)) {
      return unsupported("ci_cd.run_correlations.inventory", cicdScopeMessage);
    }
    return { capability: "ci_cd.run_correlations.inventory", data: cicdInventory };
  }
  if (method === "GET" && path === "/api/v0/ci-cd/run-correlations") {
    const { cicdList, cicdScopeMessage, isDemoCicdQuery } = await import("./demoCicdFixtures");
    if (!isDemoCicdQuery(params)) {
      return unsupported("ci_cd.run_correlations.list", cicdScopeMessage);
    }
    return { capability: "ci_cd.run_correlations.list", data: cicdList };
  }
  if (method === "GET" && path === "/api/v0/images") {
    const { imageList } = await import("./demoSupplyChainFixtures");
    return { capability: "platform_impact.container_image_list", data: imageList };
  }
  if (method === "GET" && path === "/api/v0/supply-chain/sbom-attestations/attachments/count") {
    const { sbomCount } = await import("./demoSupplyChainFixtures");
    return { capability: "supply_chain.sbom_attestation_attachments.aggregate", data: sbomCount };
  }
  if (method === "GET" && path === "/api/v0/supply-chain/sbom-attestations/attachments/inventory") {
    const { sbomInventory } = await import("./demoSupplyChainFixtures");
    return {
      capability: "supply_chain.sbom_attestation_attachments.inventory",
      data: sbomInventory,
    };
  }
  if (method === "GET" && path === "/api/v0/supply-chain/sbom-attestations/attachments") {
    const { sbomAttachments } = await import("./demoSupplyChainFixtures");
    return { capability: "supply_chain.sbom_attestation_attachments.list", data: sbomAttachments };
  }
  if (method === "GET" && path === "/api/v0/dependencies") {
    const { dependencyList } = await import("./demoSupplyChainFixtures");
    return { capability: "dependencies.list", data: dependencyList };
  }
  if (method === "GET" && path === "/api/v0/status/operations") {
    const { operationsBoardWire } = await import("./demoOperationsFixture");
    return { capability: "operations.status", data: operationsBoardWire() };
  }
  return null;
}

function unsupported(capability: string, message: string): DemoResult {
  return {
    capability,
    data: null,
    error: {
      code: "demo_fixture_scope_not_covered",
      message,
    },
  };
}

function numberParam(params: URLSearchParams, name: string, fallback: number): number {
  const value = Number(params.get(name));
  return Number.isFinite(value) && value > 0 ? value : fallback;
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
