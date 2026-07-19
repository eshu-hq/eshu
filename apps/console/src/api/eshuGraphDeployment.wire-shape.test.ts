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
      }),
    );
    expect(summary.edges).toContainEqual(
      expect.objectContaining({
        s: "instance:prod",
        t: "platform:ecs_service:checkout-prod",
        verb: "RUNS_ON",
      }),
    );
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
});
