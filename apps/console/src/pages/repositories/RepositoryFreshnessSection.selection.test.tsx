import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { RepositoryFreshnessSection } from "./RepositoryFreshnessSection";
import type { EshuApiClient } from "../../api/client";

function freshnessWire(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    verdict: "current",
    observed_commit: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
    observed_at: new Date().toISOString(),
    generation: null,
    stages: { collected: true, reduced: true, projected: true, materialized: true },
    outstanding_by_stage: [],
    shared_enrichment: { pending: false, pending_domains: [] },
    unobserved_push: null,
    as_of: new Date().toISOString(),
    scoped: false,
    ...overrides,
  };
}

function countingClient(wire: Record<string, unknown>): EshuApiClient & { calls: number } {
  let calls = 0;
  const client = {
    get calls() {
      return calls;
    },
    get: async (path: string) => {
      if (!path.includes("/freshness")) throw new Error(`unexpected get ${path}`);
      calls += 1;
      return { data: wire, error: null, truth: null };
    },
  };
  return client as unknown as EshuApiClient & { calls: number };
}

describe("RepositoryFreshnessSection selection evidence", () => {
  it("renders the not_selected verdict with its selection state and reason", async () => {
    const client = countingClient(
      freshnessWire({
        verdict: "not_selected",
        selection: {
          state: "not_selected",
          reason: "rule_excluded",
          state_since: "2026-06-20T09:00:00Z",
          last_listed_at: null,
          evaluated_at: "2026-06-21T12:00:00Z",
          live_selector_count: 2,
        },
      }),
    );

    render(
      <RepositoryFreshnessSection client={client} repoId="repository:legacy-api" pollMs={20} />,
    );

    expect(await screen.findByText("Not selected for indexing")).toBeInTheDocument();
    expect(screen.getByText("Collector selection")).toBeInTheDocument();
    expect(screen.getByText("Not selected")).toBeInTheDocument();
    expect(
      screen.getByText(
        "excluded by a selection rule · since 2026-06-20T09:00:00Z · 2 live selectors",
      ),
    ).toBeInTheDocument();
  });

  it("does not poll a not_selected repository", async () => {
    const client = countingClient(
      freshnessWire({
        verdict: "not_selected",
        selection: { state: "not_selected", reason: "archived_excluded", live_selector_count: 1 },
      }),
    );

    render(
      <RepositoryFreshnessSection client={client} repoId="repository:legacy-api" pollMs={20} />,
    );

    expect(await screen.findByText("Not selected for indexing")).toBeInTheDocument();
    await new Promise((resolve) => setTimeout(resolve, 120));
    await waitFor(() => expect(client.calls).toBe(1));
  });

  it("states that no live selector evidence exists when selection is unknown", async () => {
    const client = countingClient(freshnessWire());

    render(
      <RepositoryFreshnessSection client={client} repoId="repository:checkout" pollMs={50000} />,
    );

    expect(await screen.findByText("Collector selection")).toBeInTheDocument();
    expect(screen.getByText("No live selector evidence")).toBeInTheDocument();
  });
});
