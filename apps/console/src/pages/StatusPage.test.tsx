import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { EshuApiClient } from "../api/client";
import { StatusPage } from "./StatusPage";

// The page joins three bounded status reads. The mock client answers each by
// path so the rendered hero %, the per-collector state badges + progress bars,
// and the live-refresh poll can be asserted without a live backend.
function statusClient(overrides?: { freshnessState?: string }): EshuApiClient {
  return {
    get: async (path: string) => {
      if (path.includes("/status/collector-readiness")) {
        return {
          data: {
            readiness: [
              { collector_kind: "git", display_name: "Git", instance_id: "git-1", claim_state: "claim_driven", promotion_state: "implemented" },
              { collector_kind: "sbom_attestation", display_name: "SBOM", instance_id: "sbom-1", promotion_state: "failed" },
              { collector_kind: "aws", display_name: "AWS Cloud", instance_id: "aws-1", promotion_state: "implemented" }
            ]
          },
          error: null,
          truth: { profile: "production", level: "exact", capability: "status.collector_readiness", freshness: { state: "fresh" } }
        };
      }
      if (path.includes("/status/freshness-causality")) {
        return {
          data: {
            state: overrides?.freshnessState ?? "building",
            generations: { active: 2, pending: 3, completed: 5, superseded: 0, failed: 0 },
            pending_projection: { outstanding: 12, dead_letter: 1, domains: 2 }
          },
          error: null,
          truth: null
        };
      }
      throw new Error(`unexpected get ${path}`);
    },
    getJson: async (path: string) => {
      if (path.includes("/index-status")) {
        return {
          status: "indexing",
          repository_count: 900,
          queue: { outstanding: 200, in_flight: 10, succeeded: 800, dead_letter: 1 },
          coordinator: {
            collector_instances: [
              { instance_id: "git-1", collector_kind: "git", enabled: true, last_observed_at: new Date(Date.now() - 40000).toISOString() },
              { instance_id: "sbom-1", collector_kind: "sbom_attestation", enabled: true, last_observed_at: new Date(Date.now() - 5000000).toISOString() },
              { instance_id: "aws-1", collector_kind: "aws", enabled: true, last_observed_at: new Date(Date.now() - 60000).toISOString() }
            ],
            collector_backpressure: [
              { collector_kind: "git", collector_instance_id: "git-1", pending: 34, claimed: 2, dead_letter: 0 },
              { collector_kind: "sbom_attestation", collector_instance_id: "sbom-1", pending: 8, dead_letter: 3 }
            ]
          }
        };
      }
      if (path.includes("/status/ingesters")) {
        return { ingesters: [{ name: "git-1", kind: "git", fact_count: 142900, health: "healthy" }] };
      }
      throw new Error(`unexpected getJson ${path}`);
    }
  } as unknown as EshuApiClient;
}

describe("StatusPage", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("shows the loading state until the overview resolves", () => {
    const client = { get: () => new Promise(() => {}), getJson: () => new Promise(() => {}) } as unknown as EshuApiClient;
    render(<StatusPage client={client} />);
    expect(screen.getByText(/Loading status/i)).toBeInTheDocument();
  });

  it("renders the hero indexing percentage and the per-collector live-state table", async () => {
    const client = statusClient();
    render(<StatusPage client={client} />);
    // Hero indexing percent is present.
    expect(await screen.findByText(/% indexed/i)).toBeInTheDocument();
    // The three collectors appear by display name in their table rows. The
    // collector glyph also renders short kind text (e.g. "Git"), so assert on
    // the row's <strong> display-name element specifically.
    expect(await screen.findByRole("cell", { name: /Git\b.*poll/i })).toBeInTheDocument();
    expect(screen.getByText("SBOM")).toBeInTheDocument();
    expect(screen.getByText("AWS Cloud")).toBeInTheDocument();
    // Catch-up classifications surface as readable labels.
    expect(screen.getAllByText(/Catching Up/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Stalled/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Up To Date/i).length).toBeGreaterThan(0);
  });

  it("renders progress bars for each collector row", async () => {
    const client = statusClient();
    render(<StatusPage client={client} />);
    await screen.findByText("SBOM");
    const bars = screen.getAllByRole("progressbar");
    // One hero ring + one per collector row (3).
    expect(bars.length).toBeGreaterThanOrEqual(4);
  });

  it("renders the pipeline / service state section", async () => {
    const client = statusClient();
    render(<StatusPage client={client} />);
    expect(await screen.findByText(/Pipeline/i)).toBeInTheDocument();
  });

  it("live-refreshes on an interval", async () => {
    const get = vi.fn(async (path: string) => {
      if (path.includes("/status/collector-readiness")) return { data: { readiness: [] }, error: null, truth: null };
      return { data: { state: "fresh", generations: {}, pending_projection: {} }, error: null, truth: null };
    });
    const getJson = vi.fn(async () => ({ coordinator: { collector_instances: [] } }));
    const client = { get, getJson } as unknown as EshuApiClient;
    render(<StatusPage client={client} pollMs={50} />);
    await waitFor(() => expect(get).toHaveBeenCalled());
    const firstCount = get.mock.calls.length;
    await waitFor(() => expect(get.mock.calls.length).toBeGreaterThan(firstCount), { timeout: 1000 });
  });

  it("shows an explicit unavailable state when the source is down", async () => {
    const client = {
      get: async () => { throw new Error("offline"); },
      getJson: async () => { throw new Error("offline"); }
    } as unknown as EshuApiClient;
    render(<StatusPage client={client} />);
    expect(await screen.findByText(/unavailable/i)).toBeInTheDocument();
  });
});
