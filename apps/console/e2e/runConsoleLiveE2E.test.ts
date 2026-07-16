import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import type { ChildProcessByStdio } from "node:child_process";
import type { Readable } from "node:stream";
import type { Page } from "playwright";
import { describe, expect, it, vi } from "vitest";

import { startDevServerWithSpawner, stopLiveE2EDevServer } from "./liveE2EDevServer.ts";
import { captureRoute, proofManifestForLaunchedBrowser } from "./runConsoleLiveE2E";
import { locatorStub } from "./routeWorkflowProbesTestSupport";

type ViteChild = ChildProcessByStdio<null, Readable, Readable>;

describe("console live E2E launched-browser identity", () => {
  it("records the version reported by the browser instance", () => {
    const version = vi.fn(() => "Chromium 140.0.7339.16");
    const manifest = proofManifestForLaunchedBrowser(
      {
        ESHU_E2E_PROOF_ID: "dashboard-proof-20260715",
        ESHU_E2E_SOURCE_HASH: "a".repeat(64),
        ESHU_E2E_RUNNER_HASH: "b".repeat(64),
        ESHU_E2E_API_IMAGE_DIGEST: `sha256:${"c".repeat(64)}`,
        ESHU_E2E_API_VERSION: "proof-a1b2c3",
        ESHU_E2E_NORNIC_IMAGE_DIGEST: `sha256:${"d".repeat(64)}`,
        ESHU_E2E_NORNIC_VERSION: "v1.1.11",
        ESHU_E2E_NODE_VERSION: "v24.4.1",
        ESHU_E2E_PLAYWRIGHT_VERSION: "1.60.0",
        ESHU_E2E_CORPUS_ATTESTATION: "retained-task-777",
        ESHU_E2E_CORPUS_REPOSITORY_COUNT: "896",
      },
      { version },
    );

    expect(version).toHaveBeenCalledOnce();
    expect(manifest.runtime.launchedBrowserVersion).toBe("Chromium 140.0.7339.16");
  });
});

function stalledViteChild(): {
  readonly child: ViteChild;
  readonly kill: ReturnType<typeof vi.fn>;
} {
  const emitter = new EventEmitter() as EventEmitter & {
    exitCode: number | null;
    signalCode: NodeJS.Signals | null;
    stderr: PassThrough;
    stdout: PassThrough;
    kill: ReturnType<typeof vi.fn>;
  };
  emitter.exitCode = null;
  emitter.signalCode = null;
  emitter.stderr = new PassThrough();
  emitter.stdout = new PassThrough();
  emitter.kill = vi.fn((signal: NodeJS.Signals) => {
    emitter.signalCode = signal;
    queueMicrotask(() => emitter.emit("exit", null, signal));
    return true;
  });
  return { child: emitter as unknown as ViteChild, kill: emitter.kill };
}

describe("console live E2E dev server ownership", () => {
  it("terminates the spawned Vite child when readiness times out", async () => {
    const { child, kill } = stalledViteChild();

    await expect(startDevServerWithSpawner(() => child, 1)).rejects.toThrow(
      "dev server did not become ready in time",
    );

    expect(kill).toHaveBeenCalledWith("SIGTERM");
  });

  it("waits for the Vite child to exit after a forced kill", async () => {
    const { child, kill } = stalledViteChild();
    let exited = false;
    kill.mockImplementation((signal: NodeJS.Signals) => {
      if (signal === "SIGKILL") {
        queueMicrotask(() => {
          exited = true;
          child.emit("exit", null, signal);
        });
      }
      return true;
    });

    await stopLiveE2EDevServer({ process: child, baseUrl: "" }, 1);

    expect(kill).toHaveBeenNthCalledWith(1, "SIGTERM");
    expect(kill).toHaveBeenNthCalledWith(2, "SIGKILL");
    expect(exited).toBe(true);
  });
});

