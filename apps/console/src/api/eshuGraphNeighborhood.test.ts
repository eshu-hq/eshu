import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { entityMapToGraph, loadEntityMapGraph } from "./eshuGraph";

describe("eshuGraph neighborhood", () => {
  it("entityMapToGraph centers on the resolved candidate and maps evidence.relationships by direction", () => {
    const graph = entityMapToGraph(
      {
        from: "catalog-api",
        resolution: {
          candidates: [{ id: "workload:catalog-api", name: "catalog-api", labels: ["Workload"] }],
        },
        evidence: {
          relationships: [
            {
              entity_id: "repository:r_f9600c28",
              entity_name: "catalog-api",
              entity_labels: ["Repository"],
              direction: "incoming",
              relationship_type: "DEFINES",
              relationship_source: "graph",
              repo_id: "repository:r_f9600c28",
              depth: 1,
            },
            {
              entity_id: "workload:payments",
              entity_name: "payments",
              entity_labels: ["Workload"],
              direction: "outgoing",
              relationship_types: ["DEPENDS_ON"],
              environment: "acme-prod",
            },
          ],
        },
      },
      "catalog-api",
    );
    const hero = graph.nodes.find((node) => node.hero);
    expect(hero?.id).toBe("workload:catalog-api");
    expect(
      graph.nodes.some((node) => node.id === "workload:payments" && node.label === "payments"),
    ).toBe(true);
    expect(graph.edges.find((edge) => edge.verb === "DEFINES")).toMatchObject({
      s: "repository:r_f9600c28",
      t: "workload:catalog-api",
      evidence: [
        "relationship source: graph",
        "direction: incoming",
        "entity labels: Repository",
        "repo: repository:r_f9600c28",
        "depth: 1",
      ],
    });
    expect(graph.edges.find((edge) => edge.verb === "DEPENDS_ON")).toMatchObject({
      s: "workload:catalog-api",
      t: "workload:payments",
      layer: "runtime",
      evidence: [
        "relationship source: graph",
        "direction: outgoing",
        "entity labels: Workload",
        "environment: acme-prod",
      ],
    });
  });

  it("loadEntityMapGraph posts impact/entity-map with from and parses evidence.relationships", async () => {
    let calledPath = "";
    let body: unknown = null;
    const client = {
      post: async (path: string, requestBody: unknown) => {
        calledPath = path;
        body = requestBody;
        return {
          data: {
            from: "checkout",
            resolution: {
              candidates: [{ id: "workload:checkout", name: "checkout", labels: ["Workload"] }],
            },
            evidence: {
              relationships: [
                {
                  entity_name: "payments",
                  entity_labels: ["Workload"],
                  direction: "outgoing",
                  relationship_type: "DEPENDS_ON",
                },
              ],
            },
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;
    const graph = await loadEntityMapGraph(client, "checkout");
    expect(calledPath).toBe("/api/v0/impact/entity-map");
    expect(body).toMatchObject({ from: "checkout", depth: 2 });
    expect(body).not.toHaveProperty("max_depth");
    expect(graph.nodes.find((node) => node.hero)?.id).toBe("workload:checkout");
    expect(graph.edges).toHaveLength(1);
  });

  it("loadEntityMapGraph rejects API error envelopes instead of rendering a fallback node", async () => {
    const client = {
      post: async () => ({
        data: null,
        error: {
          code: "unsupported_capability",
          message: "entity map is unavailable",
        },
        truth: null,
      }),
    } as unknown as EshuApiClient;

    await expect(loadEntityMapGraph(client, "checkout")).rejects.toThrow(
      "unsupported_capability: entity map is unavailable",
    );
  });
});
