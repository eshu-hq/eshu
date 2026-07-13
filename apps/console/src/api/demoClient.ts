import { EshuApiClient, type EshuFetcher } from "./client";
import { demoApiBaseUrl } from "./demoFixtures";
import type { EshuEnvelope, EshuTruth } from "./envelope";
export { demoApiBaseUrl, demoDefaults, demoRepositories } from "./demoFixtures";

export function createDemoApiClient(): EshuApiClient {
  return new EshuApiClient({
    baseUrl: demoApiBaseUrl,
    fetcher: demoFetcher,
    timeoutMs: 0,
  });
}

const demoFetcher: EshuFetcher = async (input, init) => {
  const request = new Request(input, init);
  const url = new URL(request.url);
  const path = stripDemoBase(url.pathname);
  const body = await requestBody(request);
  // /freshness (issue #5143) is dispatched separately, via a dynamic import
  // of demoFreshnessFixture.ts, ahead of the demoRouter.ts dispatch below.
  // Both the freshness fixture and the general dispatch table are dynamic
  // imports (issue #5139): demoClient.ts and demoFixtures.ts are imported
  // eagerly from App.tsx, so a static import of the dispatch table or any
  // per-surface fixture would add its weight to the console's tightly
  // budgeted main bundle (scripts/console-bundle-budget.mjs) even though most
  // demo sessions only ever touch a handful of surfaces.
  if (request.method === "GET" && path.endsWith("/freshness")) {
    const { demoFreshnessWire } = await import("./demoFreshnessFixture");
    return Response.json(
      envelope(demoFreshnessWire(repoIdFromFreshnessPath(path)), "repositories.freshness"),
    );
  }
  const { demoResponse } = await import("./demoRouter");
  const result = await demoResponse(path, request.method, url.searchParams, body);
  if (result === null) {
    return Response.json(
      envelope(null, "demo.missing", {
        code: "demo_fixture_not_found",
        message: `Demo fixture does not cover ${request.method} ${path}`,
      }),
      { status: 404 },
    );
  }
  return Response.json(envelope(result.data, result.capability, result.error ?? null));
};

async function requestBody(request: Request): Promise<unknown> {
  if (request.method === "GET" || request.method === "HEAD") {
    return null;
  }
  const text = await request.text();
  if (text.trim().length === 0) {
    return null;
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return null;
  }
}

function envelope<TData>(
  data: TData,
  capability: string,
  error: EshuEnvelope<TData>["error"] = null,
): EshuEnvelope<TData> {
  return {
    data: error === null ? data : null,
    error,
    truth: error === null ? demoTruth(capability) : null,
  };
}

function demoTruth(capability: string): EshuTruth {
  return {
    basis: "demo_fixture",
    capability,
    freshness: { state: "fresh" },
    level: "exact",
    profile: "demo_fixture",
    reason: "Prospect demo fixture corpus; not live workspace data.",
  };
}

function stripDemoBase(pathname: string): string {
  return pathname.startsWith(demoApiBaseUrl.slice(0, -1))
    ? pathname.slice(demoApiBaseUrl.length - 1)
    : pathname;
}

function repoIdFromFreshnessPath(path: string): string {
  return decodeURIComponent(path.replace("/api/v0/repositories/", "").replace("/freshness", ""));
}
