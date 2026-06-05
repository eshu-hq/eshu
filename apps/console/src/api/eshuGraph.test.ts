import { describe, expect, it } from "vitest";
import { relationshipsToGraph, loadEntityMapGraph, resolveEntityName } from "./eshuGraph";
import type { EshuApiClient } from "./client";

describe("eshuGraph", () => {
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

  it("loadEntityMapGraph posts to impact/entity-map", async () => {
    let calledPath = "";
    const client = {
      post: async (path: string) => { calledPath = path; return { data: { target: { id: "e", name: "e" }, relationships: [] }, error: null, truth: null }; }
    } as unknown as EshuApiClient;
    await loadEntityMapGraph(client, "e");
    expect(calledPath).toBe("/api/v0/impact/entity-map");
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
