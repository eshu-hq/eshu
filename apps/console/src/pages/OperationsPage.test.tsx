import { render, screen } from "@testing-library/react";

import { OperationsPage } from "./OperationsPage";
import { demoModel } from "../console/demoModel";
import { emptySeries } from "../console/liveModel";

describe("OperationsPage", () => {
  it("labels repository language inventory with the aggregate endpoint", () => {
    render(<OperationsPage model={demoModel} />);

    expect(screen.getByText("GET /api/v0/repositories/language-inventory")).toBeInTheDocument();
    expect(screen.queryByText("GET /api/v0/repositories/by-language")).not.toBeInTheDocument();
  });

  it("renders live query latency series when metrics samples are available", () => {
    render(<OperationsPage model={{
      ...demoModel,
      series: {
        ...demoModel.series,
        deadLetters: [0, 1],
        queryP50: [3],
        queryP95: [7],
        queryP99: [11]
      }
    }} />);

    expect(screen.getByText("Query latency")).toBeInTheDocument();
    expect(screen.getByText("p50 3ms · p95 7ms · p99 11ms")).toBeInTheDocument();
  });

  it("renders live graph growth series when metrics samples are available", () => {
    render(<OperationsPage model={{
      ...demoModel,
      series: {
        ...demoModel.series,
        graphNodes: [41000, 41120],
        graphEdges: [128000, 129200]
      }
    }} />);

    expect(screen.getByText("Graph growth")).toBeInTheDocument();
    expect(screen.getByText("41.1k nodes · 129.2k edges")).toBeInTheDocument();
  });

  it("keeps demo-only metric decorations out of live Operations", () => {
    render(<OperationsPage model={{ ...demoModel, source: "live" }} />);

    expect(screen.getByText("Metric contract pending")).toBeInTheDocument();
    expect(screen.getByText("Tracked in issue #2216")).toBeInTheDocument();
    expect(screen.getByText(/write-throughput, cache-hit, and vulnerability-feed intake/)).toBeInTheDocument();
  });

  it("shows 'no history yet' placeholder when metrics source is configured but has no samples", () => {
    render(<OperationsPage model={{
      ...demoModel,
      series: { ...emptySeries, metricsConfigured: true }
    }} />);

    expect(screen.getAllByText(/Trend history appears when the metrics source has recent samples/)).not.toHaveLength(0);
    expect(screen.queryByText(/Metrics source not configured/)).not.toBeInTheDocument();
  });

  it("shows explicit 'not configured' message when metrics source is absent", () => {
    render(<OperationsPage model={{
      ...demoModel,
      series: { ...emptySeries, metricsConfigured: false }
    }} />);

    const notConfigured = screen.getAllByText(/Metrics source not configured/);
    expect(notConfigured.length).toBeGreaterThan(0);
    expect(screen.queryByText(/Trend history appears when the metrics source has recent samples/)).not.toBeInTheDocument();
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
    render(<OperationsPage model={{
      ...demoModel,
      source: "live",
      argoCDApps: [],
      provenance: { ...demoModel.provenance, argoCDApps: "live" }
    }} />);

    expect(screen.getByText(/No ArgoCD Application or ApplicationSet nodes found/)).toBeInTheDocument();
  });

  it("hides ArgoCD panel for demo source with no apps", () => {
    render(<OperationsPage model={{
      ...demoModel,
      argoCDApps: [],
      provenance: { ...demoModel.provenance, argoCDApps: "demo" }
    }} />);

    expect(screen.queryByText("ArgoCD deployed workloads")).not.toBeInTheDocument();
  });
});
