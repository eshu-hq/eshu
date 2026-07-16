import { describe, expect, it, vi } from "vitest";

import { installRoutePerformanceRecorder, type DevToolsSession } from "./routePerformanceRecorder";

class FakeDevToolsSession implements DevToolsSession {
  readonly send = vi.fn(async (): Promise<unknown> => ({}));
  private readonly listeners = new Map<string, Set<(payload: unknown) => void>>();

  on(event: string, listener: (payload: unknown) => void): void {
    const listeners = this.listeners.get(event) ?? new Set();
    listeners.add(listener);
    this.listeners.set(event, listeners);
  }

  off(event: string, listener: (payload: unknown) => void): void {
    this.listeners.get(event)?.delete(listener);
  }

  emit(event: string, payload: unknown): void {
    for (const listener of this.listeners.get(event) ?? []) listener(payload);
  }
}

describe("installRoutePerformanceRecorder", () => {
  it("records completed API timing and encoded bytes relative to a capture", async () => {
    const session = new FakeDevToolsSession();
    const recorder = await installRoutePerformanceRecorder(session, () => 1_000);
    const capture = recorder.beginCapture();

    session.emit("Network.requestWillBeSent", {
      requestId: "one",
      timestamp: 10,
      wallTime: 1,
      request: {
        method: "GET",
        url: "http://127.0.0.1:5180/eshu-api/repositories?limit=500",
      },
    });
    session.emit("Network.responseReceived", {
      requestId: "one",
      timestamp: 10.04,
      response: { status: 200 },
    });
    session.emit("Network.loadingFinished", {
      requestId: "one",
      timestamp: 10.05,
      encodedDataLength: 150,
    });

    session.emit("Network.requestWillBeSent", {
      requestId: "two",
      timestamp: 11,
      wallTime: 1.1,
      request: {
        method: "POST",
        url: "http://127.0.0.1:5180/eshu-api/ask",
      },
    });
    session.emit("Network.loadingFailed", {
      requestId: "two",
      timestamp: 11.02,
    });

    expect(session.send).toHaveBeenCalledWith("Network.enable");
    expect(recorder.recordsSince(capture)).toEqual([
      {
        sequence: 1,
        method: "GET",
        url: "http://127.0.0.1:5180/eshu-api/repositories?limit=500",
        status: 200,
        startedAtMs: 0,
        responseReceivedAtMs: 40,
        finishedAtMs: 50,
        transferredBytes: 150,
        failed: false,
      },
      {
        sequence: 2,
        method: "POST",
        url: "http://127.0.0.1:5180/eshu-api/ask",
        status: 0,
        startedAtMs: 100,
        responseReceivedAtMs: null,
        finishedAtMs: 120,
        transferredBytes: 0,
        failed: true,
      },
    ]);
  });

  it("excludes non-API and pre-capture requests", async () => {
    const session = new FakeDevToolsSession();
    let now = 1_000;
    const recorder = await installRoutePerformanceRecorder(session, () => now);

    session.emit("Network.requestWillBeSent", {
      requestId: "before",
      timestamp: 1,
      wallTime: 0.9,
      request: { method: "GET", url: "http://127.0.0.1:5180/eshu-api/status" },
    });
    session.emit("Network.loadingFinished", {
      requestId: "before",
      timestamp: 1.01,
      encodedDataLength: 5,
    });
    now = 1_100;
    const capture = recorder.beginCapture();
    session.emit("Network.requestWillBeSent", {
      requestId: "asset",
      timestamp: 2,
      wallTime: 1.2,
      request: { method: "GET", url: "http://127.0.0.1:5180/src/main.tsx" },
    });

    expect(recorder.recordsSince(capture)).toEqual([]);
  });
});
