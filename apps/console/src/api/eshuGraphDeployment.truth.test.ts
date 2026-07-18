import { describe, expect, it } from "vitest";

import { EshuApiHttpError } from "./client";
import type { EshuApiClient } from "./client";
import { deploymentStoryToGraph, loadEntityStoryGraph } from "./eshuGraph";

const exactTruth = {
  basis: "authoritative_graph",
  capability: "platform_impact.deployment_chain",
  freshness: { state: "fresh" as const },
  level: "exact" as const,
  profile: "local_full_stack" as const,
};

describe("Graph Explorer deployment truth", () => {
  it("preserves dual-platform retained truth without inventing deployment placement", () => {
    const graph = deploymentStoryToGraph(
      retainedServiceContext(),
      "checkout-api",
      retainedDeploymentTrace(),
      { contextTruth: exactTruth, detail: "summary", traceTruth: exactTruth },
    );

    expect(graph.nodes.filter((node) => node.id === "repository:r_gitops")).toHaveLength(1);
    const node = (id: string) => graph.nodes.find((candidate) => candidate.id === id);
    expect(node("workload:checkout-api")).toMatchObject({
      hero: true,
      label: "checkout-api",
      truth: "exact",
    });
    expect(node("workload-instance:checkout-api:prod")).toMatchObject({ label: "prod" });
    expect(node("workload-instance:checkout-api:prod")?.sub).toContain(
      "workload-instance:checkout-api:prod",
    );
    expect(node("workload-instance:checkout-api:staging")).toMatchObject({ label: "staging" });
    expect(node("repository:r_runtime_only")).toMatchObject({ label: "runtime-deploy" });
    expect(node("repository:r_runtime_only")?.sub).toContain("relationship endpoints not exposed");
    expect(node("summary:api_endpoints")).toMatchObject({ label: "56 API endpoints aggregated" });

    const edge = (source: string, verb: string) =>
      graph.edges.find((candidate) => candidate.s === source && candidate.verb === verb);
    const artifactEdge = edge("repository:r_gitops", "DEPLOYS_FROM");
    expect(artifactEdge).toMatchObject({
      sourceFamily: "kustomize",
      t: "repository:r_checkout",
      truthState: "derived",
    });
    expect(artifactEdge?.evidence).toContain("evidence kind: KUSTOMIZE_RESOURCE_REFERENCE");
    expect(artifactEdge?.evidence).toContain("path: applications/checkout/prod/kustomization.yaml");
    expect(artifactEdge?.evidence).toContain("truth level: exact");
    expect(node("repository:r_gitops")?.source).toMatchObject({
      filePath: "applications/checkout/prod/kustomization.yaml",
      repoId: "repository:r_gitops",
    });
    expect(edge("workload-instance:checkout-api:prod", "INSTANCE_OF")).toMatchObject({
      t: "workload:checkout-api",
    });
    const ecsEdge = edge("workload-instance:checkout-api:prod", "RUNS_ON");
    expect(ecsEdge?.t).toMatch(/^platform:/);
    expect(ecsEdge?.evidence).toContain("platform kind: ecs_service");
    const kubernetesEdge = edge("workload-instance:checkout-api:staging", "RUNS_ON");
    expect(kubernetesEdge?.t).toMatch(/^platform:/);
    expect(kubernetesEdge?.evidence).toContain("platform kind: kubernetes");
    const selectsEdge = edge("k8s:checkout-service", "SELECTS");
    expect(selectsEdge?.t).toBe("k8s:checkout-deployment");
    expect(selectsEdge?.evidence).toContain("reason: k8s_service_name_namespace");
    const networkEdge = graph.edges.find((candidate) => candidate.verb === "HOSTNAME_TO_RUNTIME");
    expect(networkEdge?.s).toMatch(/^network:/);
    expect(networkEdge?.t).toMatch(/^network:/);
    expect(networkEdge?.evidence).toContain("environment: prod");
    expect(
      graph.edges.some((edge) =>
        ["DEPLOYS_HELM", "DEFINES", "PACKAGES", "RELATED"].includes(edge.verb),
      ),
    ).toBe(false);
    expect(graph.nodes.map((candidate) => candidate.id).sort()).toEqual([
      "cloud:checkout-db",
      "entrypoint:0:checkout.example.test",
      "k8s:checkout-deployment",
      "k8s:checkout-service",
      "network:0:from:checkout.example.test",
      "network:0:to:checkout-ecs-prod",
      "platform:ecs_service:checkout-ecs-prod",
      "platform:kubernetes:checkout-eks-staging",
      "repository:r_checkout",
      "repository:r_gitops",
      "repository:r_runtime_only",
      "summary:api_endpoints",
      "summary:artifacts",
      "workload-instance:checkout-api:prod",
      "workload-instance:checkout-api:staging",
      "workload:checkout-api",
    ]);
    expect(graph.edges).toHaveLength(9);
    expect(
      graph.edges.some(
        (edge) => edge.s === "repository:r_runtime_only" || edge.t === "repository:r_runtime_only",
      ),
    ).toBe(false);
  });

  it("keeps ambiguous, provenance-only, and stale evidence visible but non-materialized", () => {
    const graph = deploymentStoryToGraph(
      {
        id: "workload:checkout-api",
        name: "checkout-api",
        repo_id: "repository:r_checkout",
        repo_name: "checkout-api",
        deployment_evidence: {
          artifacts: [
            deploymentArtifact({
              source_repo_id: "repository:r_ambiguous",
              source_repo_name: "ambiguous-config",
              outcome: "ambiguous",
            }),
            deploymentArtifact({
              source_repo_id: "repository:r_provenance",
              source_repo_name: "provenance-config",
              provenance_only: true,
            }),
            deploymentArtifact({
              source_freshness: "stale",
              source_repo_id: "repository:r_stale",
              source_repo_name: "stale-config",
            }),
          ],
        },
      },
      "checkout-api",
      {},
      { contextTruth: exactTruth },
    );

    expect(graph.edges).toHaveLength(0);
    expect(graph.nodes.map((node) => node.label)).toEqual(
      expect.arrayContaining([
        "ambiguous-config",
        "provenance-config",
        "stale-config",
        "3 deployment relationships not admitted",
      ]),
    );
    expect(graph.nodes.find((node) => node.id === "repository:r_stale")?.sub).toContain("stale");

    const staleTruthGraph = deploymentStoryToGraph(
      {
        deployment_evidence: { artifacts: [deploymentArtifact()] },
        id: "workload:checkout-api",
        name: "checkout-api",
      },
      "checkout-api",
      {},
      { contextTruth: { ...exactTruth, freshness: { state: "stale" } } },
    );
    expect(staleTruthGraph.edges).toHaveLength(0);
    expect(staleTruthGraph.nodes.find((node) => node.id === "repository:r_gitops")?.sub).toContain(
      "stale",
    );
  });

  it("shows missing environment honestly and exposes family-aware truncation", () => {
    const instances = Array.from({ length: 14 }, (_, index) => ({
      environment: index === 0 ? "" : `env-${index}`,
      instance_id: `workload-instance:checkout-api:${index}`,
      materialization_provenance: [`fact:${index}`],
      platforms: [
        {
          platform_kind: index % 2 === 0 ? "ecs_service" : "kubernetes",
          platform_name: `platform-${index}`,
        },
      ],
    }));
    const summary = deploymentStoryToGraph(
      { id: "workload:checkout-api", name: "checkout-api", instances },
      "checkout-api",
      { instances },
      { detail: "summary" },
    );
    const expanded = deploymentStoryToGraph(
      { id: "workload:checkout-api", name: "checkout-api", instances },
      "checkout-api",
      { instances },
      { detail: "expanded" },
    );

    const missingEnvironment = summary.nodes.find(
      (node) => node.id === "workload-instance:checkout-api:0",
    );
    expect(missingEnvironment?.label).toBe("workload-instance:checkout-api:0");
    expect(missingEnvironment?.sub).toContain("environment not provided");
    expect(summary.nodes.find((node) => node.id === "summary:instances")?.label).toMatch(
      /instances not shown/,
    );
    expect(expanded.nodes.filter((node) => node.kind === "instance").length).toBeGreaterThan(
      summary.nodes.filter((node) => node.kind === "instance").length,
    );
    expect(expanded.nodes.length).toBeLessThanOrEqual(60);
    expect(expanded.edges.length).toBeLessThanOrEqual(90);
  });

  it("loads service context and deployment trace while preserving authorization failures", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        return { data: retainedServiceContext(), error: null, truth: exactTruth };
      },
      post: async (path: string) => {
        calls.push(path);
        if (path === "/api/v0/impact/trace-deployment-chain") {
          throw new EshuApiHttpError(403);
        }
        throw new Error(`unexpected POST ${path}`);
      },
    } as unknown as EshuApiClient;

    await expect(loadEntityStoryGraph(client, "checkout-api")).rejects.toMatchObject({
      status: 403,
    });
    expect(calls).toEqual([
      "/api/v0/services/checkout-api/context",
      "/api/v0/impact/trace-deployment-chain",
    ]);
  });
});

