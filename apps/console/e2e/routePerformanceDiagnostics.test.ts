import { describe, expect, it } from "vitest";

import {
  summarizeRoutePerformance,
  type RecordedRouteRequest,
} from "./routePerformanceDiagnostics.ts";

describe("route performance diagnostics", () => {
  it("reports exact duplicates, transfer size, overlap, ordering, and the slowest request", () => {
    const requests: readonly RecordedRouteRequest[] = [
      {
        sequence: 1,
        method: "GET",
        url: "http://127.0.0.1:5180/eshu-api/api/v0/repositories?limit=500&offset=0",
        status: 200,
        startedAtMs: 5,
        responseReceivedAtMs: 95,
        finishedAtMs: 105,
        transferredBytes: 100,
        failed: false,
      },
      {
        sequence: 2,
        method: "GET",
        url: "http://127.0.0.1:5180/eshu-api/api/v0/repositories?limit=500&offset=0",
        status: 200,
        startedAtMs: 20,
        responseReceivedAtMs: 60,
        finishedAtMs: 70,
        transferredBytes: 120,
        failed: false,
      },
      {
        sequence: 3,
        method: "GET",
        url: "http://127.0.0.1:5180/eshu-api/api/v0/catalog?limit=2000",
        status: 200,
        startedAtMs: 110,
        responseReceivedAtMs: 290,
        finishedAtMs: 310,
        transferredBytes: 80,
        failed: false,
      },
    ];

    const result = summarizeRoutePerformance(requests, {
      firstUsefulContentMs: 150,
      routeReadyMs: 400,
    });

    expect(result).toMatchObject({
      duplicateRequestCount: 1,
      firstUsefulContentMs: 150,
      maxSimultaneousRequests: 2,
      postResponseToFirstUsefulContentMs: 45,
      requestCount: 3,
      routeReadyMs: 400,
      transferredBytes: 300,
    });
    expect(result.duplicateRequests).toEqual([
      {
        count: 2,
        method: "GET",
        pathname: "/eshu-api/api/v0/repositories",
        queryKeys: ["limit", "offset"],
        sequences: [1, 2],
      },
    ]);
    expect(result.slowestRequest).toMatchObject({
      durationMs: 200,
      method: "GET",
      pathname: "/eshu-api/api/v0/catalog",
      sequence: 3,
    });
    expect(result.timeline[0]).toMatchObject({
      downloadMs: 10,
      timeToFirstByteMs: 90,
    });
    expect(result.timeline).toEqual([
      expect.objectContaining({
        overlappingSequences: [],
        predecessorSequence: null,
        sequence: 1,
      }),
      expect.objectContaining({
        overlappingSequences: [1],
        predecessorSequence: null,
        sequence: 2,
      }),
      expect.objectContaining({
        overlappingSequences: [],
        predecessorSequence: 1,
        sequence: 3,
      }),
    ]);
    expect(JSON.stringify(result)).not.toContain("offset=0");
    expect(JSON.stringify(result)).not.toContain("127.0.0.1");
  });

  it("returns an empty diagnostic packet when the route issued no API request", () => {
    expect(
      summarizeRoutePerformance([], {
        firstUsefulContentMs: 12,
        routeReadyMs: 25,
      }),
    ).toEqual({
      duplicateRequestCount: 0,
      duplicateRequests: [],
      firstUsefulContentMs: 12,
      maxSimultaneousRequests: 0,
      postResponseToFirstUsefulContentMs: null,
      requestCount: 0,
      routeReadyMs: 25,
      slowestRequest: null,
      timeline: [],
      transferredBytes: 0,
    });
  });

  it("redacts dynamic repository and service identifiers from endpoint shapes", () => {
    const result = summarizeRoutePerformance(
      [
        {
          sequence: 1,
          method: "GET",
          url: "http://localhost/eshu-api/api/v0/repositories/repository%3Aprivate-id/story",
          status: 200,
          startedAtMs: 0,
          responseReceivedAtMs: 8,
          finishedAtMs: 10,
          transferredBytes: 10,
          failed: false,
        },
        {
          sequence: 2,
          method: "GET",
          url: "http://localhost/eshu-api/api/v0/services/private-service/context",
          status: 200,
          startedAtMs: 10,
          responseReceivedAtMs: 18,
          finishedAtMs: 20,
          transferredBytes: 10,
          failed: false,
        },
      ],
      { firstUsefulContentMs: 1, routeReadyMs: 20 },
    );

    expect(result.timeline.map((request) => request.pathname)).toEqual([
      "/eshu-api/api/v0/repositories/:repository/story",
      "/eshu-api/api/v0/services/:service/context",
    ]);
    expect(JSON.stringify(result)).not.toMatch(/private-id|private-service/);
  });

  it("redacts Vite filesystem module paths from performance packets", () => {
    const result = summarizeRoutePerformance(
      [
        {
          sequence: 1,
          method: "GET",
          url: "http://localhost/@fs/Users/operator/private-worktree/node_modules/vite/env.mjs",
          status: 200,
          startedAtMs: 0,
          responseReceivedAtMs: 1,
          finishedAtMs: 2,
          transferredBytes: 10,
          failed: false,
        },
      ],
      { firstUsefulContentMs: 2, routeReadyMs: 3 },
    );

    expect(result.timeline[0].pathname).toBe("/@fs/:local-module");
    expect(JSON.stringify(result)).not.toContain("operator/private-worktree");
  });
});
