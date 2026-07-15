// Pure assertion logic for the console live E2E gate (issue #3326).
//
// This module owns the route catalog the gate walks and the rule engine that
// decides, from signals captured by the Playwright runner, whether a route
// passed. It is deliberately free of any browser, Playwright, or Node API so it
// can be unit-tested in isolation (the runner is the integration gate; this is
// its TDD-covered decision core).
//
// The gate proves the PRIVATE/LIVE console against the real local stack. The
// hard requirements it encodes:
//   - private mode connected to the live API (pill text "Live", not demo).
//   - no silent demo fallback (the "Prospect demo" / demo-fixtures banner must
//     be absent).
//   - no unhandled browser console errors.
//   - no unexpected failed network requests (only allow-listed non-2xx pass).
//   - each route renders either real data or an explicit empty/unavailable
//     state (substantive rendered content, never a blank crash).

import {
  defaultNetworkAllowList,
  type ConsoleRoute,
  type NetworkAllowRule,
} from "./consoleRouteCatalog";

export { consoleRoutes, defaultNetworkAllowList } from "./consoleRouteCatalog";
export type {
  ConsoleRoute,
  NetworkAllowRule,
  RouteArea,
  RouteWorkflowSpec,
  WorkflowField,
} from "./consoleRouteCatalog";

export interface RouteWorkflowObservation {
  readonly id: string;
  readonly passed: boolean;
  readonly detail: string;
}

// NetworkObservation is one network request the page issued, reduced to the
// fields the gate reasons about.
export interface NetworkObservation {
  readonly url: string;
  readonly method: string;
  readonly status: number;
  // failureText is set when the request never produced a response (DNS, abort,
  // connection refused). A non-null failureText is always unexpected.
  readonly failureText: string | null;
}

// RouteSignals is everything the runner captured for one route after it
// navigated and the page settled.
export interface RouteSignals {
  readonly route: ConsoleRoute;
  // connected is true when the source pill reports a connected state.
  readonly connected: boolean;
  // sourceMode is the mode the app believes it is in, read from the UI
  // ("live" | "demo" | other). Live mode is the only acceptable value.
  readonly sourceMode: string;
  // demoBannerPresent is true when the demo-fixtures provenance banner rendered.
  readonly demoBannerPresent: boolean;
  // mainContentChars is the trimmed text length of the route's main region. A
  // value at or above the substance threshold proves the route rendered real
  // data or an explicit empty/unavailable state rather than a blank crash.
  readonly mainContentChars: number;
  // consoleErrors are page-level console.error / uncaught error messages.
  readonly consoleErrors: readonly string[];
  // network is every request the page issued while on this route.
  readonly network: readonly NetworkObservation[];
  // workflow is the route-specific action or state proof, when configured.
  readonly workflow: RouteWorkflowObservation | null;
  readonly apiQuiet?: ApiQuietObservation;
  readonly durationMs: number;
}

export interface ApiQuietObservation {
  readonly settled: boolean;
  readonly inFlight: number;
  readonly waitedMs: number;
}

// RouteFailure is one reason a route did not pass, with a machine-usable code
// and a human explanation.
export interface RouteFailure {
  readonly code: FailureCode;
  readonly detail: string;
}

export type FailureCode =
  | "not_connected"
  | "demo_fallback"
  | "blank_render"
  | "console_error"
  | "api_not_quiet"
  | "unexpected_network"
  | "workflow_failed";

// RouteResult is the evaluated outcome for one route.
export interface RouteResult {
  readonly route: ConsoleRoute;
  readonly passed: boolean;
  readonly durationMs: number;
  readonly failures: readonly RouteFailure[];
  // allowedNonOk lists non-2xx responses that matched an allow rule, for the
  // report's justified-exceptions section.
  readonly allowedNonOk: readonly AllowedNonOk[];
}

export interface AllowedNonOk {
  readonly url: string;
  readonly method: string;
  readonly status: number;
  readonly reason: string;
}

// minMainContentChars is the substance threshold. Every real console route —
// even one showing only an empty/unavailable state — renders well over this
// many characters of chrome plus status copy. A blank crash leaves the main
// region effectively empty, so this cleanly separates "rendered something" from
// "rendered nothing".
export const minMainContentChars = 40;

