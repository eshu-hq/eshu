import { eshuDefaultTimeoutMs } from "../src/api/client.ts";

const apiTimeoutGraceMs = 3_000;

// apiQuietPolicy keeps route ownership deterministic. The harness must remain
// on a route long enough for EshuApiClient's own timeout to abort a slow
// request, plus a grace window for Playwright's requestfailed event to settle.
export const apiQuietPolicy = Object.freeze({
  maxWaitMs: eshuDefaultTimeoutMs + apiTimeoutGraceMs,
  pollMs: 100,
  quietWindowMs: 600,
});

export interface ApiQuietTracker {
  readonly inFlight: () => number;
  readonly lastChangeAt: () => number;
}

export interface ApiQuietResult {
  readonly settled: boolean;
  readonly inFlight: number;
  readonly waitedMs: number;
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
