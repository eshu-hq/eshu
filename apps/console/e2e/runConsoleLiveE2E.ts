// Console live E2E gate runner (issue #3326). It drives the private console
// against an operator-managed corpus stack, captures route/network/DOM signals,
// and delegates verdicts to the pure routeAssertions module. The default gate
// claims an isolated retained-corpus identity through the real setup wizard and
// executes every route workflow with the resulting browser-session cookie.
// The .mjs bootstrap loads this module through Vite's SSR transformer; run it
// with `npm run console:e2e` as documented in apps/console/README.md.

import { mkdir, rm, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium, type Browser, type Page } from "playwright";

import {
  buildNetworkReportObservation,
  consoleRoutes,
  buildRouteReportObservation,
  defaultNetworkAllowList,
  evaluateNetworkObservations,
  evaluateRoute,
  summarizeGate,
  type ConsoleRoute,
  type NetworkObservation,
  type RouteResult,
  type RouteSignals,
} from "../src/e2e/routeAssertions.ts";
import { executeRouteWorkflow } from "./routeWorkflowProbes.ts";
import {
  loadIndexedRepositoryInventoryAnchor,
  type IndexedRepositoryInventoryAnchor,
} from "./askExactCountWorkflowProbe.ts";
import { createImpactFindingsLoader } from "./vulnerabilityRouteWorkflowProbe.ts";
import type { LoadImpactFindings } from "./vulnerabilityRouteWorkflowProbe.ts";
import { createBrowserSessionFetcher } from "./browserSessionFetcher.ts";
import {
  awaitRetainedWizardSessionQuiet,
  establishRetainedWizardSession,
  parseLiveE2EAuthMode,
} from "./liveE2EBrowserSession.ts";
import {
  awaitApiQuiet,
  filterAllowedResourceConsoleErrors,
  installApiQuietTracker,
  installNetworkObservationRecorder,
  navigateClientRoute,
  traceCaptureEnabled,
  type ApiQuietTracker,
} from "./liveE2EPolicy.ts";
import {
  authCoverageReport,
  createAuthRouteCoverage,
  formatAuthRouteCoverage,
} from "./liveE2EAuthCoverage.ts";
import {
  startLiveE2EDevServer,
  stopLiveE2EDevServer,
  type LiveE2EDevServer,
} from "./liveE2EDevServer.ts";
import {
  assertProofManifestRepositoryCount,
  proofManifestFromEnvironment,
} from "./liveE2EProofManifest.ts";

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
const authMode = parseLiveE2EAuthMode(process.env.ESHU_E2E_AUTH_MODE);
const postgresDSN = (process.env.ESHU_E2E_POSTGRES_DSN ?? "").trim();
const authSecretEncKey = (process.env.ESHU_AUTH_SECRET_ENC_KEY ?? "").trim();
const wizardNewPassword = (process.env.ESHU_E2E_WIZARD_NEW_PASSWORD ?? "").trim();
// The console always talks to its own dev-server proxy path, never the API
// directly; the proxy target is the live API.
const consoleApiBaseUrl = "/eshu-api/";
const perRouteSettleMs = 1500;
const navTimeoutMs = 30000;
// ignorableConsoleError filters benign noise that is not an app fault. React
// dev-mode emits a known download-the-devtools info line on some setups; we do
// not treat that as a failure. Everything else counts.
function ignorableConsoleError(message: string): boolean {
  return message.includes("Download the React DevTools");
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
export async function captureRoute(
  page: Page,
  route: ConsoleRoute,
  tracker: ApiQuietTracker,
  allNetwork: readonly NetworkObservation[],
  bootstrapNetwork: readonly NetworkObservation[] = [],
  indexedRepositoryInventory: IndexedRepositoryInventoryAnchor | null = null,
  loadImpactFindings?: LoadImpactFindings,
): Promise<RouteSignals> {
  const startedAt = Date.now();
  const consoleErrors: string[] = [];
  const networkStart = allNetwork.length;

  const onConsole = (msg: { type: () => string; text: () => string }): void => {
    if (msg.type() === "error" && !ignorableConsoleError(msg.text())) {
      consoleErrors.push(msg.text());
    }
  };
  const onPageError = (error: Error): void => {
    consoleErrors.push(`uncaught: ${error.message}`);
  };
  page.on("console", onConsole);
  page.on("pageerror", onPageError);

  // Keep the connected app shell alive while changing routes. Repeating a
  // document-level page.goto here rebooted the full snapshot on every route,
  // multiplied boot fan-out, and attributed the prior route's aborted requests
  // to the next route. This follows the same history/popstate contract as the
  // router's in-app links, then waits until the router has accepted the path.
  await navigateClientRoute(
    route.path,
    async (path) => {
      await page.evaluate((nextPath) => {
        window.history.pushState({}, "", nextPath);
        window.dispatchEvent(new PopStateEvent("popstate"));
      }, path);
    },
    async (path) => {
      await page.waitForFunction(
        (expectedPath) => window.location.pathname === expectedPath,
        path,
        { timeout: navTimeoutMs },
      );
    },
  );
  await page.waitForSelector(".source-pill.src-connected", { timeout: navTimeoutMs });
  await page.waitForTimeout(perRouteSettleMs);
  const apiQuiet = await waitForApiQuiet(page, tracker);

  // A route may prove only responses observed after its own navigation began.
  // Bootstrap remains an explicit, separately evaluated preflight below.
  const routeNetwork = allNetwork.slice(networkStart);
  const workflow = route.workflow
    ? await executeRouteWorkflow(
        page,
        route.workflow,
        async () => {
          const result = await waitForApiQuiet(page, tracker);
          if (!result.settled) throw new Error(`${result.inFlight} API request(s) remained active`);
        },
        routeNetwork,
        bootstrapNetwork,
        loadImpactFindings,
        indexedRepositoryInventory,
      )
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
  const network = allNetwork.slice(networkStart);

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

// runConsoleLiveE2E runs the gate and returns a process exit code (0 = pass).
// It never calls process.exit itself so the finally block always tears down the
// dev server and frees the port, even when the gate fails. It is invoked by the
// .mjs bootstrap (scripts/console-live-e2e-runtime.mjs), which loads this
// TypeScript module through Vite's SSR transformer so the gate runs on any
// supported Node version without native TS stripping or an extra dependency.
export async function runConsoleLiveE2E(): Promise<number> {
  let proofManifest;
  try {
    proofManifest = proofManifestFromEnvironment(process.env);
  } catch (error) {
    process.stderr.write(
      `console-live-e2e: ${error instanceof Error ? error.message : String(error)}\n`,
    );
    return 1;
  }
  if (authMode === "bearer" && apiKey.length === 0) {
    process.stderr.write("console-live-e2e: ESHU_E2E_API_KEY is required in bearer mode\n");
    return 1;
  }
  if (
    authMode === "browser_session" &&
    (postgresDSN === "" || authSecretEncKey === "" || wizardNewPassword === "")
  ) {
    process.stderr.write(
      "console-live-e2e: browser_session mode requires ESHU_E2E_POSTGRES_DSN, ESHU_AUTH_SECRET_ENC_KEY, and ESHU_E2E_WIZARD_NEW_PASSWORD\n",
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

  const captureTrace = traceCaptureEnabled(process.env.ESHU_CONSOLE_E2E_TRACE);
  let server: LiveE2EDevServer | undefined;
  let browser: Browser | undefined;
  try {
    server = await startLiveE2EDevServer({
      repoRoot,
      apiBase,
      apiKey,
      useBearerAuth: authMode === "bearer",
    });
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
    if (authMode === "browser_session") {
      const sessionProof = await establishRetainedWizardSession(page, {
        consoleBaseUrl: server.baseUrl,
        postgresDSN,
        authSecretEncKey,
        newPassword: wizardNewPassword,
        timeoutMs: navTimeoutMs,
      });
      process.stdout.write(`console-live-e2e: ${sessionProof}\n`);
      await awaitRetainedWizardSessionQuiet(page, tracker);
    }
    const networkRecorder = installNetworkObservationRecorder(page);
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
    const bootstrapNetwork = [...networkRecorder.observations];
    const bootstrapEvaluation = evaluateNetworkObservations(
      "bootstrap",
      bootstrapNetwork,
      defaultNetworkAllowList,
    );
    process.stdout.write(
      `  ${bootstrapEvaluation.passed ? "PASS" : "FAIL"} bootstrap network (reqs=${bootstrapNetwork.length}, errors=${bootstrapEvaluation.failures.length})\n`,
    );
    for (const failure of bootstrapEvaluation.failures) {
      process.stdout.write(`        - ${failure.code}: ${failure.detail}\n`);
    }

    const authCoverage = createAuthRouteCoverage(consoleRoutes, authMode);
    process.stdout.write(formatAuthRouteCoverage(authCoverage));
    const proofFetcher =
      authMode === "browser_session" ? createBrowserSessionFetcher(page) : globalThis.fetch;
    const proofAPIKey = authMode === "browser_session" ? "" : apiKey;
    const indexedRepositoryInventory = await loadIndexedRepositoryInventoryAnchor(
      apiBase,
      proofAPIKey,
      proofFetcher,
    );
    assertProofManifestRepositoryCount(proofManifest, indexedRepositoryInventory.total);
    const loadImpactFindings = createImpactFindingsLoader(apiBase, proofAPIKey, proofFetcher);

    const results: RouteResult[] = [];
    const observations = [];
    for (const route of authCoverage.eligibleRoutes) {
      const signals = await captureRoute(
        page,
        route,
        tracker,
        networkRecorder.observations,
        bootstrapNetwork,
        indexedRepositoryInventory,
        loadImpactFindings,
      );
      observations.push(buildRouteReportObservation(signals));
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
    networkRecorder.stop();
    await browser.close();
    browser = undefined;

    const summary = summarizeGate(results, [bootstrapEvaluation]);
    await writeFile(
      reportPath,
      JSON.stringify(
        {
          apiBase,
          proofManifest,
          ...authCoverageReport(authCoverage),
          bootstrap: {
            apiQuiet: bootQuiet,
            requestCount: bootstrapNetwork.length,
            ...buildNetworkReportObservation(bootstrapNetwork),
            verdict: bootstrapEvaluation,
          },
          observations,
          summary,
        },
        null,
        2,
      ),
      "utf8",
    );
    process.stdout.write(
      `console-live-e2e: ${summary.passedCount}/${summary.total} routes passed; report ${reportPath}\n`,
    );

    if (!summary.passed) {
      process.stderr.write(
        `console-live-e2e: ${summary.failedCount} route(s) and ${summary.preflightFailureCount} preflight request(s) failed the live gate\n`,
      );
      return 1;
    }
    return 0;
  } finally {
    if (browser) {
      await browser.close().catch(() => undefined);
    }
    if (server) await stopLiveE2EDevServer(server);
  }
}
