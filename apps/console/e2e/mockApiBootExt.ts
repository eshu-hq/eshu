// mockApiBootExt.ts — Overflow handlers for boot-critical API mocks.

import type { Route } from "playwright";

import { envelope } from "./mockApi.ts";

function tsParams(metric: string): number[] {
  const n = 48;
  return Array.from(
    { length: n },
    (_, index) => 100 + Math.round(Math.sin(index / 5) * 30 + metric.length * 7),
  );
}

export async function handleBootRouteExt(
  route: Route,
  path: string,
  method: string,
  params: URLSearchParams,
): Promise<boolean> {
  if (method === "GET" && path === "/api/v0/supply-chain/advisories") {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            advisories: [
              {
                id: "GHSA-xxxx-xxxx-xxxx",
                package: "express",
                severity: "high",
                cvss: 7.5,
                summary: "Prototype pollution",
              },
            ],
            count: 1,
            limit: 50,
            truncated: false,
          },
          "advisories.list",
        ),
      ),
    });
    return true;
  }

  if (method === "GET" && path === "/api/v0/status/collector-readiness") {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          [
            {
              kind: "git",
              instance_id: "git-primary",
              mode: "primary",
              enabled: true,
              ready: true,
              last_observed_at: new Date().toISOString(),
            },
            {
              kind: "kubernetes",
              instance_id: "k8s-observer",
              mode: "primary",
              enabled: true,
              ready: true,
              last_observed_at: new Date().toISOString(),
            },
          ],
          "status.collector_readiness",
        ),
      ),
    });
    return true;
  }

  if (method === "GET" && path === "/api/v0/metrics/timeseries") {
    const metric = params.get("metric") ?? "unknown";
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        envelope(
          {
            metric,
            values: tsParams(metric).map((value, index) => ({
              ts: new Date(Date.now() - (47 - index) * 1800000).toISOString(),
              value,
            })),
          },
          "metrics.timeseries",
        ),
      ),
    });
    return true;
  }

  return false;
}
