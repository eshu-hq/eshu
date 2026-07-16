import type { Page } from "playwright";

interface BrowserFetchResult {
  readonly body: string;
  readonly headers: readonly (readonly [string, string])[];
  readonly status: number;
  readonly statusText: string;
}

interface BrowserFetchInput {
  readonly body: string | null;
  readonly headers: readonly (readonly [string, string])[];
  readonly method: string;
  readonly target: string;
  readonly timeoutMs: number;
}

function inputURL(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

function serializableBody(body: BodyInit | null | undefined): string | null {
  if (body === null || body === undefined) return null;
  if (typeof body === "string") return body;
  if (body instanceof URLSearchParams) return body.toString();
  throw new Error("browser-session proof fetcher supports only string request bodies");
}

async function evaluateBrowserFetch(
  page: Page,
  input: BrowserFetchInput,
): Promise<BrowserFetchResult> {
  return page.evaluate(async (request) => {
    const response = await fetch(request.target, {
      body: request.body,
      credentials: "include",
      headers: Object.fromEntries(request.headers),
      method: request.method,
      signal: AbortSignal.timeout(request.timeoutMs),
    });
    return {
      body: await response.text(),
      headers: [...response.headers.entries()],
      status: response.status,
      statusText: response.statusText,
    };
  }, input);
}

// createBrowserSessionFetcher keeps authoritative comparator reads on the
// console origin so they use the same HttpOnly wizard-session cookie as route
// actions. It returns a normal Response to reuse the existing proof parsers.
export function createBrowserSessionFetcher(
  page: Page,
  proxyPrefix = "/eshu-api",
  timeoutMs = 15_000,
): typeof fetch {
  return async (input, init) => {
    const parsed = new URL(inputURL(input), "http://console.invalid");
    const prefix = proxyPrefix.endsWith("/") ? proxyPrefix.slice(0, -1) : proxyPrefix;
    const request: BrowserFetchInput = {
      body: serializableBody(init?.body),
      headers: [...new Headers(init?.headers).entries()],
      method: init?.method ?? "GET",
      target: `${prefix}${parsed.pathname}${parsed.search}`,
      timeoutMs,
    };
    if (init?.signal?.aborted) throw init.signal.reason;

    let abortListener: (() => void) | undefined;
    const aborted = init?.signal
      ? new Promise<never>((_resolve, reject) => {
          abortListener = () => reject(init.signal?.reason ?? new Error("request aborted"));
          init.signal?.addEventListener("abort", abortListener, { once: true });
        })
      : null;
    try {
      const result = aborted
        ? await Promise.race([evaluateBrowserFetch(page, request), aborted])
        : await evaluateBrowserFetch(page, request);
      return new Response(result.body, {
        headers: Object.fromEntries(result.headers),
        status: result.status,
        statusText: result.statusText,
      });
    } finally {
      if (abortListener) init?.signal?.removeEventListener("abort", abortListener);
    }
  };
}
