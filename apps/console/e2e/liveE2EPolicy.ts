import { eshuDefaultTimeoutMs } from "../src/api/client.ts";
import { defaultNetworkAllowList, type NetworkObservation } from "../src/e2e/routeAssertions.ts";
import type { Page } from "playwright";

const apiTimeoutGraceMs = 3_000;

export function allowedConsoleStatuses(network: readonly NetworkObservation[]): readonly number[] {
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

// apiQuietPolicy keeps route ownership deterministic. The harness must remain
// on a route long enough for EshuApiClient's own timeout to abort a slow
// request, plus a grace window for Playwright's requestfailed event to settle.
export const apiQuietPolicy = Object.freeze({
  absoluteMaxWaitMs: (eshuDefaultTimeoutMs + apiTimeoutGraceMs) * 2,
  maxWaitMs: eshuDefaultTimeoutMs + apiTimeoutGraceMs,
  pollMs: 100,
  quietWindowMs: 600,
});

export interface ApiQuietTracker {
  readonly inFlight: () => number;
  readonly lastChangeAt: () => number;
}

export interface NetworkObservationRecorder {
  readonly observations: NetworkObservation[];
  readonly stop: () => void;
}

export function installNetworkObservationRecorder(page: Page): NetworkObservationRecorder {
  const observations: NetworkObservation[] = [];
  const onResponse = (response: {
    url: () => string;
    status: () => number;
    request: () => { method: () => string };
  }): void => {
    observations.push({
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
    observations.push({
      url: request.url(),
      method: request.method(),
      status: 0,
      failureText: request.failure()?.errorText ?? "request failed",
    });
  };
  page.on("response", onResponse);
  page.on("requestfailed", onRequestFailed);
  return {
    observations,
    stop: () => {
      page.off("response", onResponse);
      page.off("requestfailed", onRequestFailed);
    },
  };
}

// installApiQuietTracker owns proxied API requests across route transitions so
// proof callers cannot leave a route while one of its requests is still able to
// fail. The page lifetime owns the listeners.
export function installApiQuietTracker(page: Page): ApiQuietTracker {
  let inFlight = 0;
  let lastChangeAt = Date.now();
  const settle = (url: string): void => {
    if (!isConsoleApiUrl(url)) return;
    inFlight = Math.max(0, inFlight - 1);
    lastChangeAt = Date.now();
  };
  page.on("request", (request) => {
    if (!isConsoleApiUrl(request.url())) return;
    inFlight += 1;
    lastChangeAt = Date.now();
  });
  page.on("requestfinished", (request) => settle(request.url()));
  page.on("requestfailed", (request) => settle(request.url()));
  return {
    inFlight: () => inFlight,
    lastChangeAt: () => lastChangeAt,
  };
}

export interface ApiQuietResult {
  readonly settled: boolean;
  readonly inFlight: number;
  readonly waitedMs: number;
}

type RouteStep = (path: string) => Promise<void>;

// navigateClientRoute preserves the connected console shell while changing
// routes. A document navigation would reboot the full snapshot for every page,
// multiply API fan-out, and attribute the previous page's aborted requests to
// the page being entered.
export async function navigateClientRoute(
  path: string,
  pushRoute: RouteStep,
  waitForRoute: RouteStep,
): Promise<void> {
  await pushRoute(path);
  await waitForRoute(path);
}

export async function awaitApiQuiet(
  tracker: ApiQuietTracker,
  wait: (durationMs: number) => Promise<void>,
  now: () => number = Date.now,
): Promise<ApiQuietResult> {
  const startedAt = now();
  for (;;) {
    const current = now();
    const idleFor = current - tracker.lastChangeAt();
    const inFlight = tracker.inFlight();
    if (inFlight === 0 && idleFor >= apiQuietPolicy.quietWindowMs) {
      return { settled: true, inFlight: 0, waitedMs: current - startedAt };
    }
    if (current - startedAt >= apiQuietPolicy.absoluteMaxWaitMs) {
      return { settled: false, inFlight, waitedMs: current - startedAt };
    }
    if (inFlight > 0 && idleFor >= apiQuietPolicy.maxWaitMs) {
      return { settled: false, inFlight, waitedMs: current - startedAt };
    }
    await wait(apiQuietPolicy.pollMs);
  }
}

export function filterAllowedResourceConsoleErrors(
  errors: readonly string[],
  allowedStatuses: readonly number[],
): readonly string[] {
  const remaining = new Map<number, number>();
  for (const status of allowedStatuses) {
    remaining.set(status, (remaining.get(status) ?? 0) + 1);
  }
  return errors.filter((message) => {
    if (!message.includes("Failed to load resource")) return true;
    const status = message.match(/\b(400|401)\b/)?.[1];
    if (!status) return true;
    const numericStatus = Number(status);
    const count = remaining.get(numericStatus) ?? 0;
    if (count === 0) return true;
    remaining.set(numericStatus, count - 1);
    return false;
  });
}

export function isConsoleApiUrl(url: string): boolean {
  return url.includes("/eshu-api/");
}

// Authenticated Playwright traces can retain request headers. Capture remains
// off unless an operator explicitly opts in for a locally protected artifact.
export function traceCaptureEnabled(value: string | undefined): boolean {
  const normalized = value?.trim().toLowerCase() ?? "";
  return normalized === "1" || normalized === "true";
}
