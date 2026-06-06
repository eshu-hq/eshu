import { describe, expect, it } from "vitest";
import { relationshipsToGraph, loadEntityGraph, loadEntityMapGraph, loadBlastGraph, resolveEntityName, codeRelationshipsToGraph, entityMapToGraph } from "./eshuGraph";
import type { EshuApiClient } from "./client";

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
            target: "api-node-boats",
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

    const graph = await loadBlastGraph(client, "api-node-boats");
    expect(calledPath).toBe("/api/v0/impact/blast-radius");
    expect(body).toMatchObject({ target: "api-node-boats", target_type: "repository" });
    const hero = graph.nodes.find((n) => n.hero);
    expect(hero?.label).toBe("api-node-boats");
    // Only the origin + the one real dependent; placeholder/empty rows dropped.
    expect(graph.nodes.map((n) => n.label).sort()).toEqual(["api-node-boats", "lib-common"]);
    expect(graph.edges).toHaveLength(1);
    expect(graph.edges[0]).toMatchObject({ s: "lib-common", t: hero?.id, verb: "DEPENDS_ON" });
  });

  it("loadBlastGraph renders the origin alone when there are no real dependents", async () => {
    const client = {
      post: async () => ({
        data: { target: "api-node-boats", affected: [{ repo: "DISTINCT affected.name" }] },
        error: null,
        truth: null
      })
    } as unknown as EshuApiClient;
    const graph = await loadBlastGraph(client, "api-node-boats");
    expect(graph.nodes).toHaveLength(1);
    expect(graph.nodes[0]?.hero).toBe(true);
    expect(graph.edges).toHaveLength(0);
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
    expect(body).toMatchObject({ entity_id: "content-entity:e_center" });
    expect(graph.nodes.find((n) => n.hero)?.label).toBe("createNewVersion");
    expect(graph.edges).toHaveLength(1);
  });

  it("entityMapToGraph centers on the resolved candidate and maps evidence.relationships by direction", () => {
    const graph = entityMapToGraph({
      from: "api-node-boats",
      resolution: { candidates: [{ id: "workload:api-node-boats", name: "api-node-boats", labels: ["Workload"] }] },
      evidence: { relationships: [
        { entity_id: "repository:r_f9600c28", entity_name: "api-node-boats", entity_labels: ["Repository"], direction: "incoming", relationship_type: "DEFINES" },
        // no relationship_type (singular) — must fall back to relationship_types[0]
        { entity_id: "workload:payments", entity_name: "payments", entity_labels: ["Workload"], direction: "outgoing", relationship_types: ["DEPENDS_ON"] }
      ] }
    }, "api-node-boats");
    const hero = graph.nodes.find((n) => n.hero);
    expect(hero?.id).toBe("workload:api-node-boats");
    // nodes are keyed by entity_id, not the display name
    expect(graph.nodes.some((n) => n.id === "workload:payments" && n.label === "payments")).toBe(true);
    expect(graph.edges.find((e) => e.verb === "DEFINES")).toMatchObject({ s: "repository:r_f9600c28", t: "workload:api-node-boats" });
    expect(graph.edges.find((e) => e.verb === "DEPENDS_ON")).toMatchObject({ s: "workload:api-node-boats", t: "workload:payments", layer: "runtime" });
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

  it("resolveEntityName returns the top candidate, falling back to the raw query", async () => {
    const withMatch = { postJson: async () => ({ entities: [{ id: "svc:checkout", name: "checkout-service", type: "service", repo_id: "r", repo_name: "r", file_path: "", labels: [] }] }) } as unknown as EshuApiClient;
    expect(await resolveEntityName(withMatch, "checkout")).toBe("checkout-service");

    const noMatch = { postJson: async () => ({ entities: [] }) } as unknown as EshuApiClient;
    expect(await resolveEntityName(noMatch, "checkout")).toBe("checkout");

    const errClient = { postJson: async () => { throw new Error("boom"); } } as unknown as EshuApiClient;
    expect(await resolveEntityName(errClient, "checkout")).toBe("checkout");
  });
});
