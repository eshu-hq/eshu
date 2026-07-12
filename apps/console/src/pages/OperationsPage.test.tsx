import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { OperationsPage } from "./OperationsPage";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import { emptySeries } from "../console/liveModel";

describe("OperationsPage", () => {
  it("labels repository language inventory with the aggregate endpoint", () => {
    render(<OperationsPage model={demoModel} />);

    expect(screen.getByText("GET /api/v0/repositories/language-inventory")).toBeInTheDocument();
    expect(screen.queryByText("GET /api/v0/repositories/by-language")).not.toBeInTheDocument();
  });

  it("renders live query latency series when metrics samples are available", () => {
    render(
      <OperationsPage
        model={{
          ...demoModel,
          series: {
            ...demoModel.series,
            deadLetters: [0, 1],
            queryP50: [3],
            queryP95: [7],
            queryP99: [11],
          },
        }}
      />,
    );

    expect(screen.getByText("Query latency")).toBeInTheDocument();
    expect(screen.getByText("p50 3ms · p95 7ms · p99 11ms")).toBeInTheDocument();
  });

  it("renders live graph growth series when metrics samples are available", () => {
    render(
      <OperationsPage
        model={{
          ...demoModel,
          series: {
            ...demoModel.series,
            graphNodes: [41000, 41120],
            graphEdges: [128000, 129200],
          },
        }}
      />,
    );

    expect(screen.getByText("Graph growth")).toBeInTheDocument();
    expect(screen.getByText("41.1k nodes · 129.2k edges")).toBeInTheDocument();
  });

  it("keeps demo-only metric decorations out of live Operations", () => {
    render(<OperationsPage model={{ ...demoModel, source: "live" }} />);

    expect(screen.getByText("Metric contract pending")).toBeInTheDocument();
    expect(screen.getByText("Tracked in issue #2216")).toBeInTheDocument();
    expect(
      screen.getByText(/write-throughput, cache-hit, and vulnerability-feed intake/),
    ).toBeInTheDocument();
  });

  it("shows 'no history yet' placeholder when metrics source is configured but has no samples", () => {
    render(
      <OperationsPage
        model={{
          ...demoModel,
          series: { ...emptySeries, metricsConfigured: true },
        }}
      />,
    );

    expect(
      screen.getAllByText(/Trend history appears when the metrics source has recent samples/),
    ).not.toHaveLength(0);
    expect(screen.queryByText(/Metrics source not configured/)).not.toBeInTheDocument();
  });

  it("shows explicit 'not configured' message when metrics source is absent", () => {
    render(
      <OperationsPage
        model={{
          ...demoModel,
          series: { ...emptySeries, metricsConfigured: false },
        }}
      />,
    );

    const notConfigured = screen.getAllByText(/Metrics source not configured/);
    expect(notConfigured.length).toBeGreaterThan(0);
    expect(
      screen.queryByText(/Trend history appears when the metrics source has recent samples/),
    ).not.toBeInTheDocument();
  });

  it("renders ArgoCD deployed workloads grid when apps are present", () => {
    render(<OperationsPage model={demoModel} />);

    expect(screen.getByText("ArgoCD deployed workloads")).toBeInTheDocument();
    expect(screen.getByText(/4 apps/)).toBeInTheDocument();
    expect(screen.getByText(/3 source-indexed/)).toBeInTheDocument();
    expect(screen.getByText("checkout-app")).toBeInTheDocument();
    expect(screen.getByText("external-app")).toBeInTheDocument();
  });

  it("renders indexed and not-indexed pills for ArgoCD apps", () => {
    render(<OperationsPage model={demoModel} />);

    const indexedPills = screen.getAllByText("indexed");
    const notIndexedPills = screen.getAllByText("not indexed");
    expect(indexedPills.length).toBe(3);
    expect(notIndexedPills.length).toBe(1);
  });

  it("shows empty ArgoCD state for live source with live provenance but zero apps", () => {
    render(
      <OperationsPage
        model={{
          ...demoModel,
          source: "live",
          argoCDApps: [],
          provenance: { ...demoModel.provenance, argoCDApps: "live" },
        }}
      />,
    );

    expect(
      screen.getByText(/No ArgoCD Application or ApplicationSet nodes found/),
    ).toBeInTheDocument();
  });

  it("hides ArgoCD panel for demo source with no apps", () => {
    render(
      <OperationsPage
        model={{
          ...demoModel,
          argoCDApps: [],
          provenance: { ...demoModel.provenance, argoCDApps: "demo" },
        }}
      />,
    );

    expect(screen.queryByText("ArgoCD deployed workloads")).not.toBeInTheDocument();
  });
});

