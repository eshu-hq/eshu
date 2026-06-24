import { render, screen, waitFor, within } from "@testing-library/react";

import { ObservabilityPage } from "./ObservabilityPage";
import type { EshuApiClient } from "../api/client";

describe("ObservabilityPage", () => {
  it("keeps provider empty state hidden while coverage is loading", () => {
    const client = {
      getJson: () => new Promise(() => {})
    } as unknown as EshuApiClient;

    render(<ObservabilityPage client={client} />);

    expect(screen.getByText(/provider anchors: grafana, prometheus, loki, tempo/i)).toBeInTheDocument();
    expect(screen.getAllByText("Loading observability coverage...").length).toBeGreaterThan(0);
    expect(screen.queryByText(/No observability coverage/)).not.toBeInTheDocument();
  });

  it("labels empty coverage as empty rather than live", async () => {
    const client = {
      getJson: async () => ({ correlations: [], truncated: false })
    } as unknown as EshuApiClient;

    render(<ObservabilityPage client={client} />);

    await waitFor(() => expect(screen.getAllByText("empty").length).toBeGreaterThan(0));
    expect(screen.queryByText("live")).not.toBeInTheDocument();
  });

  it("renders demo-style signal sources and coverage matrix from live correlations", async () => {
    const client = {
      getJson: async (path: string) => {
        if (path.includes("provider=grafana")) {
          return {
            correlations: [{
              correlation_id: "c1",
              provider: "grafana",
              coverage_signal: "dashboard",
              observability_object_ref: "grafana:svc-catalog-dashboard",
              target_service_ref: "svc-catalog",
              coverage_status: "covered",
              resource_class: "service",
              source_kind: "grafana",
              freshness_state: "fresh"
            }],
            truncated: false
          };
        }
        if (path.includes("provider=loki")) {
          return {
            correlations: [{
              correlation_id: "c2",
              provider: "loki",
              coverage_signal: "logs",
              observability_object_ref: "loki:svc-catalog-logs",
              target_service_ref: "svc-catalog",
              coverage_status: "stale",
              resource_class: "service",
              source_kind: "loki",
              freshness_state: "stale"
            }],
            truncated: false
          };
        }
        return { correlations: [], truncated: false };
      }
    } as unknown as EshuApiClient;

    render(<ObservabilityPage client={client} />);

    expect(await screen.findByText("Signal sources")).toBeInTheDocument();
    const matrixPanel = screen.getByRole("heading", { name: "Coverage matrix" }).closest("section");
    expect(matrixPanel).not.toBeNull();
    expect(within(matrixPanel as HTMLElement).getByText("svc-catalog")).toBeInTheDocument();
    expect(within(matrixPanel as HTMLElement).queryByText("grafana:svc-catalog-dashboard")).not.toBeInTheDocument();
    expect(screen.getByText("loki:svc-catalog-logs")).toBeInTheDocument();
    expect(screen.getAllByText("dashboard").length).toBeGreaterThan(0);
    expect(screen.getAllByText("logs").length).toBeGreaterThan(0);
  });

  it("keeps partial provider failures distinct from every provider being unavailable", async () => {
    const client = {
      getJson: async (path: string) => {
        if (path.includes("provider=tempo")) throw new Error("tempo down");
        return { correlations: [], truncated: false };
      }
    } as unknown as EshuApiClient;

    render(<ObservabilityPage client={client} />);

    expect(await screen.findByText("Some observability providers are unavailable; no coverage rows were returned yet.")).toBeInTheDocument();
    expect(screen.queryByText("Observability coverage is unavailable for every provider.")).not.toBeInTheDocument();
  });
});
