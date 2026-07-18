import { describe, expect, it } from "vitest";

import { EshuApiHttpError } from "./client";
import type { EshuApiClient } from "./client";
import { deploymentStoryToGraph, loadEntityStoryGraph } from "./eshuGraph";

describe("eshuGraph deployment story", () => {
  it("deploymentStoryToGraph preserves the exact artifact relationship endpoints", () => {
    const graph = deploymentStoryToGraph(
      {
        name: "svc-platform",
        repo_name: "svc-platform",
        deployment_evidence: {
          artifacts: [
            {
              source_repo_id: "repository:r_dd626fe7",
              source_repo_name: "iac-eks-argocd",
              target_repo_id: "repository:r_078043f1",
              target_repo_name: "svc-platform",
              relationship_type: "DEPLOYS_FROM",
              artifact_family: "kustomize",
              evidence_kind: "KUSTOMIZE_RESOURCE_REFERENCE",
              environment: "acme-prod",
              path: "applicationsets/core-engineering/api-node/kustomization.yaml",
            },
            {
              source_repo_id: "repository:r_66cd2d76",
              source_repo_name: "helm-charts",
              target_repo_id: "repository:r_078043f1",
              target_repo_name: "svc-platform",
              relationship_type: "DEPLOYS_FROM",
              artifact_family: "helm",
              evidence_kind: "HELM_CHART_REFERENCE",
              path: "svc-platform/Chart.yaml",
            },
            {
              source_repo_id: "repository:r_8634f55e",
              source_repo_name: "iac-eks-observability",
              target_repo_id: "repository:r_078043f1",
              target_repo_name: "svc-platform",
              relationship_type: "DEPLOYS_FROM",
              artifact_family: "helm",
              path: "bbexporter/overlays/acme-prod/values.yaml",
            },
          ],
        },
      },
      "svc-platform",
    );

    expect(graph.nodes.map((node) => node.label).sort()).toEqual([
      "helm-charts",
      "iac-eks-argocd",
      "iac-eks-observability",
      "svc-platform",
      "svc-platform",
    ]);
    expect(graph.edges).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          s: "repository:r_dd626fe7",
          t: "repository:r_078043f1",
          verb: "DEPLOYS_FROM",
          layer: "deploy",
          sourceFamily: "kustomize",
          evidence: [
            "evidence kind: KUSTOMIZE_RESOURCE_REFERENCE",
            "path: applicationsets/core-engineering/api-node/kustomization.yaml",
            "environment: acme-prod",
          ],
        }),
        expect.objectContaining({
          s: "repository:r_66cd2d76",
          t: "repository:r_078043f1",
          verb: "DEPLOYS_FROM",
          layer: "deploy",
          sourceFamily: "helm",
          evidence: ["evidence kind: HELM_CHART_REFERENCE", "path: svc-platform/Chart.yaml"],
        }),
      ]),
    );
    expect(
      graph.edges.some((edge) => ["DEPLOYS_HELM", "PACKAGES", "RELATED"].includes(edge.verb)),
    ).toBe(false);
  });

  it("loadEntityStoryGraph prefers service deployment context when deployment evidence exists", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        return {
          data: {
            name: "svc-platform",
            repo_name: "svc-platform",
            deployment_evidence: {
              artifacts: [
                {
                  source_repo_id: "repository:r_66cd2d76",
                  source_repo_name: "helm-charts",
                  target_repo_id: "repository:r_078043f1",
                  target_repo_name: "svc-platform",
                  relationship_type: "DEPLOYS_FROM",
                  artifact_family: "helm",
                  path: "svc-platform/Chart.yaml",
                },
              ],
            },
          },
          error: null,
          truth: null,
        };
      },
      post: async (path: string) => {
        calls.push(path);
        if (path === "/api/v0/impact/trace-deployment-chain") {
          return { data: {}, error: null, truth: null };
        }
        throw new Error(`unexpected POST ${path}`);
      },
    } as unknown as EshuApiClient;

    const graph = await loadEntityStoryGraph(client, "svc-platform");

    expect(calls).toEqual([
      "/api/v0/services/svc-platform/context",
      "/api/v0/impact/trace-deployment-chain",
    ]);
    expect(graph.edges).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          s: "repository:r_66cd2d76",
          t: "repository:r_078043f1",
          verb: "DEPLOYS_FROM",
        }),
      ]),
    );
  });

  it("loadEntityStoryGraph uses repository context deployment evidence before entity-map", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        if (path === "/api/v0/services/svc-platform/context") {
          throw new EshuApiHttpError(404);
        }
        if (path === "/api/v0/repositories/repository%3Ar_078043f1/context") {
          return {
            data: {
              repository: { id: "repository:r_078043f1", name: "svc-platform" },
              deployment_evidence: {
                artifacts: [
                  {
                    source_repo_id: "repository:r_dd626fe7",
                    source_repo_name: "iac-eks-argocd",
                    target_repo_id: "repository:r_078043f1",
                    target_repo_name: "svc-platform",
                    relationship_type: "DEPLOYS_FROM",
                    artifact_family: "kustomize",
                    path: "applicationsets/core-engineering/api-node/kustomization.yaml",
                  },
                  {
                    source_repo_id: "repository:r_66cd2d76",
                    source_repo_name: "helm-charts",
                    target_repo_id: "repository:r_078043f1",
                    target_repo_name: "svc-platform",
                    relationship_type: "DEPLOYS_FROM",
                    artifact_family: "helm",
                    path: "svc-platform/Chart.yaml",
                  },
                ],
              },
            },
            error: null,
            truth: null,
          };
        }
        throw new Error(`unexpected GET ${path}`);
      },
      post: async (path: string) => {
        throw new Error(`unexpected POST ${path}`);
      },
    } as unknown as EshuApiClient;

    const graph = await loadEntityStoryGraph(client, "svc-platform", "repository:r_078043f1");

    expect(calls).toEqual([
      "/api/v0/services/svc-platform/context",
      "/api/v0/repositories/repository%3Ar_078043f1/context",
    ]);
    expect(graph.edges).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          s: "repository:r_dd626fe7",
          t: "repository:r_078043f1",
          verb: "DEPLOYS_FROM",
        }),
        expect.objectContaining({
          s: "repository:r_66cd2d76",
          t: "repository:r_078043f1",
          verb: "DEPLOYS_FROM",
        }),
      ]),
    );
    expect(
      graph.edges.some((edge) => ["DEPLOYS_HELM", "PACKAGES", "RELATED"].includes(edge.verb)),
    ).toBe(false);
  });

  it("loadEntityStoryGraph falls back to entity-map when service context is not found", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        throw new EshuApiHttpError(404);
      },
      post: async (path: string) => {
        calls.push(path);
        return {
          data: {
            from: "repository:r1",
            resolution: {
              candidates: [{ id: "repository:r1", name: "repo-a", labels: ["Repository"] }],
            },
            evidence: {
              relationships: [
                {
                  entity_id: "workload:svc",
                  entity_name: "svc",
                  entity_labels: ["Workload"],
                  direction: "outgoing",
                  relationship_type: "DEPLOYS_FROM",
                },
              ],
            },
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    const graph = await loadEntityStoryGraph(client, "repository:r1");

    expect(calls).toEqual([
      "/api/v0/services/repository%3Ar1/context",
      "/api/v0/impact/entity-map",
    ]);
    expect(graph.nodes.find((node) => node.hero)?.label).toBe("repo-a");
  });
});
