import type { EshuApiClient } from "./client";
import {
  loadCodeGraph,
  loadCodeGraphInventory,
  type CodeGraphRelationshipStory,
} from "./codeGraphLoader";
import type { EshuEnvelope } from "./envelope";
import type { CodeRelationshipStoryResponse } from "./eshuGraph";

const target = {
  entityId: "content-entity:e1",
  id: "dead-1",
  name: "unusedRoute",
};

function storyEnvelope(): EshuEnvelope<CodeRelationshipStoryResponse> {
  return {
    data: {
      entity_id: target.entityId,
      labels: ["Function"],
      name: target.name,
      relationships: [
        {
          direction: "incoming",
          provenance: {
            confidence_tier: "high",
            truth_state: "derived",
          },
          source_id: "content-entity:caller",
          source_name: "caller",
          type: "CALLS",
        },
      ],
    },
    error: null,
    truth: null,
  };
}

function scopedStoryEnvelope(repoId: string): EshuEnvelope<CodeGraphRelationshipStory> {
  const envelope = storyEnvelope();
  return {
    ...envelope,
    data: {
      ...envelope.data,
      scope: { repo_id: repoId },
      target_resolution: {
        entity_id: target.entityId,
        repo_id: repoId,
        status: "resolved",
      },
    },
  };
}

