// Console live E2E gate runner (issue #3326).
//
// Drives the PRIVATE/LIVE Eshu console against the REAL local Compose stack
// through a Chromium browser and proves every major route renders real data or
// an explicit empty/unavailable state with no demo fallback, no unhandled
// console errors, and no unexpected failed network requests.
//
// This file is the integration harness. The pass/fail decision logic lives in
// the unit-tested pure module src/e2e/routeAssertions.ts; this runner only
// captures browser signals and hands them to that evaluator.
//
// Flow:
//   1. Start the Vite dev server (it proxies /eshu-api -> the local API) with
//      VITE_ESHU_API_KEY so the console authenticates against the live stack.
//   2. Seed localStorage with a private-mode environment before the app boots.
//   3. For each route: navigate, let it settle, capture console + network +
//      DOM signals, screenshot.
//   4. Evaluate every route and write a JSON report + screenshots + trace into
//      the gitignored artifacts dir. Exit non-zero if any route fails.
//
// This module exports runConsoleLiveE2E() and is loaded by the .mjs bootstrap
// scripts/console-live-e2e-runtime.mjs, which transforms it through Vite's SSR
// loader. That keeps the gate runnable on any supported Node version without
// native TypeScript stripping (Node >= 23.6 only) or an extra dependency. Run
// it via the npm script (see apps/console/README.md):
//   npm run console:e2e

import { spawn, type ChildProcessByStdio } from "node:child_process";
import type { Readable } from "node:stream";
import { mkdir, rm, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium, type Browser, type Page } from "playwright";

import {
  consoleRoutes,
  defaultNetworkAllowList,
  evaluateRoute,
  summarizeGate,
  type ConsoleRoute,
  type NetworkObservation,
  type RouteResult,
  type RouteSignals,
} from "../src/e2e/routeAssertions.ts";
import { executeRouteWorkflow } from "./routeWorkflowProbes.ts";
import {
  awaitApiQuiet,
  filterAllowedResourceConsoleErrors,
  isConsoleApiUrl,
  traceCaptureEnabled,
} from "./liveE2EPolicy.ts";

const here = dirname(fileURLToPath(import.meta.url));
const consoleDir = resolve(here, "..");
const repoRoot = resolve(consoleDir, "..", "..");
const artifactsDir = resolve(repoRoot, "e2e-artifacts");
const screenshotsDir = resolve(artifactsDir, "screenshots");
const tracePath = resolve(artifactsDir, "trace.zip");
const reportPath = resolve(artifactsDir, "console-live-e2e-report.json");

// Local-only test key. The runner reads it from the environment so the key is
// never hard-coded; the npm script supplies it from the gitignored env file.
const apiKey = (process.env.ESHU_E2E_API_KEY ?? "").trim();
const apiBase = (process.env.ESHU_E2E_API_BASE ?? "http://127.0.0.1:9080").trim();
// The console always talks to its own dev-server proxy path, never the API
// directly; the proxy target is the live API.
const consoleApiBaseUrl = "/eshu-api/";
const perRouteSettleMs = 1500;
const navTimeoutMs = 30000;
const devServerReadyTimeoutMs = 120000;

interface DevServer {
  readonly process: ChildProcessByStdio<null, Readable, Readable>;
  readonly baseUrl: string;
}

// startDevServer launches Vite for the console and resolves once it prints the
// local URL. The dev server owns the /eshu-api proxy that points at the live
// stack, so running the real dev server is the correct LIVE-mode path.
const viteBin = resolve(repoRoot, "node_modules", ".bin", "vite");

