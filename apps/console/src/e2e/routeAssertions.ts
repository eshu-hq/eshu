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

// ConsoleRoute is one navigable console route the gate exercises. `path` is the
// in-app router path; `label` is the human name used in artifacts; `area`
// groups the route to the acceptance-criteria surface it proves.
export interface ConsoleRoute {
  readonly path: string;
  readonly label: string;
  readonly area: RouteArea;
}

export type RouteArea =
  | "dashboard"
  | "repositories"
  | "service"
  | "graph"
  | "cloud"
  | "observability"
  | "operations"
  | "security"
  | "ask";

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
}

// NetworkAllowRule justifies a specific expected non-2xx (or otherwise notable)
// response so the gate does not fail on it. A match requires the method, a URL
// substring, and the exact status; `reason` is recorded in the report.
export interface NetworkAllowRule {
  readonly method: string;
  readonly urlIncludes: string;
  readonly status: number;
  readonly reason: string;
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
  | "unexpected_network";

// RouteResult is the evaluated outcome for one route.
export interface RouteResult {
  readonly route: ConsoleRoute;
  readonly passed: boolean;
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

// consoleRoutes is the catalog the gate walks. Every entry is a real route
// enumerated from the console router in apps/console/src/App.tsx; the gate must
// not invent routes. Parameterized routes (e.g. /service-report/:serviceName)
// are exercised through their base listing route.
export const consoleRoutes: readonly ConsoleRoute[] = [
  { path: "/", label: "Dashboard", area: "dashboard" },
  { path: "/dashboard", label: "Dashboard (alias)", area: "dashboard" },
  { path: "/repositories", label: "Repositories", area: "repositories" },
  { path: "/catalog", label: "Service Catalog", area: "service" },
  { path: "/service-story", label: "Service Story", area: "service" },
  { path: "/service-report", label: "Service Report", area: "service" },
  { path: "/explorer", label: "Explorer", area: "graph" },
  { path: "/code-graph", label: "Code Graph", area: "graph" },
  { path: "/topology", label: "Topology", area: "graph" },
  { path: "/cloud", label: "Cloud", area: "cloud" },
  { path: "/cloud-drift", label: "Cloud Drift", area: "cloud" },
  { path: "/iac", label: "IaC", area: "cloud" },
  { path: "/images", label: "Images", area: "cloud" },
  { path: "/observability", label: "Observability", area: "observability" },
  { path: "/incidents", label: "Incidents", area: "observability" },
  { path: "/freshness-causality", label: "Freshness", area: "observability" },
  { path: "/operations", label: "Operations", area: "operations" },
  { path: "/collector-readiness", label: "Collector Readiness", area: "operations" },
  { path: "/capabilities", label: "Capabilities", area: "operations" },
  { path: "/surface-inventory", label: "Surface Inventory", area: "operations" },
  { path: "/findings", label: "Findings", area: "security" },
  { path: "/vulnerabilities", label: "Vulnerabilities", area: "security" },
  { path: "/secrets-iam", label: "Secrets & IAM", area: "security" },
  { path: "/sbom", label: "SBOM", area: "security" },
  { path: "/exposure", label: "Exposure Path", area: "security" },
  { path: "/dependencies", label: "Dependencies", area: "security" },
  { path: "/dead-code", label: "Dead Code", area: "graph" },
  { path: "/impact", label: "Impact", area: "graph" },
  { path: "/changed-since", label: "Changed Since", area: "graph" },
  { path: "/replatforming", label: "Replatforming", area: "service" },
  { path: "/ci-cd/run-correlations", label: "CI/CD Run Correlations", area: "operations" },
  { path: "/ask", label: "Ask Eshu", area: "ask" }
];

// defaultNetworkAllowList holds the only justified non-2xx responses. Each entry
// must name a concrete reason; an empty list means "every request must be 2xx".
// Populate this only with genuinely-expected non-200s discovered during a run,
// never to paper over a real failure.
export const defaultNetworkAllowList: readonly NetworkAllowRule[] = [];

function isOkStatus(status: number): boolean {
  // status 0 is Playwright's marker for a request with no HTTP response (it is
  // paired with failureText); treat only 2xx/3xx as ok.
  return status >= 200 && status < 400;
}

function matchAllowRule(
  observation: NetworkObservation,
  rule: NetworkAllowRule
): boolean {
  return (
    observation.method.toUpperCase() === rule.method.toUpperCase() &&
    observation.status === rule.status &&
    observation.url.includes(rule.urlIncludes)
  );
}

// evaluateRoute applies every gate rule to one route's captured signals and
// returns a structured pass/fail result. It never throws; an empty failures
// array means the route passed.
export function evaluateRoute(
  signals: RouteSignals,
  allowList: readonly NetworkAllowRule[] = defaultNetworkAllowList
): RouteResult {
  const failures: RouteFailure[] = [];
  const allowedNonOk: AllowedNonOk[] = [];

  if (!signals.connected) {
    failures.push({
      code: "not_connected",
      detail: `route ${signals.route.path} did not reach a connected live source (mode=${signals.sourceMode})`
    });
  }

  if (signals.sourceMode === "demo" || signals.demoBannerPresent) {
    failures.push({
      code: "demo_fallback",
      detail: `route ${signals.route.path} fell back to demo data (mode=${signals.sourceMode}, demoBanner=${String(signals.demoBannerPresent)})`
    });
  }

  if (signals.mainContentChars < minMainContentChars) {
    failures.push({
      code: "blank_render",
      detail: `route ${signals.route.path} rendered ${signals.mainContentChars} chars of main content (< ${minMainContentChars}); expected real data or an explicit empty/unavailable state`
    });
  }

  for (const message of signals.consoleErrors) {
    failures.push({
      code: "console_error",
      detail: `route ${signals.route.path} logged a browser console error: ${message}`
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
        reason: rule.reason
      });
      continue;
    }
    const failureSuffix = observation.failureText === null
      ? `status ${observation.status}`
      : `network failure ${observation.failureText}`;
    failures.push({
      code: "unexpected_network",
      detail: `route ${signals.route.path} issued an unexpected request: ${observation.method} ${observation.url} (${failureSuffix})`
    });
  }

  return {
    route: signals.route,
    passed: failures.length === 0,
    failures,
    allowedNonOk
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
    results
  };
}
