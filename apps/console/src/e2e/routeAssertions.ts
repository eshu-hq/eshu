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
  WorkflowEmptyState,
  WorkflowFollowLink,
  WorkflowField,
  WorkflowResponseExpectation,
} from "./consoleRouteCatalog";

export interface RouteWorkflowObservation {
  readonly id: string;
  readonly passed: boolean;
  readonly detail: string;
  readonly dataShapes?: readonly WorkflowDataShapeObservation[];
  readonly requests?: readonly WorkflowRequestObservation[];
}

export interface WorkflowDataShapeObservation {
  readonly selector: string;
  readonly visibleCount: number;
}

export interface WorkflowRequestObservation {
  readonly method: string;
  readonly pathname: string;
  readonly status: number;
  readonly phase?: "bootstrap" | "route";
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

// NetworkEvaluation is the verdict for one bounded request-owning phase. The
// bootstrap phase uses this directly; route evaluation reuses the same rules.
export interface NetworkEvaluation {
  readonly scope: string;
  readonly passed: boolean;
  readonly failures: readonly RouteFailure[];
  readonly allowedNonOk: readonly AllowedNonOk[];
}

export interface RouteReportNetworkObservation {
  readonly pathname: string;
  readonly method: string;
  readonly status: number;
  readonly failureText: string | null;
}

export interface RouteReportObservation {
  readonly path: string;
  readonly durationMs: number;
  readonly mainContentChars: number;
  readonly apiQuiet: ApiQuietObservation | null;
  readonly network: readonly RouteReportNetworkObservation[];
  readonly networkTruncated: boolean;
  readonly workflow: RouteWorkflowObservation | null;
}

export interface NetworkReportObservation {
  readonly network: readonly RouteReportNetworkObservation[];
  readonly networkTruncated: boolean;
}

const maxReportedRequestsPerRoute = 200;

// buildNetworkReportObservation retains bounded, query-free transport proof
// for request phases that are not owned by a route, such as initial app boot.
export function buildNetworkReportObservation(
  network: readonly NetworkObservation[],
): NetworkReportObservation {
  return {
    network: network.slice(0, maxReportedRequestsPerRoute).map((observation) => ({
      failureText:
        observation.failureText === null ? null : redactUrlQueries(observation.failureText),
      method: observation.method,
      pathname: safePathname(observation.url),
      status: observation.status,
    })),
    networkTruncated: network.length > maxReportedRequestsPerRoute,
  };
}

// buildRouteReportObservation retains the bounded request/status and visible
// data-shape proof used for the verdict. URLs are reduced to pathnames so query
// values, headers, response bodies, and credentials never enter the artifact.
export function buildRouteReportObservation(signals: RouteSignals): RouteReportObservation {
  const networkReport = buildNetworkReportObservation(signals.network);
  return {
    apiQuiet: signals.apiQuiet ?? null,
    durationMs: signals.durationMs,
    mainContentChars: signals.mainContentChars,
    ...networkReport,
    path: signals.route.path,
    workflow: signals.workflow,
  };
}

function safePathname(url: string): string {
  try {
    return new URL(url).pathname;
  } catch {
    return "invalid-url";
  }
}

function redactUrlQueries(value: string): string {
  return value.replace(/https?:\/\/[^\s)]+/g, (url) => safePathname(url));
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

// evaluateNetworkObservations applies the exact route network policy to any
// request-owning phase. Transport failures are never allow-listed.
export function evaluateNetworkObservations(
  scope: string,
  network: readonly NetworkObservation[],
  allowList: readonly NetworkAllowRule[] = defaultNetworkAllowList,
): NetworkEvaluation {
  const failures: RouteFailure[] = [];
  const allowedNonOk: AllowedNonOk[] = [];

  for (const observation of network) {
    if (isOkStatus(observation.status) && observation.failureText === null) {
      continue;
    }
    const rule = allowList.find((candidate) => matchAllowRule(observation, candidate));
    if (rule && observation.failureText === null) {
      allowedNonOk.push({
        url: safePathname(observation.url),
        method: observation.method,
        status: observation.status,
        reason: rule.reason,
      });
      continue;
    }
    const failureSuffix =
      observation.failureText === null
        ? `status ${observation.status}`
        : `network failure ${redactUrlQueries(observation.failureText)}`;
    failures.push({
      code: "unexpected_network",
      detail: `${scope} issued an unexpected request: ${observation.method} ${safePathname(observation.url)} (${failureSuffix})`,
    });
  }

  return { scope, passed: failures.length === 0, failures, allowedNonOk };
}

// evaluateRoute applies every gate rule to one route's captured signals and
// returns a structured pass/fail result. It never throws; an empty failures
// array means the route passed.
export function evaluateRoute(
  signals: RouteSignals,
  allowList: readonly NetworkAllowRule[] = defaultNetworkAllowList,
): RouteResult {
  const failures: RouteFailure[] = [];

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
      detail: `route ${signals.route.path} logged a browser console error: ${redactUrlQueries(message)}`,
    });
  }

  const network = evaluateNetworkObservations(
    `route ${signals.route.path}`,
    signals.network,
    allowList,
  );
  failures.push(...network.failures);

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
    allowedNonOk: network.allowedNonOk,
  };
}

// GateSummary aggregates per-route results into an overall verdict.
export interface GateSummary {
  readonly passed: boolean;
  readonly total: number;
  readonly passedCount: number;
  readonly failedCount: number;
  readonly preflightPassed: boolean;
  readonly preflightFailureCount: number;
  readonly preflight: readonly NetworkEvaluation[];
  readonly results: readonly RouteResult[];
}

// summarizeGate folds route results into a single verdict. The gate passes only
// when every route passed; there is no partial-pass fallback.
export function summarizeGate(
  results: readonly RouteResult[],
  preflight: readonly NetworkEvaluation[] = [],
): GateSummary {
  const passedCount = results.filter((result) => result.passed).length;
  const preflightFailureCount = preflight.reduce(
    (count, result) => count + result.failures.length,
    0,
  );
  return {
    passed: results.length > 0 && passedCount === results.length && preflightFailureCount === 0,
    total: results.length,
    passedCount,
    failedCount: results.length - passedCount,
    preflightPassed: preflightFailureCount === 0,
    preflightFailureCount,
    preflight,
    results,
  };
}