function retainedServiceContext() {
  return {
    api_surface: {
      endpoint_count: 56,
      endpoints: Array.from({ length: 12 }, (_, index) => ({ path: `/v1/items/${index}` })),
    },
    deployment_evidence: {
      artifact_count: 4,
      artifacts: [
        deploymentArtifact(),
        deploymentArtifact({ evidence_kind: "ARGOCD_APPLICATION_REFERENCE" }),
        deploymentArtifact({
          artifact_family: "helm",
          evidence_kind: "HELM_VALUES_REFERENCE",
          path: "charts/checkout/values-prod.yaml",
        }),
        deploymentArtifact({
          artifact_family: "terraform",
          evidence_kind: "TERRAFORM_MODULE_REFERENCE",
          outcome: "provenance_only",
          provenance_only: true,
          source_repo_id: "repository:r_provenance",
          source_repo_name: "terraform-platform",
        }),
      ],
    },
    id: "workload:checkout-api",
    instances: retainedDeploymentTrace().instances?.map((instance) => ({
      environment: instance.environment,
      instance_id: instance.instance_id,
    })),
    name: "checkout-api",
    repo_id: "repository:r_checkout",
    repo_name: "checkout-api",
    result_limits: { instance_count: 2, limit: 50, truncated: false },
  };
}

