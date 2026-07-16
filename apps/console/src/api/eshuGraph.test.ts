import { describe, expect, it } from "vitest";

import { EshuApiHttpError } from "./client";
import type { EshuApiClient } from "./client";
import {
  relationshipsToGraph,
  loadEntityGraph,
  loadBlastGraph,
  resolveEntityName,
  codeRelationshipsToGraph,
  recommendedModeForKind,
} from "./eshuGraph";

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
              { repo: "DISTINCT affected.name" },
              { repo: "" },
            ],
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    const graph = await loadBlastGraph(client, "catalog-api");
    expect(calledPath).toBe("/api/v0/impact/blast-radius");
    expect(body).toMatchObject({ target: "catalog-api", target_type: "repository" });
    const hero = graph.nodes.find((node) => node.hero);
    expect(hero?.label).toBe("catalog-api");
    expect(graph.nodes.map((node) => node.label).sort()).toEqual(["catalog-api", "lib-common"]);
    expect(graph.edges).toHaveLength(1);
    expect(graph.edges[0]).toMatchObject({ s: "lib-common", t: hero?.id, verb: "DEPENDS_ON" });
  });

  it("loadBlastGraph renders the origin alone when there are no real dependents", async () => {
    const client = {
      post: async () => ({
        data: { target: "catalog-api", affected: [{ repo: "DISTINCT affected.name" }] },
        error: null,
        truth: null,
      }),
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
          message: "blast radius unavailable",
        },
        truth: null,
      }),
    } as unknown as EshuApiClient;

    await expect(loadBlastGraph(client, "catalog-api")).rejects.toThrow("unsupported_capability");
  });

  it("relationshipsToGraph builds a hero center and maps verbs to layers + direction", () => {
    const graph = relationshipsToGraph(
      {
        target: { id: "svc:checkout", name: "checkout", type: "service" },
        relationships: [
          { verb: "DEPENDS_ON", target: { id: "svc:payments", name: "payments", type: "service" } },
          {
            verb: "CALLS",
            direction: "incoming",
            target: { id: "fn:caller", name: "caller", type: "function" },
          },
        ],
      },
      "checkout",
    );

    const hero = graph.nodes.find((node) => node.hero);
    expect(hero?.id).toBe("svc:checkout");
    expect(graph.nodes).toHaveLength(3);
    const dep = graph.edges.find((edge) => edge.verb === "DEPENDS_ON");
    expect(dep).toMatchObject({ s: "svc:checkout", t: "svc:payments", layer: "runtime" });
    const incoming = graph.edges.find((edge) => edge.verb === "CALLS");
    expect(incoming).toMatchObject({ s: "fn:caller", t: "svc:checkout", layer: "code" });
  });

  it("codeRelationshipsToGraph maps incoming/outgoing edges around the center", () => {
    const graph = codeRelationshipsToGraph(
      {
        entity_id: "content-entity:e_center",
        name: "createNewVersion",
        labels: ["Function"],
        incoming: [{ type: "CALLS", source_id: "content-entity:e_main", source_name: "main" }],
        outgoing: [{ type: "CALLS", target_id: "content-entity:e_dep", target_name: "listFiles" }],
      },
      { id: "content-entity:e_center", name: "createNewVersion" },
    );
    const hero = graph.nodes.find((node) => node.hero);
    expect(hero?.id).toBe("content-entity:e_center");
    expect(graph.nodes).toHaveLength(3);
    expect(graph.edges.find((edge) => edge.s === "content-entity:e_main")).toMatchObject({
      t: "content-entity:e_center",
      verb: "CALLS",
      layer: "code",
    });
    expect(graph.edges.find((edge) => edge.t === "content-entity:e_dep")).toMatchObject({
      s: "content-entity:e_center",
      verb: "CALLS",
      layer: "code",
    });
  });

  it("codeRelationshipsToGraph classifies related code symbols from relationship direction", () => {
    const graph = codeRelationshipsToGraph(
      {
        entity_id: "content-entity:e_center",
        name: "handler",
        labels: ["Function"],
        incoming: [{ type: "CALLS", source_id: "content-entity:e_route", source_name: "route" }],
        outgoing: [
          { type: "CALLS", target_id: "content-entity:e_callee", target_name: "loadProfile" },
          { type: "IMPORTS", target_id: "content-entity:e_pkg", target_name: "apiClient" },
        ],
      },
      { id: "content-entity:e_center", name: "handler" },
    );

    expect(graph.nodes.find((node) => node.id === "content-entity:e_route")).toMatchObject({
      kind: "client",
      sub: "incoming CALLS",
    });
    expect(graph.nodes.find((node) => node.id === "content-entity:e_callee")).toMatchObject({
      kind: "client",
      sub: "outgoing CALLS",
    });
    expect(graph.nodes.find((node) => node.id === "content-entity:e_pkg")).toMatchObject({
      kind: "library",
      sub: "outgoing IMPORTS",
    });
  });

  it("codeRelationshipsToGraph keeps source metadata for the centered code entity", () => {
    const graph = codeRelationshipsToGraph(
      {
        entity_id: "content-entity:e_center",
        name: "searchByPortalId",
        labels: ["Function"],
        repo_id: "repository:r_platform",
        repo_name: "svc-platform",
        file_path: "server/resources/listing/index.js",
        start_line: 1653,
        end_line: 1662,
        incoming: [],
        outgoing: [],
      },
      { id: "content-entity:e_center", name: "searchByPortalId" },
    );

    expect(graph.nodes.find((node) => node.hero)?.source).toEqual({
      repoId: "repository:r_platform",
      repoName: "svc-platform",
      filePath: "server/resources/listing/index.js",
      startLine: 1653,
      endLine: 1662,
    });
  });

  it("loadEntityGraph resolves the query to an entity_id, then posts code/relationships by entity_id", async () => {
    let calledPath = "";
    let body: unknown = null;
    const client = {
      postJson: async () => ({
        entities: [
          { id: "content-entity:e_center", name: "createNewVersion", labels: ["Function"] },
        ],
      }),
      post: async (path: string, requestBody: unknown) => {
        calledPath = path;
        body = requestBody;
        return {
          data: {
            entity_id: "content-entity:e_center",
            name: "createNewVersion",
            labels: ["Function"],
            incoming: [{ type: "CALLS", source_id: "content-entity:e_main", source_name: "main" }],
            outgoing: [],
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;
    const graph = await loadEntityGraph(client, "createNewVersion");
    expect(calledPath).toBe("/api/v0/code/relationships");
    expect(body).toMatchObject({ entity_id: "content-entity:e_center", max_depth: 1 });
    expect(body).not.toMatchObject({ depth: 1 });
    expect(graph.nodes.find((node) => node.hero)?.label).toBe("createNewVersion");
    expect(graph.edges).toHaveLength(1);
  });

  it("loadEntityGraph renders the searched node alone when nothing resolves (no bogus entity_id 404)", async () => {
    let postCalled = false;
    const client = {
      postJson: async () => ({ entities: [] }),
      post: async () => {
        postCalled = true;
        return { data: {}, error: null, truth: null };
      },
    } as unknown as EshuApiClient;
    const graph = await loadEntityGraph(client, "no-such-entity");
    expect(postCalled).toBe(false);
    expect(graph.nodes).toHaveLength(1);
    expect(graph.nodes[0]).toMatchObject({ id: "no-such-entity", hero: true });
    expect(graph.edges).toHaveLength(0);
  });

  it("loadEntityGraph degrades a 404 from code/relationships to an empty graph (center alone)", async () => {
    const client = {
      postJson: async () => ({
        entities: [{ id: "workload:catalog-api", name: "catalog-api", labels: ["Workload"] }],
      }),
      post: async () => {
        throw new EshuApiHttpError(404);
      },
    } as unknown as EshuApiClient;
    const graph = await loadEntityGraph(client, "catalog-api");
    expect(graph.nodes).toHaveLength(1);
    expect(graph.nodes[0]).toMatchObject({ id: "workload:catalog-api", hero: true });
    expect(graph.edges).toHaveLength(0);
  });

  it("loadEntityGraph still surfaces a real server error (500) from code/relationships", async () => {
    const client = {
      postJson: async () => ({
        entities: [
          { id: "content-entity:e_center", name: "createNewVersion", labels: ["Function"] },
        ],
      }),
      post: async () => {
        throw new EshuApiHttpError(500);
      },
    } as unknown as EshuApiClient;
    await expect(loadEntityGraph(client, "createNewVersion")).rejects.toBeInstanceOf(
      EshuApiHttpError,
    );
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
    expect(recommendedModeForKind(undefined)).toBe("direct");
    expect(recommendedModeForKind("")).toBe("direct");
  });

  it("resolveEntityName falls back only for a valid no-match and propagates resolver failures", async () => {
    const withMatch = {
      postJson: async () => ({
        entities: [
          {
            id: "svc:checkout",
            name: "checkout-service",
            type: "service",
            repo_id: "r",
            repo_name: "r",
            file_path: "",
            labels: [],
          },
        ],
      }),
    } as unknown as EshuApiClient;
    expect(await resolveEntityName(withMatch, "checkout")).toBe("checkout-service");

    const noMatch = { postJson: async () => ({ entities: [] }) } as unknown as EshuApiClient;
    expect(await resolveEntityName(noMatch, "checkout")).toBe("checkout");

    const errClient = {
      postJson: async () => {
        throw new Error("boom");
      },
    } as unknown as EshuApiClient;
    await expect(resolveEntityName(errClient, "checkout")).rejects.toThrow("boom");
  });
});
