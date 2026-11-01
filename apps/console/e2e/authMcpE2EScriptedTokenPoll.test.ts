// The scripted-OAuth tools/call proof must fail closed and tolerate the
// one transient core really has: the oidcbearer provider snapshot is TTL-based
// (30s) with no invalidation hook, so the first JWT call after the provider is
// created can answer `unauthenticated` until core refreshes. These tests pin the
// retry-on-unauthenticated / fail-fast-on-everything-else contract with an
// injected clock and sleep, so no test waits for real time.
import { describe, expect, it } from "vitest";

import type { McpJsonRpcResult } from "./authMcpE2EJsonRpc.ts";
import {
  classifyScriptedTokenCall,
  pollScriptedTokenWorks,
  scriptedTokenPollIntervalMs,
  scriptedTokenPollTimeoutMs,
} from "./authMcpE2EScriptedTokenPoll.ts";

function toolCall(structured: unknown, httpStatus = 200): McpJsonRpcResult {
  const json = { jsonrpc: "2.0", id: 3, result: { structuredContent: structured } };
  return { httpStatus, wwwAuthenticate: null, bodyText: JSON.stringify(json), json };
}

const unauthenticated = toolCall({
  data: null,
  truth: null,
  error: { code: "unauthenticated", message: "authentication is required" },
});

const success = (count: number): McpJsonRpcResult =>
  toolCall({
    data: { repositories: Array.from({ length: count }, (_, i) => ({ id: `repo-${i}` })), total: count },
    truth: { level: "exact" },
    error: null,
  });

// fakeClock advances only when sleep is called, so elapsed time is exact.
function fakeClock() {
  let nowMs = 1_000;
  const sleeps: number[] = [];
  return {
    now: () => nowMs,
    sleep: async (ms: number) => {
      sleeps.push(ms);
      nowMs += ms;
    },
    sleeps,
  };
}

// scripted returns the queued results in order, repeating the last one.
function scripted(results: readonly McpJsonRpcResult[]) {
  let calls = 0;
  return {
    call: async () => results[Math.min(calls++, results.length - 1)]!,
    count: () => calls,
  };
}

describe("classifyScriptedTokenCall", () => {
  it("reads the repository count from data.repositories, not a top-level total", () => {
    const verdict = classifyScriptedTokenCall(success(2));
    expect(verdict).toEqual({ kind: "ok", repositoryCount: 2 });
  });

  it("reports zero repositories only when data.repositories is a real empty array", () => {
    expect(classifyScriptedTokenCall(success(0))).toEqual({ kind: "ok", repositoryCount: 0 });
  });

  it("treats an unauthenticated envelope error as not-converged-yet", () => {
    const verdict = classifyScriptedTokenCall(unauthenticated);
    expect(verdict.kind).toBe("retry");
    expect(verdict.kind === "retry" && verdict.detail).toContain("unauthenticated");
  });

  it("fails immediately on a non-200 HTTP status", () => {
    expect(() => classifyScriptedTokenCall(toolCall({}, 401))).toThrow(/expected 200, got 401/);
  });

  it("fails immediately on an envelope error with any other code", () => {
    const forbidden = toolCall({ data: null, truth: null, error: { code: "forbidden", message: "nope" } });
    expect(() => classifyScriptedTokenCall(forbidden)).toThrow(/forbidden/);
  });

  it("fails closed on a payload with no data.repositories list instead of reporting zero", () => {
    expect(() => classifyScriptedTokenCall(toolCall({ total: 0 }))).toThrow(/no envelope data object/);
    expect(() => classifyScriptedTokenCall(toolCall({ data: {}, error: null }))).toThrow(/no repositories array/);
  });

  it("fails closed when structuredContent is missing", () => {
    const noStructured: McpJsonRpcResult = {
      httpStatus: 200,
      wwwAuthenticate: null,
      bodyText: "{}",
      json: { result: {} },
    };
    expect(() => classifyScriptedTokenCall(noStructured)).toThrow(/missing structuredContent/);
  });
});

describe("pollScriptedTokenWorks", () => {
  it("returns on the first attempt with convergence time 0 when core already converged", async () => {
    const clock = fakeClock();
    const probe = scripted([success(0)]);
    const detail = await pollScriptedTokenWorks(probe.call, clock);
    expect(probe.count()).toBe(1);
    expect(clock.sleeps).toEqual([]);
    expect(detail).toContain("repositories=0");
    expect(detail).toContain("converged after 0ms");
  });

  it("retries unauthenticated then succeeds, and reports convergence time and count", async () => {
    const clock = fakeClock();
    const probe = scripted([unauthenticated, unauthenticated, unauthenticated, success(0)]);
    const detail = await pollScriptedTokenWorks(probe.call, clock);
    expect(probe.count()).toBe(4);
    expect(clock.sleeps).toEqual([scriptedTokenPollIntervalMs, scriptedTokenPollIntervalMs, scriptedTokenPollIntervalMs]);
    expect(detail).toContain(`converged after ${3 * scriptedTokenPollIntervalMs}ms`);
    expect(detail).toContain("4 attempt(s)");
    expect(detail).toContain("repositories=0");
  });

  it("times out on a permanent unauthenticated and carries the last observed error", async () => {
    const clock = fakeClock();
    const probe = scripted([unauthenticated]);
    await expect(pollScriptedTokenWorks(probe.call, clock)).rejects.toThrow(
      new RegExp(`still unauthenticated after ${scriptedTokenPollTimeoutMs}ms.*permanent denial.*unauthenticated.*authentication is required`),
    );
    // One attempt per interval across the whole budget, including t=0 and t=timeout.
    expect(probe.count()).toBe(scriptedTokenPollTimeoutMs / scriptedTokenPollIntervalMs + 1);
    expect(clock.now()).toBe(1_000 + scriptedTokenPollTimeoutMs);
  });

  it("fails immediately on a non-unauthenticated envelope error without retrying", async () => {
    const clock = fakeClock();
    const probe = scripted([toolCall({ data: null, truth: null, error: { code: "backend_unavailable", message: "x" } })]);
    await expect(pollScriptedTokenWorks(probe.call, clock)).rejects.toThrow(/backend_unavailable/);
    expect(probe.count()).toBe(1);
    expect(clock.sleeps).toEqual([]);
  });

  it("fails immediately on HTTP non-200 without retrying", async () => {
    const clock = fakeClock();
    const probe = scripted([toolCall({}, 503)]);
    await expect(pollScriptedTokenWorks(probe.call, clock)).rejects.toThrow(/expected 200, got 503/);
    expect(probe.count()).toBe(1);
  });

  it("fails immediately on a malformed success shape without retrying", async () => {
    const clock = fakeClock();
    const probe = scripted([toolCall({ total: 0 })]);
    await expect(pollScriptedTokenWorks(probe.call, clock)).rejects.toThrow(/no envelope data object/);
    expect(probe.count()).toBe(1);
  });

  it("keeps the budget at or above the 30s oidcbearer snapshot TTL", () => {
    expect(scriptedTokenPollTimeoutMs).toBeGreaterThanOrEqual(30_000);
    expect(scriptedTokenPollIntervalMs).toBe(2_000);
    expect(scriptedTokenPollTimeoutMs).toBe(60_000);
  });
});