function isOkStatus(status: number): boolean {
  // status 0 is Playwright's marker for a request with no HTTP response (it is
  // paired with failureText); treat only 2xx/3xx as ok.
  return status >= 200 && status < 400;
}

function matchAllowRule(observation: NetworkObservation, rule: NetworkAllowRule): boolean {
  let pathname = "";
  try {
    pathname = new URL(observation.url).pathname;
  } catch {
    return false;
  }
  return (
    observation.method.toUpperCase() === rule.method.toUpperCase() &&
    observation.status === rule.status &&
    pathname === rule.pathname
  );
}

// evaluateRoute applies every gate rule to one route's captured signals and
// returns a structured pass/fail result. It never throws; an empty failures
// array means the route passed.
export function evaluateRoute(
  signals: RouteSignals,
  allowList: readonly NetworkAllowRule[] = defaultNetworkAllowList,
): RouteResult {
  const failures: RouteFailure[] = [];
  const allowedNonOk: AllowedNonOk[] = [];

  if (!signals.connected) {
    failures.push({
      code: "not_connected",
      detail: `route ${signals.route.path} did not reach a connected live source (mode=${signals.sourceMode})`,
    });
  }

  if (signals.apiQuiet?.settled === false) {
    failures.push({
      code: "api_not_quiet",
      detail: `${signals.apiQuiet.inFlight} API request(s) remained active after ${signals.apiQuiet.waitedMs}ms`,
    });
  }

  if (signals.sourceMode === "demo" || signals.demoBannerPresent) {
    failures.push({
      code: "demo_fallback",
      detail: `route ${signals.route.path} fell back to demo data (mode=${signals.sourceMode}, demoBanner=${String(signals.demoBannerPresent)})`,
    });
  }

  if (signals.mainContentChars < minMainContentChars) {
    failures.push({
      code: "blank_render",
      detail: `route ${signals.route.path} rendered ${signals.mainContentChars} chars of main content (< ${minMainContentChars}); expected real data or an explicit empty/unavailable state`,
    });
  }

  for (const message of signals.consoleErrors) {
    failures.push({
      code: "console_error",
      detail: `route ${signals.route.path} logged a browser console error: ${message}`,
    });
  }

  for (const observation of signals.network) {
    if (isOkStatus(observation.status) && observation.failureText === null) {
      continue;
    }
    const rule = allowList.find((candidate) => matchAllowRule(observation, candidate));
    if (rule && observation.failureText === null) {
      allowedNonOk.push({
        url: observation.url,
        method: observation.method,
        status: observation.status,
        reason: rule.reason,
      });
      continue;
    }
    const failureSuffix =
      observation.failureText === null
        ? `status ${observation.status}`
        : `network failure ${observation.failureText}`;
    failures.push({
      code: "unexpected_network",
      detail: `route ${signals.route.path} issued an unexpected request: ${observation.method} ${observation.url} (${failureSuffix})`,
    });
  }

  if (signals.route.workflow) {
    const workflow = signals.workflow;
    if (workflow === null || workflow.id !== signals.route.workflow.id || !workflow.passed) {
      failures.push({
        code: "workflow_failed",
        detail:
          workflow === null
            ? `route ${signals.route.path} did not run workflow ${signals.route.workflow.id}`
            : `route ${signals.route.path} failed workflow ${signals.route.workflow.id}: ${workflow.detail}`,
      });
    }
  }

  return {
    route: signals.route,
    passed: failures.length === 0,
    durationMs: signals.durationMs,
    failures,
    allowedNonOk,
  };
}

// GateSummary aggregates per-route results into an overall verdict.
export interface GateSummary {
  readonly passed: boolean;
  readonly total: number;
  readonly passedCount: number;
  readonly failedCount: number;
  readonly results: readonly RouteResult[];
}

// summarizeGate folds route results into a single verdict. The gate passes only
// when every route passed; there is no partial-pass fallback.
export function summarizeGate(results: readonly RouteResult[]): GateSummary {
  const passedCount = results.filter((result) => result.passed).length;
  return {
    passed: results.length > 0 && passedCount === results.length,
    total: results.length,
    passedCount,
    failedCount: results.length - passedCount,
    results,
  };
}
