import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RepositoryFreshnessRow } from "./RepositoryFreshnessChip";
import type { EshuApiClient } from "../../api/client";

afterEach(() => {
  vi.restoreAllMocks();
});

function freshnessClient(data: unknown): EshuApiClient & { calls: string[] } {
  const calls: string[] = [];
  const client = {
    calls,
    get: async (path: string) => {
      calls.push(path);
      return { data, error: null, truth: null };
    },
  };
  return client as unknown as EshuApiClient & { calls: string[] };
}

describe("RepositoryFreshnessRow", () => {
  it("fetches exactly once for the selected repository and renders the verdict headline", async () => {
    const client = freshnessClient({
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
    });

    render(<RepositoryFreshnessRow client={client} repoId="repository:checkout-service" />);

    expect(await screen.findByText(/Current through a1b2c3d4e5/)).toBeInTheDocument();
    expect(client.calls).toEqual(["/api/v0/repositories/repository%3Acheckout-service/freshness"]);
  });

  it("renders nothing when the freshness read is unavailable", async () => {
    const client = {
      get: async () => {
        throw new Error("offline");
      },
    } as unknown as EshuApiClient;

    const { container } = render(
      <RepositoryFreshnessRow client={client} repoId="repository:checkout-service" />,
    );

    await waitFor(() => expect(container).toBeEmptyDOMElement());
  });

  it("renders nothing when no client is connected", () => {
    const { container } = render(<RepositoryFreshnessRow repoId="repository:checkout-service" />);
    expect(container).toBeEmptyDOMElement();
  });
});
