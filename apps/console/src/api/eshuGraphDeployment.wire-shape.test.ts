import { describe, expect, it } from "vitest";

import { deploymentStoryToGraph } from "./eshuGraph";

const exactTruth = {
  basis: "authoritative_graph",
  capability: "platform_impact.deployment_chain",
  freshness: { state: "fresh" as const },
  level: "exact" as const,
  profile: "local_full_stack" as const,
};

describe("Graph Explorer production deployment wire shapes", () => {
  it("preserves the flat instance platform contract and expands monotonically", () => {
    const relationships = Array.from({ length: 2 }, (_, index) => ({
      reason: `relationship-${index}`,
      source_id: `k8s:service:${index}`,
      source_name: `service-${index}`,
      target_id: `k8s:deployment:${index}`,
      target_name: `deployment-${index}`,
      type: "SELECTS",
    }));
    const resources = relationships.flatMap((relationship) => [
      {
        entity_id: relationship.source_id,
        entity_name: relationship.source_name,
        kind: "Service",
      },
      {
        entity_id: relationship.target_id,
        entity_name: relationship.target_name,
        kind: "Deployment",
      },
    ]);
    const trace = {
      instances: [
        {
          environment: "prod",
          instance_id: "instance:prod",
          platform_confidence: 0.98,
          platform_kind: "ecs_service",
          platform_name: "checkout-prod",
          platform_reason: "canonical runtime placement",
        },
      ],
      k8s_relationships: relationships,
      k8s_resources: resources,
      service_name: "checkout-api",
      workload_id: "workload:checkout-api",
    };

    const summary = deploymentStoryToGraph({}, "checkout-api", trace, {
      detail: "summary",
      traceTruth: exactTruth,
    });
    const expanded = deploymentStoryToGraph({}, "checkout-api", trace, {
      detail: "expanded",
      traceTruth: exactTruth,
    });

    expect(summary.nodes).toContainEqual(
      expect.objectContaining({
        id: "platform:ecs_service:checkout-prod",
        label: "checkout-prod",
        truth: "exact",
      }),
    );
    const platformEdge = summary.edges.find(
      (edge) => edge.s === "instance:prod" && edge.verb === "RUNS_ON",
    );
    expect(platformEdge).toMatchObject({
      t: "platform:ecs_service:checkout-prod",
    });
    expect(platformEdge?.evidence).toContain("truth level: exact");
    expect(expanded.edges.filter((edge) => edge.verb === "SELECTS")).toHaveLength(2);
    expect(expanded.nodes.length).toBeGreaterThanOrEqual(summary.nodes.length);
    expect(expanded.edges.length).toBeGreaterThanOrEqual(summary.edges.length);
  });

  it("makes global node-bound omissions visible instead of silently dropping families", () => {
    const instances = Array.from({ length: 14 }, (_, index) => ({
      environment: `env-${index}`,
      instance_id: `instance:${index}`,
      platform_kind: "ecs_service",
      platform_name: `platform-${index}`,
    }));
    const artifacts = Array.from({ length: 4 }, (_, index) => ({
      artifact_family: "kustomize",
      artifact_id: `artifact:${index}`,
      relationship_type: "DEPLOYS_FROM",
      source_repo_id: `repository:source-${index}`,
      source_repo_name: `source-${index}`,
      target_repo_id: `repository:target-${index}`,
      target_repo_name: `target-${index}`,
    }));
    const graph = deploymentStoryToGraph(
      {
        deployment_evidence: { artifacts },
        entrypoints: [{ id: "entry:prod", name: "prod.example.test", type: "hostname" }],
      },
      "checkout-api",
      {
        cloud_resources: [{ id: "cloud:db", kind: "rds_instance", name: "checkout-db" }],
        deployment_sources: Array.from({ length: 3 }, (_, index) => ({
          repo_id: `repository:deployment-${index}`,
          repo_name: `deployment-${index}`,
        })),
        instances,
        k8s_relationships: Array.from({ length: 4 }, (_, index) => ({
          source_id: `k8s:relationship-source:${index}`,
          target_id: `k8s:relationship-target:${index}`,
          type: "SELECTS",
        })),
        k8s_resources: Array.from({ length: 4 }, (_, index) => ({
          entity_id: `k8s:${index}`,
          entity_name: `resource-${index}`,
          kind: "Deployment",
        })),
        network_paths: [
          {
            from: "prod.example.test",
            from_type: "hostname",
            path_type: "HOSTNAME_TO_RUNTIME",
            to: "platform-0",
            to_type: "runtime_platform",
          },
        ],
      },
      { detail: "expanded", contextTruth: exactTruth, traceTruth: exactTruth },
    );

    expect(graph.nodes.length).toBeLessThanOrEqual(60);
    expect(graph.nodes.find((node) => node.id === "summary:graph_bounds")?.label).toMatch(
      /nodes not shown/,
    );
  });

  it("does not invent repository identities when canonical IDs are absent", () => {
    const graph = deploymentStoryToGraph(
      {
        deployment_evidence: {
          artifacts: [
            {
              artifact_id: "artifact:missing-source-id",
              relationship_type: "DEPLOYS_FROM",
              source_repo_name: "gitops-config",
              target_repo_id: "repository:r_checkout",
              target_repo_name: "checkout-api",
            },
          ],
        },
      },
      "checkout-api",
      {
        deployment_sources: [{ repo_name: "runtime-deploy" }],
        service_name: "checkout-api",
      },
      { contextTruth: exactTruth, traceTruth: exactTruth },
    );

    expect(graph.nodes).not.toContainEqual(
      expect.objectContaining({ id: "repository:gitops-config" }),
    );
    expect(graph.nodes).not.toContainEqual(
      expect.objectContaining({ id: "repository:runtime-deploy" }),
    );
    expect(graph.edges).not.toContainEqual(expect.objectContaining({ verb: "DEPLOYS_FROM" }));
    expect(graph.nodes).toContainEqual(
      expect.objectContaining({
        id: "summary:not_admitted",
        label: "1 deployment relationships not admitted",
      }),
    );
    expect(graph.nodes).toContainEqual(
      expect.objectContaining({
        id: "summary:source_identity",
        label: "1 deployment sources missing canonical repository identity",
      }),
    );
  });

  it("discloses upstream API truncation instead of presenting the bounded payload as complete", () => {
    const graph = deploymentStoryToGraph(
      {
        id: "workload:checkout-api",
        instances: [{ environment: "prod", instance_id: "instance:prod" }],
        name: "checkout-api",
        result_limits: {
          instance_count: 51,
          limit: 50,
          returned_count: 50,
          truncated: true,
        },
      },
      "checkout-api",
      {
        cloud_resource_limits: truncatedLimits(50, 51),
        deployment_source_limits: truncatedLimits(50, 51),
        k8s_resource_limits: truncatedLimits(50, 51),
        runtime_topology_limits: {
          instances: truncatedLimits(50, 51),
          platform_edges: truncatedLimits(50, 51),
          provisioned_platforms: truncatedLimits(50, 51),
        },
      },
      { contextTruth: exactTruth, traceTruth: exactTruth },
    );

    expect(graph.nodes).toContainEqual(
      expect.objectContaining({
        id: "summary:source_bounds",
        label: "Source API returned incomplete deployment evidence",
      }),
    );
    const bounds = graph.nodes.find((node) => node.id === "summary:source_bounds");
    expect(bounds?.sub).toContain("workload instances");
    expect(bounds?.sub).toContain("runtime instances");
    expect(bounds?.sub).toContain("platform edges");
    expect(bounds?.sub).toContain("provisioned platforms");
    expect(bounds?.sub).toContain("deployment sources");
    expect(bounds?.sub).toContain("cloud resources");
    expect(bounds?.sub).toContain("Kubernetes resources");
    expect(bounds?.sub).toContain("observed at least 51");
  });

  it("preserves context-owned runtime truncation when the trace omits limits", () => {
    const graph = deploymentStoryToGraph(
      {
        id: "workload:checkout-api",
        instances: [{ environment: "prod", instance_id: "instance:prod" }],
        name: "checkout-api",
        runtime_topology_limits: {
          instances: truncatedLimits(50, 51),
          platform_edges: truncatedLimits(50, 51),
          provisioned_platforms: truncatedLimits(50, 51),
        },
      },
      "checkout-api",
      {},
      { contextTruth: exactTruth, traceTruth: exactTruth },
    );

    expect(graph.nodes).toContainEqual(
      expect.objectContaining({
        id: "summary:source_bounds",
        label: "Source API returned incomplete deployment evidence",
      }),
    );
    const bounds = graph.nodes.find((node) => node.id === "summary:source_bounds");
    expect(bounds?.sub).toContain("runtime instances");
    expect(bounds?.sub).toContain("platform edges");
    expect(bounds?.sub).toContain("provisioned platforms");
  });

  it("does not claim instance completeness is unavailable when context limits are complete", () => {
    const graph = deploymentStoryToGraph(
      {
        id: "workload:checkout-api",
        instances: [{ environment: "prod", instance_id: "instance:prod" }],
        name: "checkout-api",
        runtime_topology_limits: {
          instances: completeLimits(1),
          platform_edges: completeLimits(0),
          provisioned_platforms: completeLimits(0),
        },
      },
      "checkout-api",
      {},
      { contextTruth: exactTruth, traceTruth: exactTruth },
    );

    const bounds = graph.nodes.find((node) => node.id === "summary:source_bounds");
    expect(bounds?.sub).not.toContain("workload instances: completeness metadata unavailable");
    expect(bounds?.sub).toContain("deployment sources: completeness metadata unavailable");
  });

  it("uses canonical topology and deployment-source endpoints without synthetic duplicates", () => {
    const graph = deploymentStoryToGraph(
      {
        instances: [
          {
            environment: "prod",
            instance_id: "instance:checkout:prod",
            platforms: [
              {
                platform_kind: "ecs_service",
                platform_name: "checkout-prod",
              },
            ],
          },
        ],
      },
      "checkout-api",
      {
        deployment_sources: [
          {
            relationship_type: "DEPLOYMENT_SOURCE",
            repo_id: "repository:r_deploy",
            repo_name: "checkout-deploy",
            source_id: "instance:checkout:prod",
            target_id: "repository:r_deploy",
          },
        ],
        instances: [
          {
            environment: "prod",
            instance_id: "instance:checkout:prod",
            platforms: [
              {
                platform_id: "platform:canonical-ecs-service",
                platform_kind: "ecs_service",
                platform_name: "checkout-prod",
                topology_basis: "direct_runtime",
                topology_edges: [
                  topologyEdge(
                    "RUNS_ON",
                    "instance:checkout:prod",
                    "platform:canonical-ecs-service",
                  ),
                ],
              },
            ],
          },
        ],
        provisioned_platforms: [
          {
            platform_id: "platform:canonical-ecs-cluster",
            platform_kind: "ecs_cluster",
            platform_name: "shared-prod",
            topology_basis: "provisioning_fallback",
            topology_edges: [
              topologyEdge(
                "PROVISIONS_DEPENDENCY_FOR",
                "repository:r_infra",
                "repository:r_checkout",
              ),
              topologyEdge(
                "PROVISIONS_PLATFORM",
                "repository:r_infra",
                "platform:canonical-ecs-cluster",
              ),
            ],
          },
        ],
        service_name: "checkout-api",
        topology_edges: [
          topologyEdge("DEFINES", "repository:r_checkout", "workload:checkout-api"),
          topologyEdge("INSTANCE_OF", "instance:checkout:prod", "workload:checkout-api"),
        ],
        workload_id: "workload:checkout-api",
      },
      { traceTruth: exactTruth },
    );

    expect(graph.nodes).toContainEqual(
      expect.objectContaining({ id: "platform:canonical-ecs-service", label: "checkout-prod" }),
    );
    expect(graph.nodes).not.toContainEqual(
      expect.objectContaining({ id: "platform:ecs_service:checkout-prod" }),
    );
    expect(graph.nodes.filter((node) => node.label === "checkout-prod")).toHaveLength(1);
    for (const [source, verb, target] of [
      ["repository:r_checkout", "DEFINES", "workload:checkout-api"],
      ["instance:checkout:prod", "INSTANCE_OF", "workload:checkout-api"],
      ["instance:checkout:prod", "RUNS_ON", "platform:canonical-ecs-service"],
      ["instance:checkout:prod", "DEPLOYMENT_SOURCE", "repository:r_deploy"],
      ["repository:r_infra", "PROVISIONS_DEPENDENCY_FOR", "repository:r_checkout"],
      ["repository:r_infra", "PROVISIONS_PLATFORM", "platform:canonical-ecs-cluster"],
    ] as const) {
      expect(graph.edges.filter((edge) => edge.s === source && edge.verb === verb)).toEqual([
        expect.objectContaining({ t: target }),
      ]);
    }
  });
});

function truncatedLimits(returnedCount: number, observedCount: number) {
  return {
    limit: returnedCount,
    observed_count: observedCount,
    observed_count_is_lower_bound: true,
    returned_count: returnedCount,
    truncated: true,
  };
}

function completeLimits(count: number) {
  return {
    limit: 50,
    observed_count: count,
    observed_count_is_lower_bound: false,
    returned_count: count,
    truncated: false,
  };
}

function topologyEdge(relationshipType: string, sourceID: string, targetID: string) {
  return {
    confidence: 0.99,
    evidence_source: "canonical_graph",
    reason: "exact retained relationship",
    relationship_type: relationshipType,
    source_id: sourceID,
    target_id: targetID,
  };
}
