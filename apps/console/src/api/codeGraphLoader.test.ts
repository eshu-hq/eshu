import type { EshuApiClient } from "./client";
import { loadCodeGraph } from "./codeGraphLoader";
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

describe("loadCodeGraph", () => {
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
