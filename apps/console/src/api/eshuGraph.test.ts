import { describe, expect, it } from "vitest";

import { EshuApiHttpError } from "./client";
import type { EshuApiClient } from "./client";
import { relationshipsToGraph, loadEntityGraph, loadEntityMapGraph, loadEntityStoryGraph, loadBlastGraph, resolveEntityName, codeRelationshipsToGraph, entityMapToGraph, deploymentStoryToGraph, recommendedModeForKind } from "./eshuGraph";

describe("eshuGraph", () => {
  it("loadBlastGraph posts target + target_type and maps the affected list", async () => {
    let calledPath = "";
    let body: unknown = null;
    const client = {
      post: async (path: string, b: unknown) => {
        calledPath = path;
        body = b;
        return {
          data: {
            target: "catalog-api",
            target_type: "repository",
            affected: [
              { repo: "lib-common", hops: 1 },
              { repo: "DISTINCT affected.name" }, // backend null-projection placeholder
              { repo: "" }
            ]
          },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    const graph = await loadBlastGraph(client, "catalog-api");
    expect(calledPath).toBe("/api/v0/impact/blast-radius");
    expect(body).toMatchObject({ target: "catalog-api", target_type: "repository" });
    const hero = graph.nodes.find((n) => n.hero);
    expect(hero?.label).toBe("catalog-api");
    // Only the origin + the one real dependent; placeholder/empty rows dropped.
    expect(graph.nodes.map((n) => n.label).sort()).toEqual(["catalog-api", "lib-common"]);
    expect(graph.edges).toHaveLength(1);
    expect(graph.edges[0]).toMatchObject({ s: "lib-common", t: hero?.id, verb: "DEPENDS_ON" });
  });

  it("loadBlastGraph renders the origin alone when there are no real dependents", async () => {
    const client = {
      post: async () => ({
        data: { target: "catalog-api", affected: [{ repo: "DISTINCT affected.name" }] },
        error: null,
        truth: null
      })
    } as unknown as EshuApiClient;
    const graph = await loadBlastGraph(client, "catalog-api");
    expect(graph.nodes).toHaveLength(1);
    expect(graph.nodes[0]?.hero).toBe(true);
    expect(graph.edges).toHaveLength(0);
  });

  it("loadBlastGraph rejects error envelopes instead of rendering a fake origin graph", async () => {
    const client = {
      post: async () => ({
        data: null,
        error: {
          code: "unsupported_capability",
          message: "blast radius unavailable"
        },
        truth: null
      })
    } as unknown as EshuApiClient;

    await expect(loadBlastGraph(client, "catalog-api")).rejects.toThrow("unsupported_capability");
  });

  it("relationshipsToGraph builds a hero center and maps verbs to layers + direction", () => {
    const graph = relationshipsToGraph({
      target: { id: "svc:checkout", name: "checkout", type: "service" },
      relationships: [
        { verb: "DEPENDS_ON", target: { id: "svc:payments", name: "payments", type: "service" } },
        { verb: "CALLS", direction: "incoming", target: { id: "fn:caller", name: "caller", type: "function" } }
      ]
    }, "checkout");

    const hero = graph.nodes.find((n) => n.hero);
    expect(hero?.id).toBe("svc:checkout");
    expect(graph.nodes).toHaveLength(3);
    const dep = graph.edges.find((e) => e.verb === "DEPENDS_ON");
    expect(dep).toMatchObject({ s: "svc:checkout", t: "svc:payments", layer: "runtime" });
    // incoming edge points back into the center
    const inc = graph.edges.find((e) => e.verb === "CALLS");
    expect(inc).toMatchObject({ s: "fn:caller", t: "svc:checkout", layer: "code" });
  });

  it("codeRelationshipsToGraph maps incoming/outgoing edges around the center", () => {
    const graph = codeRelationshipsToGraph({
      entity_id: "content-entity:e_center", name: "createNewVersion", labels: ["Function"],
      incoming: [{ type: "CALLS", source_id: "content-entity:e_main", source_name: "main" }],
      outgoing: [{ type: "CALLS", target_id: "content-entity:e_dep", target_name: "listFiles" }]
    }, { id: "content-entity:e_center", name: "createNewVersion" });
    const hero = graph.nodes.find((n) => n.hero);
    expect(hero?.id).toBe("content-entity:e_center");
    expect(graph.nodes).toHaveLength(3);
    expect(graph.edges.find((e) => e.s === "content-entity:e_main")).toMatchObject({ t: "content-entity:e_center", verb: "CALLS", layer: "code" });
    expect(graph.edges.find((e) => e.t === "content-entity:e_dep")).toMatchObject({ s: "content-entity:e_center", verb: "CALLS", layer: "code" });
  });

  it("codeRelationshipsToGraph classifies related code symbols from relationship direction", () => {
    const graph = codeRelationshipsToGraph({
      entity_id: "content-entity:e_center", name: "handler", labels: ["Function"],
      incoming: [{ type: "CALLS", source_id: "content-entity:e_route", source_name: "route" }],
      outgoing: [
        { type: "CALLS", target_id: "content-entity:e_callee", target_name: "loadProfile" },
        { type: "IMPORTS", target_id: "content-entity:e_pkg", target_name: "apiClient" }
      ]
    }, { id: "content-entity:e_center", name: "handler" });

    expect(graph.nodes.find((node) => node.id === "content-entity:e_route")).toMatchObject({ kind: "client", sub: "incoming CALLS" });
    expect(graph.nodes.find((node) => node.id === "content-entity:e_callee")).toMatchObject({ kind: "client", sub: "outgoing CALLS" });
    expect(graph.nodes.find((node) => node.id === "content-entity:e_pkg")).toMatchObject({ kind: "library", sub: "outgoing IMPORTS" });
  });

  it("codeRelationshipsToGraph keeps source metadata for the centered code entity", () => {
    const graph = codeRelationshipsToGraph({
      entity_id: "content-entity:e_center",
      name: "searchByPortalId",
      labels: ["Function"],
      repo_id: "repository:r_platform",
      repo_name: "svc-platform",
      file_path: "server/resources/listing/index.js",
      start_line: 1653,
      end_line: 1662,
      incoming: [],
      outgoing: []
    }, { id: "content-entity:e_center", name: "searchByPortalId" });

    expect(graph.nodes.find((node) => node.hero)?.source).toEqual({
      repoId: "repository:r_platform",
      repoName: "svc-platform",
      filePath: "server/resources/listing/index.js",
      startLine: 1653,
      endLine: 1662
    });
  });

  it("loadEntityGraph resolves the query to an entity_id, then posts code/relationships by entity_id", async () => {
    let calledPath = "";
    let body: unknown = null;
    const client = {
      // entities/resolve (used by resolveEntity) goes through postJson
      postJson: async () => ({ entities: [{ id: "content-entity:e_center", name: "createNewVersion", labels: ["Function"] }] }),
      post: async (path: string, b: unknown) => {
        calledPath = path; body = b;
        return { data: { entity_id: "content-entity:e_center", name: "createNewVersion", labels: ["Function"], incoming: [{ type: "CALLS", source_id: "content-entity:e_main", source_name: "main" }], outgoing: [] }, error: null, truth: null };
      }
    } as unknown as EshuApiClient;
    const graph = await loadEntityGraph(client, "createNewVersion");
    expect(calledPath).toBe("/api/v0/code/relationships");
    expect(body).toMatchObject({ entity_id: "content-entity:e_center", max_depth: 1 });
    expect(body).not.toMatchObject({ depth: 1 });
    expect(graph.nodes.find((n) => n.hero)?.label).toBe("createNewVersion");
    expect(graph.edges).toHaveLength(1);
  });

  it("entityMapToGraph centers on the resolved candidate and maps evidence.relationships by direction", () => {
    const graph = entityMapToGraph({
      from: "catalog-api",
      resolution: { candidates: [{ id: "workload:catalog-api", name: "catalog-api", labels: ["Workload"] }] },
      evidence: { relationships: [
        { entity_id: "repository:r_f9600c28", entity_name: "catalog-api", entity_labels: ["Repository"], direction: "incoming", relationship_type: "DEFINES", relationship_source: "graph", repo_id: "repository:r_f9600c28", depth: 1 },
        // no relationship_type (singular) — must fall back to relationship_types[0]
        { entity_id: "workload:payments", entity_name: "payments", entity_labels: ["Workload"], direction: "outgoing", relationship_types: ["DEPENDS_ON"], environment: "acme-prod" }
      ] }
    }, "catalog-api");
    const hero = graph.nodes.find((n) => n.hero);
    expect(hero?.id).toBe("workload:catalog-api");
    // nodes are keyed by entity_id, not the display name
    expect(graph.nodes.some((n) => n.id === "workload:payments" && n.label === "payments")).toBe(true);
    expect(graph.edges.find((e) => e.verb === "DEFINES")).toMatchObject({ s: "repository:r_f9600c28", t: "workload:catalog-api", evidence: ["relationship source: graph", "direction: incoming", "entity labels: Repository", "repo: repository:r_f9600c28", "depth: 1"] });
    expect(graph.edges.find((e) => e.verb === "DEPENDS_ON")).toMatchObject({ s: "workload:catalog-api", t: "workload:payments", layer: "runtime", evidence: ["relationship source: graph", "direction: outgoing", "entity labels: Workload", "environment: acme-prod"] });
  });

  it("loadEntityMapGraph posts impact/entity-map with from and parses evidence.relationships", async () => {
    let calledPath = "";
    let body: unknown = null;
    const client = {
      post: async (path: string, b: unknown) => {
        calledPath = path; body = b;
        return { data: { from: "checkout", resolution: { candidates: [{ id: "workload:checkout", name: "checkout", labels: ["Workload"] }] }, evidence: { relationships: [{ entity_name: "payments", entity_labels: ["Workload"], direction: "outgoing", relationship_type: "DEPENDS_ON" }] } }, error: null, truth: null };
      }
    } as unknown as EshuApiClient;
    const graph = await loadEntityMapGraph(client, "checkout");
    expect(calledPath).toBe("/api/v0/impact/entity-map");
    // The endpoint's request field is `depth` (not `max_depth`).
    expect(body).toMatchObject({ from: "checkout", depth: 2 });
    expect(body).not.toHaveProperty("max_depth");
    expect(graph.nodes.find((n) => n.hero)?.id).toBe("workload:checkout");
    expect(graph.edges).toHaveLength(1);
  });

  it("loadEntityMapGraph rejects API error envelopes instead of rendering a fallback node", async () => {
    const client = {
      post: async () => ({
        data: null,
        error: {
          code: "unsupported_capability",
          message: "entity map is unavailable"
        },
        truth: null
      })
    } as unknown as EshuApiClient;

    await expect(loadEntityMapGraph(client, "checkout")).rejects.toThrow(
      "unsupported_capability: entity map is unavailable"
    );
  });

  it("deploymentStoryToGraph turns service context artifacts into a typed deployment chain", () => {
    const graph = deploymentStoryToGraph({
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
            path: "applicationsets/core-engineering/api-node/kustomization.yaml"
          },
          {
            source_repo_id: "repository:r_66cd2d76",
            source_repo_name: "helm-charts",
            target_repo_id: "repository:r_078043f1",
            target_repo_name: "svc-platform",
            relationship_type: "DEPLOYS_FROM",
            artifact_family: "helm",
            evidence_kind: "HELM_CHART_REFERENCE",
            path: "svc-platform/Chart.yaml"
          },
          {
            source_repo_id: "repository:r_8634f55e",
            source_repo_name: "iac-eks-observability",
            target_repo_id: "repository:r_078043f1",
            target_repo_name: "svc-platform",
            relationship_type: "DEPLOYS_FROM",
            artifact_family: "helm",
            path: "bbexporter/overlays/acme-prod/values.yaml"
          }
        ]
      }
    }, "svc-platform");

    expect(graph.nodes.map((n) => n.label).sort()).toEqual([
      "helm-charts",
      "iac-eks-argocd",
      "svc-platform",
      "svc-platform"
    ]);
    expect(graph.edges).toEqual(expect.arrayContaining([
      expect.objectContaining({ s: "repository:r_dd626fe7", t: "repository:r_66cd2d76", verb: "DEPLOYS_HELM", layer: "deploy", evidence: ["artifact family: kustomize", "evidence kind: KUSTOMIZE_RESOURCE_REFERENCE", "path: applicationsets/core-engineering/api-node/kustomization.yaml", "environment: acme-prod"] }),
      expect.objectContaining({ s: "repository:r_66cd2d76", t: "repository:r_078043f1", verb: "PACKAGES", layer: "deploy", evidence: ["artifact family: helm", "evidence kind: HELM_CHART_REFERENCE", "path: svc-platform/Chart.yaml"] }),
      expect.objectContaining({ s: "repository:r_078043f1", t: "workload:svc-platform", verb: "DEPLOYS_FROM", layer: "deploy" })
    ]));
    expect(graph.edges.some((edge) => edge.verb === "RELATED")).toBe(false);
    expect(graph.nodes.some((node) => node.label === "iac-eks-observability")).toBe(false);
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
              artifacts: [{
                source_repo_id: "repository:r_66cd2d76",
                source_repo_name: "helm-charts",
                target_repo_id: "repository:r_078043f1",
                target_repo_name: "svc-platform",
                relationship_type: "DEPLOYS_FROM",
                artifact_family: "helm",
                path: "svc-platform/Chart.yaml"
              }]
            }
          },
          error: null,
          truth: null
        };
      },
      post: async () => {
        throw new Error("entity-map should not be called when deployment evidence exists");
      }
    } as unknown as EshuApiClient;

    const graph = await loadEntityStoryGraph(client, "svc-platform");

    expect(calls).toEqual(["/api/v0/services/svc-platform/context"]);
    expect(graph.edges).toEqual(expect.arrayContaining([
      expect.objectContaining({ s: "repository:r_66cd2d76", t: "repository:r_078043f1", verb: "PACKAGES" }),
      expect.objectContaining({ s: "repository:r_078043f1", t: "workload:svc-platform", verb: "DEPLOYS_FROM" })
    ]));
  });

  it("loadEntityStoryGraph uses repository context deployment evidence before entity-map", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        if (path === "/api/v0/services/svc-platform/context") {
          return { data: { name: "svc-platform", repo_name: "svc-platform" }, error: null, truth: null };
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
                    path: "applicationsets/core-engineering/api-node/kustomization.yaml"
                  },
                  {
                    source_repo_id: "repository:r_66cd2d76",
                    source_repo_name: "helm-charts",
                    target_repo_id: "repository:r_078043f1",
                    target_repo_name: "svc-platform",
                    relationship_type: "DEPLOYS_FROM",
                    artifact_family: "helm",
                    path: "svc-platform/Chart.yaml"
                  }
                ]
              }
            },
            error: null,
            truth: null
          };
        }
        throw new Error(`unexpected GET ${path}`);
      },
      post: async () => {
        throw new Error("entity-map should not be called when repository deployment evidence exists");
      }
    } as unknown as EshuApiClient;

    const graph = await loadEntityStoryGraph(client, "svc-platform", "repository:r_078043f1");

    expect(calls).toEqual([
      "/api/v0/services/svc-platform/context",
      "/api/v0/repositories/repository%3Ar_078043f1/context"
    ]);
    expect(graph.edges).toEqual(expect.arrayContaining([
      expect.objectContaining({ s: "repository:r_dd626fe7", t: "repository:r_66cd2d76", verb: "DEPLOYS_HELM" }),
      expect.objectContaining({ s: "repository:r_66cd2d76", t: "repository:r_078043f1", verb: "PACKAGES" }),
      expect.objectContaining({ s: "repository:r_078043f1", t: "workload:svc-platform", verb: "DEPLOYS_FROM" })
    ]));
    expect(graph.edges.some((edge) => edge.verb === "RELATED")).toBe(false);
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
            resolution: { candidates: [{ id: "repository:r1", name: "repo-a", labels: ["Repository"] }] },
            evidence: { relationships: [{ entity_id: "workload:svc", entity_name: "svc", entity_labels: ["Workload"], direction: "outgoing", relationship_type: "DEPLOYS_FROM" }] }
          },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    const graph = await loadEntityStoryGraph(client, "repository:r1");

    expect(calls).toEqual([
      "/api/v0/services/repository%3Ar1/context",
      "/api/v0/impact/entity-map"
    ]);
    expect(graph.nodes.find((node) => node.hero)?.label).toBe("repo-a");
  });

  it("loadEntityGraph renders the searched node alone when nothing resolves (no bogus entity_id 404)", async () => {
    let postCalled = false;
    const client = {
      postJson: async () => ({ entities: [] }), // resolve returns no candidate
      post: async () => { postCalled = true; return { data: {}, error: null, truth: null }; }
    } as unknown as EshuApiClient;
    const graph = await loadEntityGraph(client, "no-such-entity");
    expect(postCalled).toBe(false); // never posts a raw name as entity_id
    expect(graph.nodes).toHaveLength(1);
    expect(graph.nodes[0]).toMatchObject({ id: "no-such-entity", hero: true });
    expect(graph.edges).toHaveLength(0);
  });

  it("loadEntityGraph degrades a 404 from code/relationships to an empty graph (center alone)", async () => {
    const client = {
      postJson: async () => ({ entities: [{ id: "workload:catalog-api", name: "catalog-api", labels: ["Workload"] }] }),
      post: async () => { throw new EshuApiHttpError(404); }
    } as unknown as EshuApiClient;
    const graph = await loadEntityGraph(client, "catalog-api");
    // No code relationships exist for a service: render the resolved node alone,
    // not a thrown error.
    expect(graph.nodes).toHaveLength(1);
    expect(graph.nodes[0]).toMatchObject({ id: "workload:catalog-api", hero: true });
    expect(graph.edges).toHaveLength(0);
  });

  it("loadEntityGraph still surfaces a real server error (500) from code/relationships", async () => {
    const client = {
      postJson: async () => ({ entities: [{ id: "content-entity:e_center", name: "createNewVersion", labels: ["Function"] }] }),
      post: async () => { throw new EshuApiHttpError(500); }
    } as unknown as EshuApiClient;
    await expect(loadEntityGraph(client, "createNewVersion")).rejects.toBeInstanceOf(EshuApiHttpError);
  });

  it("recommendedModeForKind picks direct for code entities and neighborhood for service/infra", () => {
    expect(recommendedModeForKind("Function")).toBe("direct");
    expect(recommendedModeForKind("File")).toBe("direct");
    expect(recommendedModeForKind("Class")).toBe("direct");
    expect(recommendedModeForKind("Method")).toBe("direct");
    expect(recommendedModeForKind("Service")).toBe("neighborhood");
    expect(recommendedModeForKind("Workload")).toBe("neighborhood");
    expect(recommendedModeForKind("Repository")).toBe("neighborhood");
    expect(recommendedModeForKind("AwsResource")).toBe("neighborhood");
    // Unknown kinds keep the existing direct default so code search is unchanged.
    expect(recommendedModeForKind(undefined)).toBe("direct");
    expect(recommendedModeForKind("")).toBe("direct");
  });

  it("resolveEntityName returns the top candidate, falling back to the raw query", async () => {
    const withMatch = { postJson: async () => ({ entities: [{ id: "svc:checkout", name: "checkout-service", type: "service", repo_id: "r", repo_name: "r", file_path: "", labels: [] }] }) } as unknown as EshuApiClient;
    expect(await resolveEntityName(withMatch, "checkout")).toBe("checkout-service");

    const noMatch = { postJson: async () => ({ entities: [] }) } as unknown as EshuApiClient;
    expect(await resolveEntityName(noMatch, "checkout")).toBe("checkout");

    const errClient = { postJson: async () => { throw new Error("boom"); } } as unknown as EshuApiClient;
    expect(await resolveEntityName(errClient, "checkout")).toBe("checkout");
  });
});
