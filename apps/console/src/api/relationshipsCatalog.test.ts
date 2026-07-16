import { describe, expect, it, vi } from "vitest";

import { EshuApiClient, type EshuFetcher } from "./client";
import { loadRelationshipsCatalog } from "./relationshipsCatalog";

interface Deferred<T> {
  readonly promise: Promise<T>;
  readonly reject: (reason?: unknown) => void;
  readonly resolve: (value: T | PromiseLike<T>) => void;
}

function deferred<T>(): Deferred<T> {
  let reject: Deferred<T>["reject"] = () => undefined;
  let resolve: Deferred<T>["resolve"] = () => undefined;
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    reject = promiseReject;
    resolve = promiseResolve;
  });
  return { promise, reject, resolve };
}

function catalogResponse(totalEdges: number): Response {
  return Response.json({
    data: {
      layer_count: 1,
      total_edges: totalEdges,
      verb_count: 1,
      verbs: [
        {
          count: totalEdges,
          detail: "observed edges",
          evidence: "graph",
          layer: "code",
          verb: "CALLS",
        },
      ],
    },
    error: null,
    truth: null,
  });
}

describe("loadRelationshipsCatalog", () => {
  it("coalesces only concurrent loads for one client and fetches fresh data after completion", async () => {
    const firstResponse = deferred<Response>();
    const fetcher = vi
      .fn<EshuFetcher>()
      .mockImplementationOnce(() => firstResponse.promise)
      .mockResolvedValueOnce(catalogResponse(9));
    const client = new EshuApiClient({ baseUrl: "/eshu-api/", fetcher });

    const first = loadRelationshipsCatalog(client);
    const replay = loadRelationshipsCatalog(client);

    expect(fetcher).toHaveBeenCalledTimes(1);
    firstResponse.resolve(catalogResponse(7));
    const [firstResult, replayResult] = await Promise.all([first, replay]);
    expect(replayResult).toBe(firstResult);
    expect(firstResult.totalEdges).toBe(7);

    const laterResult = await loadRelationshipsCatalog(client);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(laterResult.totalEdges).toBe(9);
    expect(laterResult).not.toBe(firstResult);
  });

  it("evicts a rejected load so concurrent callers share the error and a retry stays available", async () => {
    const failedResponse = deferred<Response>();
    const fetcher = vi
      .fn<EshuFetcher>()
      .mockImplementationOnce(() => failedResponse.promise)
      .mockResolvedValueOnce(catalogResponse(11));
    const client = new EshuApiClient({ baseUrl: "/eshu-api/", fetcher });
    const failure = new Error("catalog unavailable");

    const first = loadRelationshipsCatalog(client);
    const replay = loadRelationshipsCatalog(client);

    expect(fetcher).toHaveBeenCalledTimes(1);
    failedResponse.reject(failure);
    const [firstResult, replayResult] = await Promise.allSettled([first, replay]);
    expect(firstResult).toEqual({ status: "rejected", reason: failure });
    expect(replayResult).toEqual({ status: "rejected", reason: failure });

    await expect(loadRelationshipsCatalog(client)).resolves.toMatchObject({ totalEdges: 11 });
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("does not coalesce requests from different clients", async () => {
    const fetcher = vi.fn<EshuFetcher>().mockImplementation(async () => catalogResponse(5));
    const firstClient = new EshuApiClient({ baseUrl: "/eshu-api/", fetcher });
    const secondClient = new EshuApiClient({ baseUrl: "/eshu-api/", fetcher });

    await Promise.all([
      loadRelationshipsCatalog(firstClient),
      loadRelationshipsCatalog(secondClient),
    ]);

    expect(fetcher).toHaveBeenCalledTimes(2);
  });
});