async function startDevServer(): Promise<DevServer> {
  // Invoke the vite binary directly (not via npx) so the spawned PID is vite
  // itself and SIGTERM on teardown actually frees the port; an npx wrapper can
  // leave an orphaned vite child holding the port between runs.
  const child = spawn(
    viteBin,
    [
      "--config",
      "apps/console/vite.config.ts",
      "--host",
      "127.0.0.1",
      "--strictPort",
      "--port",
      "5180",
    ],
    {
      cwd: repoRoot,
      env: {
        ...process.env,
        VITE_ESHU_API_KEY: apiKey,
        // Point the dev-server proxy at the live API the operator brought up.
        ESHU_DEV_PROXY_TARGET: apiBase,
      },
      stdio: ["ignore", "pipe", "pipe"],
    },
  );

  const baseUrl = await new Promise<string>((resolvePromise, rejectPromise) => {
    const timer = setTimeout(() => {
      rejectPromise(new Error("dev server did not become ready in time"));
    }, devServerReadyTimeoutMs);

    const onData = (chunk: Buffer): void => {
      const text = chunk.toString();
      process.stdout.write(`[vite] ${text}`);
      const match = text.match(/Local:\s+(http:\/\/127\.0\.0\.1:\d+)\/?/);
      if (match) {
        clearTimeout(timer);
        resolvePromise(match[1]);
      }
    };
    child.stdout.on("data", onData);
    child.stderr.on("data", (chunk: Buffer) =>
      process.stderr.write(`[vite-err] ${chunk.toString()}`),
    );
    child.on("exit", (code) => {
      clearTimeout(timer);
      rejectPromise(new Error(`dev server exited early with code ${String(code)}`));
    });
  });

  return { process: child, baseUrl };
}

// stopDevServer terminates the Vite process and resolves once it has exited so
// the listening port is freed before the runner returns.
async function stopDevServer(server: DevServer): Promise<void> {
  if (server.process.exitCode !== null || server.process.signalCode !== null) {
    return;
  }
  await new Promise<void>((resolvePromise) => {
    const done = (): void => resolvePromise();
    server.process.once("exit", done);
    server.process.kill("SIGTERM");
    // Hard-stop guard so a hung dev server cannot block teardown forever.
    setTimeout(() => {
      server.process.kill("SIGKILL");
      resolvePromise();
    }, 5000);
  });
}

// ignorableConsoleError filters benign noise that is not an app fault. React
// dev-mode emits a known download-the-devtools info line on some setups; we do
// not treat that as a failure. Everything else counts.
function ignorableConsoleError(message: string): boolean {
  return message.includes("Download the React DevTools");
}

// ApiQuietTracker counts in-flight /eshu-api requests on a page so the runner
// can wait for a route's data fetches to settle before navigating away. This
// prevents client-side navigation from aborting still-pending requests
// (net::ERR_ABORTED), which would otherwise be indistinguishable from a real
// failed request at the gate. Listeners are installed once and span the whole
// route walk.
interface ApiQuietTracker {
  inFlight(): number;
  lastChangeAt(): number;
}

function installApiQuietTracker(page: Page): ApiQuietTracker {
  let inFlight = 0;
  let lastChangeAt = Date.now();
  const settle = (url: string): void => {
    if (!isConsoleApiUrl(url)) {
      return;
    }
    inFlight = Math.max(0, inFlight - 1);
    lastChangeAt = Date.now();
  };
  page.on("request", (request) => {
    if (isConsoleApiUrl(request.url())) {
      inFlight += 1;
      lastChangeAt = Date.now();
    }
  });
  page.on("requestfinished", (request) => settle(request.url()));
  page.on("requestfailed", (request) => settle(request.url()));
  return {
    inFlight: () => inFlight,
    lastChangeAt: () => lastChangeAt,
  };
}

// waitForApiQuiet blocks until no /eshu-api request has been in flight for a
// short quiet window, or a hard cap elapses. The cap is deliberately longer
// than EshuApiClient's request timeout, so a client-timed-out request settles on
// its owning route before navigation can attribute its abort to the next one.
async function waitForApiQuiet(page: Page, tracker: ApiQuietTracker) {
  return awaitApiQuiet(tracker, (duration) => page.waitForTimeout(duration));
}

function allowedConsoleStatuses(network: readonly NetworkObservation[]): readonly number[] {
  return network.flatMap((observation) => {
    let pathname = "";
    try {
      pathname = new URL(observation.url).pathname;
    } catch {
      return [];
    }
    return defaultNetworkAllowList.some(
      (rule) =>
        rule.method === observation.method.toUpperCase() &&
        rule.pathname === pathname &&
        rule.status === observation.status,
    )
      ? [observation.status]
      : [];
  });
}