describe("loadCodeGraph", () => {
  it("scopes an exact entity story to its selected repository", async () => {
    const calls: unknown[] = [];
    const client = {
      post: async (_path: string, body: unknown) => {
        calls.push(body);
        return scopedStoryEnvelope("repository:r1");
      },
    } as unknown as EshuApiClient;

    await loadCodeGraph(client, { ...target, repoId: "repository:r1" });

    expect(calls).toContainEqual({
      direction: "both",
      entity_id: "content-entity:e1",
      limit: 50,
      relationship_types: [
        "CALLS",
        "IMPORTS",
        "REFERENCES",
        "INHERITS",
        "OVERRIDES",
        "TAINT_FLOWS_TO",
      ],
      repo_id: "repository:r1",
    });
  });

  it("rejects unresolved and cross-repository relationship-story targets", async () => {
    const unresolvedClient = {
      post: async () => ({
        data: {
          relationships: [],
          target_resolution: { status: "not_found" },
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;
    await expect(
      loadCodeGraph(unresolvedClient, { ...target, repoId: "repository:r1" }),
    ).rejects.toThrow("not_found in the selected repository");

    const crossRepositoryClient = {
      post: async () => ({
        data: {
          relationships: [],
          target_resolution: {
            entity_id: target.entityId,
            repo_id: "repository:other",
            status: "resolved",
          },
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;
    await expect(
      loadCodeGraph(crossRepositoryClient, { ...target, repoId: "repository:r1" }),
    ).rejects.toThrow("outside the selected repository");

    const crossRepositoryScopeClient = {
      post: async () => ({
        data: {
          relationships: [],
          scope: { repo_id: "repository:other" },
          target_resolution: {
            entity_id: target.entityId,
            repo_id: "repository:r1",
            status: "resolved",
          },
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;
    await expect(
      loadCodeGraph(crossRepositoryScopeClient, { ...target, repoId: "repository:r1" }),
    ).rejects.toThrow("outside the selected repository");

    const missingResolutionRepoClient = {
      post: async () => ({
        ...scopedStoryEnvelope("repository:r1"),
        data: {
          ...scopedStoryEnvelope("repository:r1").data,
          target_resolution: { entity_id: target.entityId, status: "resolved" },
        },
      }),
    } as unknown as EshuApiClient;
    await expect(
      loadCodeGraph(missingResolutionRepoClient, { ...target, repoId: "repository:r1" }),
    ).rejects.toThrow("did not prove selected repository ownership");

    const missingScopeRepoClient = {
      post: async () => ({
        ...scopedStoryEnvelope("repository:r1"),
        data: { ...scopedStoryEnvelope("repository:r1").data, scope: {} },
      }),
    } as unknown as EshuApiClient;
    await expect(
      loadCodeGraph(missingScopeRepoClient, { ...target, repoId: "repository:r1" }),
    ).rejects.toThrow("did not prove selected repository ownership");
  });

  it("coalesces only concurrent identical story requests and evicts them on settle", async () => {
    let resolveStory: ((value: EshuEnvelope<CodeRelationshipStoryResponse>) => void) | undefined;
    let storyCalls = 0;
    let sourceCalls = 0;
    const client = {
      post: async (path: string) => {
        if (path === "/api/v0/code/relationships/story") {
          storyCalls += 1;
          return new Promise<EshuEnvelope<CodeRelationshipStoryResponse>>((resolve) => {
            resolveStory = resolve;
          });
        }
        sourceCalls += 1;
        return { data: {}, error: null, truth: null };
      },
    } as unknown as EshuApiClient;

    const first = loadCodeGraph(client, target);
    const second = loadCodeGraph(client, target);

    expect(storyCalls).toBe(1);
    expect(sourceCalls).toBe(0);
    expect(resolveStory).toBeDefined();
    resolveStory?.(storyEnvelope());
    await Promise.all([first, second]);

    const third = loadCodeGraph(client, target);
    expect(storyCalls).toBe(2);
    resolveStory?.(storyEnvelope());
    await third;
  });

  it("does not coalesce different story keys or different clients", async () => {
    let storyCalls = 0;
    const client = {
      post: async (path: string) => {
        if (path === "/api/v0/code/relationships/story") storyCalls += 1;
        return storyEnvelope();
      },
    } as unknown as EshuApiClient;
    const otherClient = {
      post: async (path: string) => {
        if (path === "/api/v0/code/relationships/story") storyCalls += 1;
        return storyEnvelope();
      },
    } as unknown as EshuApiClient;

    await Promise.all([
      loadCodeGraph(client, target),
      loadCodeGraph(client, { ...target, entityId: "content-entity:e2" }),
      loadCodeGraph(otherClient, target),
    ]);

    expect(storyCalls).toBe(3);
  });

  it("uses source-backed story rows without issuing a redundant untyped relationship read", async () => {
    let resolveStory: ((value: EshuEnvelope<CodeRelationshipStoryResponse>) => void) | undefined;
    const calls: string[] = [];
    const client = {
      post: async (path: string) => {
        calls.push(path);
        if (path === "/api/v0/code/relationships/story") {
          return new Promise<EshuEnvelope<CodeRelationshipStoryResponse>>((resolve) => {
            resolveStory = resolve;
          });
        }
        throw new Error("untyped relationship read must not be called");
      },
    } as unknown as EshuApiClient;

    const loading = loadCodeGraph(client, target);

    expect(calls).toEqual(["/api/v0/code/relationships/story"]);
    const sourceBackedStory = storyEnvelope();
    resolveStory?.({
      ...sourceBackedStory,
      data: {
        ...sourceBackedStory.data,
        relationships: sourceBackedStory.data?.relationships?.map((relationship) => ({
          ...relationship,
          source_file_path: "src/caller.ts",
          source_repo_id: "repository:r1",
        })),
      },
    });
    const loaded = await loading;

    expect(loaded.graph.nodes.find((node) => node.id === "content-entity:caller")).toMatchObject({
      source: {
        filePath: "src/caller.ts",
        repoId: "repository:r1",
      },
    });
    expect(loaded.graph.edges[0]).toMatchObject({
      confidenceTier: "high",
      truthState: "derived",
    });
  });

  it("keeps the primary story graph without a standalone source hydration dependency", async () => {
    const client = {
      post: async (path: string) => {
        if (path === "/api/v0/code/relationships/story") return storyEnvelope();
        throw new Error("unexpected standalone relationship read");
      },
    } as unknown as EshuApiClient;

    const loaded = await loadCodeGraph(client, target);

    expect(loaded.graph.nodes.map((node) => node.id)).toEqual([
      target.entityId,
      "content-entity:caller",
    ]);
    expect(loaded.coverage).toBeUndefined();
  });
});

describe("loadCodeGraphInventory", () => {
  it("normalizes a bounded repository-scoped entity inventory", async () => {
    const calls: { readonly body: unknown; readonly path: string }[] = [];
    const client = {
      post: async (path: string, body: unknown) => {
        calls.push({ body, path });
        return {
          data: {
            next_offset: 100,
            results: [
              {
                end_line: 24,
                entity_id: "content-entity:e1",
                entity_name: "entrypoint",
                entity_type: "Function",
                file_path: "src/entry.ts",
                language: "typescript",
                repo_id: "repository:r1",
                start_line: 12,
              },
              { entity_id: "", entity_name: "invalid" },
            ],
            truncated: true,
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    const inventory = await loadCodeGraphInventory(client, "repository:r1", "service-one");

    expect(calls).toEqual([
      {
        body: { inventory_kind: "entity", limit: 100, repo_id: "repository:r1" },
        path: "/api/v0/code/structure/inventory",
      },
    ]);
    expect(inventory).toEqual({
      nextOffset: 100,
      symbols: [
        {
          classification: "Function",
          detail: "src/entry.ts",
          endLine: 24,
          entity: "service-one",
          entityId: "content-entity:e1",
          filePath: "src/entry.ts",
          id: "content-entity:e1",
          language: "typescript",
          repoId: "repository:r1",
          startLine: 12,
          title: "entrypoint",
          truth: "derived",
          type: "Code symbol",
        },
      ],
      truncated: true,
    });
  });

  it("rejects cross-repository and duplicate inventory identities", async () => {
    const crossRepositoryClient = {
      post: async () => ({
        data: {
          results: [
            {
              entity_id: "content-entity:e1",
              entity_name: "entrypoint",
              repo_id: "repository:other",
            },
          ],
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;
    await expect(
      loadCodeGraphInventory(crossRepositoryClient, "repository:r1", "service-one"),
    ).rejects.toThrow("cross-repository");

    const duplicateClient = {
      post: async () => ({
        data: {
          results: [
            { entity_id: "content-entity:e1", entity_name: "first", repo_id: "repository:r1" },
            { entity_id: "content-entity:e1", entity_name: "second", repo_id: "repository:r1" },
          ],
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;
    await expect(
      loadCodeGraphInventory(duplicateClient, "repository:r1", "service-one"),
    ).rejects.toThrow("duplicate entity identity");
  });
});