function retainedDeploymentTrace() {
  return {
    cloud_resources: [
      {
        confidence: 0.93,
        environment: "prod",
        id: "cloud:checkout-db",
        kind: "rds_instance",
        name: "checkout-db",
        reason: "runtime dependency",
      },
    ],
    deployment_overview: {
      cloud_resource_count: 1,
      deployment_source_count: 2,
      environment_count: 2,
      instance_count: 2,
      k8s_resource_count: 2,
    },
    deployment_sources: [
      {
        confidence: 0.97,
        reason: "canonical_instance_deployment_source",
        repo_id: "repository:r_gitops",
        repo_name: "gitops-config",
      },
      {
        confidence: 0.91,
        reason: "canonical_instance_deployment_source",
        repo_id: "repository:r_runtime_only",
        repo_name: "runtime-deploy",
      },
    ],
    entrypoints: [
      {
        environment: "prod",
        target: "checkout.example.test",
        type: "hostname",
        visibility: "public",
      },
    ],
    instances: [
      {
        environment: "prod",
        instance_id: "workload-instance:checkout-api:prod",
        materialization_confidence: 0.98,
        materialization_provenance: ["fact:ecs-prod"],
        platforms: [
          {
            platform_confidence: 0.97,
            platform_kind: "ecs_service",
            platform_name: "checkout-ecs-prod",
            platform_reason: "runtime placement",
          },
        ],
      },
      {
        environment: "staging",
        instance_id: "workload-instance:checkout-api:staging",
        materialization_confidence: 0.96,
        materialization_provenance: ["fact:k8s-staging"],
        platforms: [
          {
            platform_confidence: 0.95,
            platform_kind: "kubernetes",
            platform_name: "checkout-eks-staging",
            platform_reason: "runtime placement",
          },
        ],
      },
    ],
    k8s_relationships: [
      {
        reason: "k8s_service_name_namespace",
        source_id: "k8s:checkout-service",
        source_name: "checkout-api",
        target_id: "k8s:checkout-deployment",
        target_name: "checkout-api",
        type: "SELECTS",
      },
    ],
    k8s_resources: [
      {
        entity_id: "k8s:checkout-service",
        entity_name: "checkout-api",
        kind: "Service",
        qualified_name: "checkout/Service/checkout-api",
        relative_path: "deploy/service.yaml",
      },
      {
        entity_id: "k8s:checkout-deployment",
        entity_name: "checkout-api",
        kind: "Deployment",
        qualified_name: "checkout/Deployment/checkout-api",
        relative_path: "deploy/deployment.yaml",
      },
    ],
    network_paths: [
      {
        environment: "prod",
        from: "checkout.example.test",
        from_type: "hostname",
        path_type: "hostname_to_runtime",
        reason: "service hostname routes to runtime",
        to: "checkout-ecs-prod",
        to_type: "platform",
        visibility: "public",
      },
    ],
    repo_id: "repository:r_checkout",
    repo_name: "checkout-api",
    service_name: "checkout-api",
    workload_id: "workload:checkout-api",
  };
}

function deploymentArtifact(overrides: Record<string, unknown> = {}) {
  return {
    artifact_family: "kustomize",
    confidence: 0.96,
    confidence_basis: "resolved_relationship",
    environment: "prod",
    evidence_kind: "KUSTOMIZE_RESOURCE_REFERENCE",
    outcome: "materialized",
    path: "applications/checkout/prod/kustomization.yaml",
    relationship_type: "DEPLOYS_FROM",
    resolution_source: "resolved_relationships",
    resolved_id: "relationship:checkout-prod",
    source_freshness: "fresh",
    source_repo_id: "repository:r_gitops",
    source_repo_name: "gitops-config",
    target_repo_id: "repository:r_checkout",
    target_repo_name: "checkout-api",
    ...overrides,
  };
}
