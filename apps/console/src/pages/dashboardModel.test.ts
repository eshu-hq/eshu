import { describe, expect, it } from "vitest";

import { liveAtlasSeeds, selectSeedGraph } from "./dashboardModel";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";
import type { GraphNode } from "../console/types";

describe("liveAtlasSeeds", () => {
  it("does not promote a repository name into a canonical repository identity", () => {
    const repositories: readonly RepoListItem[] = [
      {
        groupKey: "",
        groupKind: "",
        groupReason: "",
        groupSource: "",
        groupTruth: "",
        id: "",
        isDependency: false,
        name: "platform",
        remoteUrl: "",
        repoSlug: "eshu/platform",
      },
    ];

    expect(liveAtlasSeeds(modelFromSnapshot(emptySnapshot("live")), repositories)).toEqual([]);
  });
});

describe("selectSeedGraph", () => {
  it("scopes catalog service seeds to the canonical workload resolver", async () => {
    const resolveBodies: unknown[] = [];
    const entityMapBodies: unknown[] = [];
    const client = seedClient(resolveBodies, entityMapBodies, [
      {
        entity_id: "workload:checkout",
        labels: ["Workload"],
        name: "checkout",
      },
    ]);

    await selectSeedGraph(client, [seedNode("svc-checkout", "service", "checkout")], () => false);

    expect(resolveBodies).toEqual([{ limit: 1, name: "checkout", type: "workload" }]);
    expect(entityMapBodies).toEqual([
      { depth: 2, from: "workload:checkout", from_type: "workload" },
    ]);
  });

  it("scopes repository seeds with their canonical repository identity", async () => {
    const resolveBodies: unknown[] = [];
    const entityMapBodies: unknown[] = [];
    const client = seedClient(resolveBodies, entityMapBodies, [
      {
        id: "repository:r_platform",
        labels: ["Repository"],
        name: "platform",
        repo_id: "repository:r_platform",
      },
    ]);

    await selectSeedGraph(
      client,
      [seedNode("repository:r_platform", "repo", "platform")],
      () => false,
    );

    expect(resolveBodies).toEqual([
      {
        limit: 1,
        name: "platform",
        repo_id: "repository:r_platform",
        type: "repository",
      },
    ]);
    expect(entityMapBodies).toEqual([
      {
        depth: 2,
        from: "repository:r_platform",
        from_type: "repository",
        repo_id: "repository:r_platform",
      },
    ]);
  });

  it("does not issue requests for empty or unsupported seed shapes", async () => {
    const resolveBodies: unknown[] = [];
    const entityMapBodies: unknown[] = [];
    const client = seedClient(resolveBodies, entityMapBodies);

    const selected = await selectSeedGraph(
      client,
      [seedNode("", "service", ""), seedNode("mystery:one", "mystery", "mystery")],
      () => false,
    );

    expect(selected).toBeUndefined();
    expect(resolveBodies).toEqual([]);
    expect(entityMapBodies).toEqual([]);
  });
});

function seedClient(
  resolveBodies: unknown[],
  entityMapBodies: unknown[] = [],
  entities: readonly unknown[] = [],
): EshuApiClient {
  return {
    postJson: async (_path: string, body: unknown) => {
      resolveBodies.push(body);
      return { entities };
    },
    post: async (_path: string, body: unknown) => {
      entityMapBodies.push(body);
      return {
        data: {
          evidence: { relationships: [] },
          from: requestString(body, "from"),
          resolution: { candidates: [] },
        },
        error: null,
        truth: null,
      };
    },
  } as unknown as EshuApiClient;
}

function seedNode(id: string, kind: string, label: string): GraphNode {
  return { col: 0, hero: true, id, kind, label, truth: "exact" };
}

function requestString(body: unknown, key: string): string {
  if (typeof body !== "object" || body === null || !(key in body)) return "";
  const value = body[key as keyof typeof body];
  return typeof value === "string" ? value : "";
}
