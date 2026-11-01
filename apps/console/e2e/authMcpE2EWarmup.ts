// authMcpE2EWarmup.ts — the one bounded retry the MCP-identity E2E allows for
// core's OIDC provider snapshot.
//
// Core builds its OIDC bearer snapshot at boot and refreshes it in the
// background at most every 30 seconds, and only when a request notices it is
// stale (go/internal/oidcbearer/cache.go). A provider created in the console
// after boot is invisible to the first JWT request that reaches core: that
// request is answered `unauthenticated` and starts the refresh, and the next
// one succeeds. The satellite forwards core's answer inside a 200 tools/call
// result, so the E2E sees the error in the envelope, not in the HTTP status.
//
// This retries ONLY that answer. Any other error, and a still-unauthenticated
// answer after the budget, is returned to the caller to fail on, so the retry
// cannot turn a real denial into a pass. Use it only on a positive-path call.

// WarmupOptions bounds the retry. attempts counts calls, so attempts=1 never
// retries. sleep is injectable so the tests need no real delay.
export interface WarmupOptions {
  readonly attempts: number;
  readonly delayMs: number;
  readonly sleep?: (ms: number) => Promise<void>;
}

// WarmupResult is the last envelope seen and how many calls it took.
export interface WarmupResult {
  readonly structured: unknown;
  readonly attempts: number;
}

const realSleep = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

// isUnauthenticatedEnvelope reports whether a tools/call truth envelope
// carries core's `unauthenticated` error. Anything that is not an object with
// an `error.code` of exactly that is false.
export function isUnauthenticatedEnvelope(structured: unknown): boolean {
  if (structured === null || typeof structured !== "object") {
    return false;
  }
  const error = (structured as { error?: unknown }).error;
  if (error === null || typeof error !== "object") {
    return false;
  }
  return (error as { code?: unknown }).code === "unauthenticated";
}

// retryUntilAuthenticated calls `call` until its envelope is not
// `unauthenticated` or the attempt budget is spent, sleeping delayMs between
// calls. It returns the last envelope without judging it.
export async function retryUntilAuthenticated(
  call: () => Promise<unknown>,
  options: WarmupOptions,
): Promise<WarmupResult> {
  if (!Number.isInteger(options.attempts) || options.attempts < 1) {
    throw new Error(`retryUntilAuthenticated: attempts must be an integer of at least 1, got ${options.attempts}`);
  }
  const sleep = options.sleep ?? realSleep;
  let structured: unknown;
  for (let attempt = 1; attempt <= options.attempts; attempt += 1) {
    structured = await call();
    if (!isUnauthenticatedEnvelope(structured) || attempt === options.attempts) {
      return { structured, attempts: attempt };
    }
    await sleep(options.delayMs);
  }
  // Unreachable: the loop always returns on its last attempt.
  return { structured, attempts: options.attempts };
}