// captureRoute navigates to one route and records its signals. localStorage is
// seeded once on the first navigation (origin is stable across routes).
async function captureRoute(
  page: Page,
  route: ConsoleRoute,
  tracker: ApiQuietTracker,
): Promise<RouteSignals> {
  const startedAt = Date.now();
  const consoleErrors: string[] = [];
  const network: NetworkObservation[] = [];

  const onConsole = (msg: { type: () => string; text: () => string }): void => {
    if (msg.type() === "error" && !ignorableConsoleError(msg.text())) {
      consoleErrors.push(msg.text());
    }
  };
  const onPageError = (error: Error): void => {
    consoleErrors.push(`uncaught: ${error.message}`);
  };
  const onResponse = (response: {
    url: () => string;
    status: () => number;
    request: () => { method: () => string };
  }): void => {
    network.push({
      url: response.url(),
      method: response.request().method(),
      status: response.status(),
      failureText: null,
    });
  };
  const onRequestFailed = (request: {
    url: () => string;
    method: () => string;
    failure: () => { errorText: string } | null;
  }): void => {
    network.push({
      url: request.url(),
      method: request.method(),
      status: 0,
      failureText: request.failure()?.errorText ?? "request failed",
    });
  };

  page.on("console", onConsole);
  page.on("pageerror", onPageError);
  page.on("response", onResponse);
  page.on("requestfailed", onRequestFailed);

  // Navigate and wait for the DOM to be ready. We intentionally use
  // "domcontentloaded" rather than "networkidle": the console runs background
  // status polls that may never let the network go fully idle, and waiting on
  // idle would either hang or mask the rendered state. We then wait for this
  // route's /eshu-api fetches to settle so navigating to the next route cannot
  // abort still-pending requests. A genuine navigation failure (e.g. dev server
  // down) still throws and fails the run.
  await page.goto(routeUrl(page, route), { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
  await page.waitForSelector(".source-pill.src-connected", { timeout: navTimeoutMs });
  await page.waitForTimeout(perRouteSettleMs);
  const apiQuiet = await waitForApiQuiet(page, tracker);

  const workflow = route.workflow
    ? await executeRouteWorkflow(page, route.workflow, async () => {
        const result = await waitForApiQuiet(page, tracker);
        if (!result.settled) throw new Error(`${result.inFlight} API request(s) remained active`);
      })
    : null;

  const dom = await page.evaluate(() => {
    const pill = document.querySelector(".source-pill");
    const main = document.querySelector(".main");
    const demoBanner = Array.from(document.querySelectorAll(".prov-banner")).some(
      (node) =>
        (node.textContent ?? "").includes("Prospect demo") ||
        (node.textContent ?? "").toLowerCase().includes("demo fixtures"),
    );
    const pillText = (pill?.textContent ?? "").trim();
    const connected = pill?.className.includes("src-connected") ?? false;
    const sourceMode = connected
      ? pillText.toLowerCase().includes("demo")
        ? "demo"
        : "live"
      : pillText.toLowerCase().replace(/[^a-z]+/g, "-") || "unknown";
    return {
      connected,
      sourceMode,
      demoBannerPresent: demoBanner,
      mainContentChars: (main?.textContent ?? "").trim().length,
    };
  });

  const safeName = route.label.replace(/[^a-z0-9]+/gi, "_").toLowerCase();
  await page.screenshot({ path: resolve(screenshotsDir, `${safeName}.png`), fullPage: true });

  page.off("console", onConsole);
  page.off("pageerror", onPageError);
  page.off("response", onResponse);
  page.off("requestfailed", onRequestFailed);

  return {
    route,
    connected: dom.connected,
    sourceMode: dom.sourceMode,
    demoBannerPresent: dom.demoBannerPresent,
    mainContentChars: dom.mainContentChars,
    consoleErrors: filterAllowedResourceConsoleErrors(
      consoleErrors,
      allowedConsoleStatuses(network),
    ),
    network,
    workflow,
    apiQuiet,
    durationMs: Date.now() - startedAt,
  };
}

function routeUrl(page: Page, route: ConsoleRoute): string {
  const origin = new URL(page.url()).origin;
  return `${origin}${route.path}`;
}

// runConsoleLiveE2E runs the gate and returns a process exit code (0 = pass).
// It never calls process.exit itself so the finally block always tears down the
// dev server and frees the port, even when the gate fails. It is invoked by the
// .mjs bootstrap (scripts/console-live-e2e-runtime.mjs), which loads this
// TypeScript module through Vite's SSR transformer so the gate runs on any
// supported Node version without native TS stripping or an extra dependency.
export async function runConsoleLiveE2E(): Promise<number> {
  if (apiKey.length === 0) {
    process.stderr.write(
      "console-live-e2e: ESHU_E2E_API_KEY is required so the console can authenticate against the live API\n",
    );
    return 1;
  }

  // Clean only this gate's own outputs, not the whole artifacts dir: the dir
  // also holds operator-managed files (logs, the local env file) that the gate
  // must not delete.
  await rm(screenshotsDir, { recursive: true, force: true });
  await rm(tracePath, { force: true });
  await rm(reportPath, { force: true });
  await mkdir(screenshotsDir, { recursive: true });

  process.stdout.write(`console-live-e2e: live API base ${apiBase}\n`);

  const server = await startDevServer();
  const captureTrace = traceCaptureEnabled(process.env.ESHU_CONSOLE_E2E_TRACE);
  let browser: Browser | undefined;
  try {
    browser = await chromium.launch();
    const context = await browser.newContext();
    if (captureTrace) {
      await context.tracing.start({ screenshots: true, snapshots: true });
    }

    // Seed the private-mode environment on the dev-server origin before the app
    // script runs, so the console boots straight into a live connection.
    await context.addInitScript(
      ([base]: readonly string[]) => {
        window.localStorage.setItem(
          "eshu.console.environment",
          JSON.stringify({ mode: "private", apiBaseUrl: base, recentApiBaseUrls: [base] }),
        );
      },
      [consoleApiBaseUrl],
    );

    const page = await context.newPage();
    const tracker = installApiQuietTracker(page);
    // Prime the origin once so the init script and storage are in place. A
    // failure here means the dev server is unreachable and must fail the run.
    await page.goto(server.baseUrl, { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
    // Wait for the boot connect to finish before walking routes. Without this,
    // the first route navigation fires while the boot snapshot fetches are still
    // in flight and the browser aborts them (net::ERR_ABORTED) — a client-side
    // navigation cancel, not a server fault, but indistinguishable at the gate.
    // Letting the connection settle first makes every route's network clean.
    await page.waitForSelector(".source-pill.src-connected", { timeout: navTimeoutMs });
    // Let the boot connect's fetches fully settle before the first route, so the
    // first navigation does not abort in-flight boot requests.
    const bootQuiet = await waitForApiQuiet(page, tracker);
    if (!bootQuiet.settled) {
      throw new Error(
        `${bootQuiet.inFlight} boot API request(s) remained active after ${bootQuiet.waitedMs}ms`,
      );
    }

    const results: RouteResult[] = [];
    for (const route of consoleRoutes) {
      const signals = await captureRoute(page, route, tracker);
      const result = evaluateRoute(signals, defaultNetworkAllowList);
      results.push(result);
      const status = result.passed ? "PASS" : "FAIL";
      const workflowStatus = signals.workflow
        ? `${signals.workflow.passed ? "pass" : "fail"}:${signals.workflow.id}`
        : "none";
      process.stdout.write(
        `  ${status} ${route.path} (duration=${signals.durationMs}ms, mode=${signals.sourceMode}, workflow=${workflowStatus}, mainChars=${signals.mainContentChars}, errors=${signals.consoleErrors.length}, reqs=${signals.network.length})\n`,
      );
      for (const failure of result.failures) {
        process.stdout.write(`        - ${failure.code}: ${failure.detail}\n`);
      }
    }

    if (captureTrace) {
      await context.tracing.stop({ path: tracePath });
    }
    await browser.close();
    browser = undefined;

    const summary = summarizeGate(results);
    await writeFile(reportPath, JSON.stringify({ apiBase, summary }, null, 2), "utf8");
    process.stdout.write(
      `console-live-e2e: ${summary.passedCount}/${summary.total} routes passed; report ${reportPath}\n`,
    );

    if (!summary.passed) {
      process.stderr.write(
        `console-live-e2e: ${summary.failedCount} route(s) failed the live gate\n`,
      );
      return 1;
    }
    return 0;
  } finally {
    if (browser) {
      await browser.close().catch(() => undefined);
    }
    await stopDevServer(server);
  }
}
