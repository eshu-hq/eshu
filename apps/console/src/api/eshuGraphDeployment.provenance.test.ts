import { describe, expect, it } from "vitest";

import { deploymentStoryToGraph } from "./eshuGraph";

const freshTruth = {
  basis: "authoritative_graph",
  capability: "platform_impact.deployment_chain",
  freshness: { state: "fresh" as const },
  level: "exact" as const,
  profile: "local_full_stack" as const,
};
const staleTruth = { ...freshTruth, freshness: { state: "stale" as const } };

describe("Graph Explorer deployment source provenance", () => {
  it("admits each relationship only from a current source that contains that exact row", () => {
    const sharedArtifact = artifact("shared");
    const graph = deploymentStoryToGraph(
      {
        deployment_evidence: {
          artifacts: [sharedArtifact, artifact("context-only")],
        },
        entrypoints: [{ id: "entry:context", name: "context.example.test", type: "hostname" }],
        id: "workload:checkout-api",
        instances: [
          {
            environment: "prod",
            instance_id: "instance:prod",
            platforms: [
              {
                platform_kind: "ecs_service",
                platform_name: "context-ecs",
                platform_reason: "context placement",
              },
            ],
          },
        ],
        name: "checkout-api",
        network_paths: [networkPath("context")],
      },
      "checkout-api",
      {
        deployment_evidence: {
          artifacts: [sharedArtifact, artifact("trace-only")],
        },
        entrypoints: [{ id: "entry:trace", name: "trace.example.test", type: "hostname" }],
        instances: [
          {
            environment: "prod",
            instance_id: "instance:prod",
            platforms: [
              {
                platform_kind: "kubernetes",
                platform_name: "trace-eks",
                platform_reason: "trace placement",
              },
            ],
          },
        ],
        network_paths: [networkPath("trace")],
        service_name: "checkout-api",
        workload_id: "workload:checkout-api",
      },
      { contextTruth: staleTruth, traceTruth: freshTruth },
    );

    const edgesFrom = (source: string) => graph.edges.filter((edge) => edge.s === source);
    expect(edgesFrom("repository:shared")).toHaveLength(1);
    expect(edgesFrom("repository:trace-only")).toHaveLength(1);
    expect(edgesFrom("repository:context-only")).toHaveLength(0);
    expect(graph.nodes.find((node) => node.id === "repository:context-only")?.sub).toContain(
      "stale",
    );

    expect(graph.edges).toContainEqual(
      expect.objectContaining({
        s: "instance:prod",
        t: "workload:checkout-api",
        verb: "INSTANCE_OF",
      }),
    );
    expect(graph.edges).toContainEqual(
      expect.objectContaining({
        s: "instance:prod",
        t: "platform:kubernetes:trace-eks",
        verb: "RUNS_ON",
      }),
    );
    expect(graph.edges).not.toContainEqual(
      expect.objectContaining({
        s: "instance:prod",
        t: "platform:ecs_service:context-ecs",
        verb: "RUNS_ON",
      }),
    );
    expect(graph.nodes.find((node) => node.id === "platform:ecs_service:context-ecs")?.truth).toBe(
      "exact",
    );

    expect(graph.edges.some((edge) => edge.s.includes("network:0:from:context"))).toBe(false);
    expect(graph.edges.some((edge) => edge.s.includes("from:trace.example.test"))).toBe(true);
    expect(graph.nodes.find((node) => node.id === "entry:context")).toBeUndefined();
    expect(graph.nodes.find((node) => node.id === "entry:trace")?.truth).toBe("exact");
    expect(graph.nodes.find((node) => node.id === "summary:entrypoints")?.label).toContain(
      "not shown",
    );
  });

  it("keeps distinct artifact generations and source handles on otherwise identical edges", () => {
    const graph = deploymentStoryToGraph(
      {
        deployment_evidence: {
          artifacts: [
            {
              ...artifact("shared"),
              artifact_id: "evidence-artifact:shared:one",
              end_line: 14,
              evidence_source: "resolver/cross-repo",
              extractor: "kustomize-parser",
              generation_id: "generation:one",
              start_line: 12,
            },
            {
              ...artifact("shared"),
              artifact_id: "evidence-artifact:shared:two",
              end_line: 24,
              evidence_source: "resolver/cross-repo",
              extractor: "kustomize-parser",
              generation_id: "generation:two",
              start_line: 22,
            },
          ],
        },
        id: "workload:checkout-api",
        name: "checkout-api",
      },
      "checkout-api",
      {},
      { contextTruth: freshTruth },
    );

    const edges = graph.edges.filter(
      (edge) => edge.s === "repository:shared" && edge.verb === "DEPLOYS_FROM",
    );
    expect(edges).toHaveLength(2);
    expect(edges.map((edge) => edge.evidence)).toEqual(
      expect.arrayContaining([
        expect.arrayContaining([
          "artifact id: evidence-artifact:shared:one",
          "generation id: generation:one",
          "source lines: 12-14",
        ]),
        expect.arrayContaining([
          "artifact id: evidence-artifact:shared:two",
          "generation id: generation:two",
          "source lines: 22-24",
        ]),
      ]),
    );
    expect(graph.nodes.find((node) => node.id === "repository:shared")?.source).toMatchObject({
      endLine: 14,
      filePath: "shared/kustomization.yaml",
      startLine: 12,
    });
  });
});

function artifact(name: string) {
  return {
    artifact_family: "kustomize",
    evidence_kind: "KUSTOMIZE_RESOURCE_REFERENCE",
    path: `${name}/kustomization.yaml`,
    relationship_type: "DEPLOYS_FROM",
    source_repo_id: `repository:${name}`,
    source_repo_name: name,
    target_repo_id: "repository:checkout-api",
    target_repo_name: "checkout-api",
  };
}

function networkPath(name: string) {
  return {
    environment: "prod",
    from: `${name}.example.test`,
    from_type: "hostname",
    path_type: "HOSTNAME_TO_RUNTIME",
    to: `${name}-runtime`,
    to_type: "runtime",
  };
}
