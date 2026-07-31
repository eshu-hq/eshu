import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";

export function codeGraphRepository(id: string, name: string): RepoListItem {
  return {
    groupKey: "source",
    groupKind: "source",
    groupReason: "fixture",
    groupSource: "fixture",
    groupTruth: "exact",
    id,
    isDependency: false,
    name,
    remoteUrl: "",
    repoSlug: `platform/${name}`,
  };
}

export interface Deferred<T> {
  readonly promise: Promise<T>;
  readonly reject: (reason?: unknown) => void;
  readonly resolve: (value: T) => void;
}

export function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, reject, resolve };
}

export function codeGraphInventory(repoId: string, name: string): unknown {
  return {
    data: {
      results: [
        {
          entity_id: `content-entity:${repoId}:${name}`,
          entity_name: name,
          entity_type: "Function",
          file_path: `src/${name}.ts`,
          repo_id: repoId,
        },
      ],
      truncated: false,
    },
    error: null,
    truth: null,
  };
}

export function clientWithCodeGraphInventory(
  loadInventory: (repoId: string) => Promise<unknown>,
  rejectedEntityId = "",
  rejectionLimit = Number.POSITIVE_INFINITY,
): EshuApiClient {
  let storyRejections = 0;
  return {
    post: async (path: string, body: unknown) => {
      if (path === "/api/v0/code/structure/inventory") {
        return loadInventory(String((body as { readonly repo_id?: string }).repo_id));
      }
      if (path === "/api/v0/code/relationships/story") {
        const entityId = String((body as { readonly entity_id?: string }).entity_id);
        const repoId = String((body as { readonly repo_id?: string }).repo_id);
        if (entityId === rejectedEntityId && storyRejections < rejectionLimit) {
          storyRejections += 1;
          return {
            data: { relationships: [], target_resolution: { status: "not_found" } },
            error: null,
            truth: null,
          };
        }
        return {
          data: {
            entity_id: entityId,
            labels: ["Function"],
            name: entityId,
            relationships: [],
            scope: { repo_id: repoId },
            target_resolution: { entity_id: entityId, repo_id: repoId, status: "resolved" },
          },
          error: null,
          truth: null,
        };
      }
      if (path === "/api/v0/code/imports/investigate") {
        return { data: { cycles: [], truncated: false }, error: null, truth: null };
      }
      throw new Error(`unexpected request: ${path}`);
    },
  } as unknown as EshuApiClient;
}
