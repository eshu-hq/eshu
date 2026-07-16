import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { EshuApiHttpError, type EshuApiClient } from "./api/client";
import type { RepoListItem } from "./api/repoCatalog";
import {
  loadRepositoryCatalogState,
  loadingRepositoryCatalog,
  readyRepositoryCatalog,
  type RepositoryCatalogState,
  useRepositoryCatalogLifecycle,
} from "./repositoryCatalogLifecycle";

const repository: RepoListItem = {
  groupKey: "Platform",
  groupKind: "source",
  groupReason: "derived from repository slug namespace",
  groupSource: "repo_slug_namespace",
  groupTruth: "derived",
  id: "repository:checkout-api",
  isDependency: false,
  name: "checkout-api",
  remoteUrl: "",
  repoSlug: "platform/checkout-api",
};

describe("repository catalog lifecycle", () => {
  it("distinguishes authoritative empty and scoped populated catalogs", async () => {
    const empty = await loadRepositoryCatalogState(clientReturning([]));
    const populated = await loadRepositoryCatalogState(clientReturning([wireRepository()]));

    expect(empty).toMatchObject({ kind: "ready", completeness: "complete", repositories: [] });
    expect(populated).toMatchObject({
      kind: "ready",
      completeness: "complete",
      repositories: [{ id: repository.id, name: repository.name }],
    });
  });

  it("surfaces server truncation instead of presenting a partial catalog as complete", async () => {
    let calls = 0;
    const firstPage = Array.from({ length: 500 }, (_, index) => ({
      ...wireRepository(),
      id: `repository:r-${index}`,
      name: `repo-${index}`,
    }));
    const client = {
      get: async () => {
        calls += 1;
        return {
          data: {
            offset: 0,
            repositories: firstPage,
            truncated: true,
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    const state = await loadRepositoryCatalogState(client);

    expect(calls).toBe(2);
    expect(state.kind).toBe("ready");
    if (state.kind !== "ready") throw new Error("expected ready repository catalog");
    expect(state.completeness).toBe("truncated");
    expect(state.repositories).toHaveLength(500);
    expect(state.warning).toContain("offset clamped");
  });

  it("does not label a contradictory empty truncated page as complete", async () => {
    const client = {
      get: async () => ({
        data: { offset: 0, repositories: [], truncated: true },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;

    const state = await loadRepositoryCatalogState(client);

    expect(state).toMatchObject({
      kind: "ready",
      completeness: "truncated",
      repositories: [],
    });
    expect(state.kind === "ready" ? state.warning : "").toContain(
      "empty page while the server still reports truncation",
    );
  });

  it.each([
    ["unauthorized", new EshuApiHttpError(401)],
    ["timeout", new DOMException("request timed out", "TimeoutError")],
    ["backend unavailable", new Error("backend unavailable")],
  ])("keeps %s distinct from an authoritative empty catalog", async (_name, failure) => {
    const client = {
      get: async () => {
        throw failure;
      },
    } as unknown as EshuApiClient;

    const state = await loadRepositoryCatalogState(client);

    expect(state.kind).toBe("unavailable");
    expect(state.repositories).toEqual([]);
  });

  it("does not expose a partial first page after a later page fails", async () => {
    let calls = 0;
    const client = {
      get: async () => {
        calls += 1;
        if (calls === 1) {
          return {
            data: {
              offset: 0,
              repositories: Array.from({ length: 500 }, wireRepository),
              truncated: true,
            },
            error: null,
            truth: null,
          };
        }
        throw new Error("page two unavailable");
      },
    } as unknown as EshuApiClient;

    const state = await loadRepositoryCatalogState(client);

    expect(state).toMatchObject({ kind: "unavailable", repositories: [] });
  });

  it("ignores a stale catalog result after the authenticated client changes", async () => {
    const first = deferred<RepositoryCatalogState>();
    const second = deferred<RepositoryCatalogState>();
    const firstClient = {} as EshuApiClient;
    const secondClient = {} as EshuApiClient;
    const { result } = renderHook(() => useRepositoryCatalogLifecycle(loadingRepositoryCatalog));

    act(() => result.current.activate(firstClient, first.promise));
    act(() => result.current.activate(secondClient, second.promise));
    await act(async () => second.resolve(readyRepositoryCatalog([repository])));
    await waitFor(() => expect(result.current.state.repositories).toEqual([repository]));
    await act(async () =>
      first.resolve(readyRepositoryCatalog([{ ...repository, name: "stale" }])),
    );

    expect(result.current.state.repositories).toEqual([repository]);
  });
});

function clientReturning(repositories: readonly unknown[]): EshuApiClient {
  return {
    get: async () => ({
      data: { offset: 0, repositories, truncated: false },
      error: null,
      truth: null,
    }),
  } as unknown as EshuApiClient;
}

function wireRepository(): Record<string, unknown> {
  return {
    group_key: repository.groupKey,
    group_kind: repository.groupKind,
    group_reason: repository.groupReason,
    group_source: repository.groupSource,
    group_truth: repository.groupTruth,
    id: repository.id,
    is_dependency: repository.isDependency,
    name: repository.name,
    repo_slug: repository.repoSlug,
  };
}

function deferred<T>(): {
  readonly promise: Promise<T>;
  readonly resolve: (value: T) => void;
} {
  let resolvePromise: ((value: T) => void) | undefined;
  const promise = new Promise<T>((resolve) => {
    resolvePromise = resolve;
  });
  return {
    promise,
    resolve: (value: T): void => resolvePromise?.(value),
  };
}