// The live operations board (issue #5137) polls GET /api/v0/status/operations
// independently of the model prop. These tests mirror StatusPage.test.tsx's
// path-switching mock client + small injected pollMs + waitFor convention.
describe("OperationsPage live operations board", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  function operationsWire(overrides: Record<string, unknown> = {}): unknown {
    return {
      version: "test",
      as_of: "2026-06-21T12:00:00Z",
      scoped: false,
      health: { state: "healthy", reasons: [] },
      collectors: [],
      stage_summaries: [],
      queue: {
        outstanding: 0,
        in_flight: 0,
        retrying: 0,
        succeeded: 0,
        dead_letter: 0,
        failed: 0,
        overdue_claims: 0,
      },
      live_activity: [],
      truncated: false,
      limit: 50,
      ...overrides,
    };
  }

  function activityRow(overrides: Record<string, unknown> = {}): unknown {
    return {
      work_item_id: "wi-1",
      stage: "reducer",
      status: "running",
      domain: "repository:checkout-service",
      lease_owner: "reducer-1",
      claim_until: null,
      attempt_count: 1,
      updated_at: null,
      created_at: null,
      age_seconds: 30,
      scope_kind: "repository",
      collector_kind: "git",
      source_system: "github",
      source_key: "sample/checkout-service",
      ...overrides,
    };
  }

  function opsClient(responses: readonly unknown[]): EshuApiClient {
    let call = 0;
    return {
      get: async (path: string) => {
        if (!path.includes("/status/operations")) throw new Error(`unexpected get ${path}`);
        const idx = Math.min(call, responses.length - 1);
        call += 1;
        return { data: responses[idx], error: null, truth: null };
      },
    } as unknown as EshuApiClient;
  }

  it("polls the mock client and renders a live_activity row, then renders updated rows after the next poll", async () => {
    const first = operationsWire({ live_activity: [activityRow()] });
    const second = operationsWire({
      live_activity: [
        activityRow({
          work_item_id: "wi-2",
          stage: "projector",
          status: "claimed",
          domain: "repository:payments-api",
          lease_owner: "projector-2",
          source_key: "sample/payments-api",
        }),
      ],
    });
    const client = opsClient([first, second]);

    render(<OperationsPage model={demoModel} client={client} pollMs={50} />);

    expect(await screen.findByText("sample/checkout-service")).toBeInTheDocument();

    await waitFor(() => expect(screen.getByText("sample/payments-api")).toBeInTheDocument(), {
      timeout: 2000,
    });
    expect(screen.queryByText("sample/checkout-service")).not.toBeInTheDocument();
  });

  it("renders scoped rows safely with an em dash for redacted repo/worker identity", async () => {
    const client = opsClient([
      operationsWire({
        scoped: true,
        live_activity: [activityRow({ lease_owner: null, source_key: null })],
      }),
    ]);
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />);

    await screen.findByText("running");
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });

  it("shows the explicit empty state when there is no in-flight work", async () => {
    const client = opsClient([operationsWire()]);
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />);

    expect(await screen.findByText("No in-flight work — pipeline idle")).toBeInTheDocument();
  });

  it("degrades the live board gracefully but keeps rendering the rest of the page when the endpoint is unavailable", async () => {
    const client = {
      get: async () => {
        throw new Error("offline");
      },
    } as unknown as EshuApiClient;
    render(<OperationsPage model={demoModel} client={client} pollMs={50000} />);

    expect(await screen.findByText(/Live operations board is unavailable/i)).toBeInTheDocument();
    expect(screen.getByText("GET /api/v0/repositories/language-inventory")).toBeInTheDocument();
  });
});