describe("console live E2E route response ownership", () => {
  it("rejects a route shell when only bootstrap observed its required response", async () => {
    const shell = locatorStub();
    const evaluate = vi.fn().mockResolvedValueOnce(undefined).mockResolvedValueOnce({
      connected: true,
      sourceMode: "live",
      demoBannerPresent: false,
      mainContentChars: 100,
    });
    const page = {
      evaluate,
      locator: vi.fn(() => shell),
      off: vi.fn(),
      on: vi.fn(),
      screenshot: vi.fn().mockResolvedValue(undefined),
      waitForFunction: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      waitForTimeout: vi.fn().mockResolvedValue(undefined),
    } as unknown as Page;
    const bootstrapNetwork = [
      {
        method: "POST",
        status: 200,
        url: "http://host/eshu-api/api/v0/relationships/catalog",
        failureText: null,
      },
    ] as const;

    const signals = await captureRoute(
      page,
      {
        path: "/relationships",
        label: "Relationships",
        area: "graph",
        workflow: {
          id: "relationships-live",
          kind: "state",
          anySelectors: [".rel-verb-row"],
          requiredResponses: [
            {
              path: "/api/v0/relationships/catalog",
              method: "POST",
              acceptedStatuses: [200],
            },
          ],
        },
      },
      { inFlight: () => 0, lastChangeAt: () => Date.now() - 1_000 },
      bootstrapNetwork,
      "/tmp/eshu-live-e2e-test-screenshots",
    );

    expect(signals.workflow?.passed).toBe(false);
    expect(signals.workflow?.detail).toContain("required route response");
    expect(signals.mainContentChars).toBe(100);
  });

  it("accepts bootstrap responses only when the snapshot-backed route declares bootstrap ownership", async () => {
    const shell = locatorStub();
    const evaluate = vi.fn().mockResolvedValueOnce(undefined).mockResolvedValueOnce({
      connected: true,
      sourceMode: "live",
      demoBannerPresent: false,
      mainContentChars: 100,
    });
    const page = {
      evaluate,
      locator: vi.fn(() => shell),
      off: vi.fn(),
      on: vi.fn(),
      screenshot: vi.fn().mockResolvedValue(undefined),
      waitForFunction: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      waitForTimeout: vi.fn().mockResolvedValue(undefined),
    } as unknown as Page;
    const bootstrapNetwork = [
      {
        method: "POST",
        status: 200,
        url: "http://host/eshu-api/api/v0/code/dead-code",
        failureText: null,
      },
      {
        method: "GET",
        status: 200,
        url: "http://host/eshu-api/api/v0/supply-chain/impact/findings?impact_status=affected_exact",
        failureText: null,
      },
      {
        method: "GET",
        status: 200,
        url: "http://host/eshu-api/api/v0/supply-chain/impact/findings?impact_status=affected_derived",
        failureText: null,
      },
    ] as const;

    const signals = await captureRoute(
      page,
      {
        path: "/findings",
        label: "Findings",
        area: "security",
        workflow: {
          id: "findings-live",
          kind: "state",
          anySelectors: ["[data-finding-row]"],
          requiredBootstrapResponses: [
            {
              path: "/api/v0/code/dead-code",
              method: "POST",
              acceptedStatuses: [200],
            },
            {
              path: "/api/v0/supply-chain/impact/findings",
              method: "GET",
              acceptedStatuses: [200],
              query: { impact_status: "affected_exact" },
            },
            {
              path: "/api/v0/supply-chain/impact/findings",
              method: "GET",
              acceptedStatuses: [200],
              query: { impact_status: "affected_derived" },
            },
          ],
        },
      },
      { inFlight: () => 0, lastChangeAt: () => Date.now() - 1_000 },
      bootstrapNetwork,
      "/tmp/eshu-live-e2e-test-screenshots",
      bootstrapNetwork,
    );

    expect(signals.workflow?.passed).toBe(true);
    expect(signals.workflow?.requests).toEqual([
      {
        method: "POST",
        pathname: "/eshu-api/api/v0/code/dead-code",
        phase: "bootstrap",
        status: 200,
      },
      {
        method: "GET",
        pathname: "/eshu-api/api/v0/supply-chain/impact/findings",
        phase: "bootstrap",
        status: 200,
      },
      {
        method: "GET",
        pathname: "/eshu-api/api/v0/supply-chain/impact/findings",
        phase: "bootstrap",
        status: 200,
      },
    ]);
  });
});
