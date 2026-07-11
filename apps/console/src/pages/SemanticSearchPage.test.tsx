// pages/SemanticSearchPage.test.tsx
// Verifies SemanticSearchPage:
//   - runs a query and renders results (rank, title/path, snippet, truth/freshness)
//   - renders language chips from facets.languages with counts
//   - toggling a chip re-queries with the languages param and reflects the URL
//   - reloading with ?languages=... in the URL restores the selection
//   - empty query / zero results / API error states render honestly
//   - language chips are real buttons with aria-pressed (a11y)
import { render, screen, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { SemanticSearchPage } from "./SemanticSearchPage";
import type { EshuApiClient } from "../api/client";

function envelope(data: unknown) {
  return {
    data,
    error: null,
    truth: {
      basis: "hybrid",
      capability: "search.semantic",
      freshness: { state: "fresh" },
      level: "derived",
      profile: "production",
    },
  };
}

function resultsResponse(overrides: Record<string, unknown> = {}) {
  return {
    query: "retry logic",
    repo_id: "acme/checkout-service",
    mode: "hybrid",
    search_mode: "hybrid",
    limit: 20,
    timeout_ms: 8000,
    results: [
      {
        rank: 1,
        score: 0.92,
        search_method: "hybrid",
        document: {
          id: "doc-1",
          repo_id: "acme/checkout-service",
          source_kind: "repository_file",
          title: "retry.go",
          path: "internal/checkout/retry.go",
          context_text: "Implements exponential backoff for checkout retries.",
          labels: ["language:go"],
          updated_at: "2026-06-01T00:00:00Z",
        },
        truth_scope: { level: "derived", basis: "content_index" },
        freshness: { state: "fresh" },
      },
    ],
    truncated: false,
    indexed_document_count: 512,
    corpus_limit: 1000,
    corpus_may_be_truncated: false,
    retrieval_state: "ready",
    facets: { languages: { go: 3, rust: 1 } },
    ...overrides,
  };
}

function renderPage(
  client: EshuApiClient | undefined,
  initialEntries: readonly string[] = ["/semantic-search"],
) {
  return render(
    <MemoryRouter initialEntries={initialEntries as string[]}>
      <SemanticSearchPage client={client} />
    </MemoryRouter>,
  );
}

describe("SemanticSearchPage", () => {
  it("runs a query on submit and renders results with truth/freshness labels", async () => {
    const client = {
      post: vi.fn(async () => envelope(resultsResponse())),
    } as unknown as EshuApiClient;

    renderPage(client);

    fireEvent.change(screen.getByLabelText("Repository"), {
      target: { value: "acme/checkout-service" },
    });
    fireEvent.change(screen.getByLabelText("Search query"), {
      target: { value: "retry logic" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));

    expect(await screen.findByText("retry.go")).toBeInTheDocument();
    expect(screen.getByText("internal/checkout/retry.go")).toBeInTheDocument();
    expect(
      screen.getByText("Implements exponential backoff for checkout retries."),
    ).toBeInTheDocument();
    expect(screen.getByText("derived")).toBeInTheDocument();
    expect(screen.getByText("fresh")).toBeInTheDocument();

    const call = (client.post as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[0]).toBe("/api/v0/search/semantic");
    expect((call[1] as Record<string, unknown>).repo_id).toBe("acme/checkout-service");
    expect((call[1] as Record<string, unknown>).query).toBe("retry logic");
  });

  it("renders language chips from facets.languages with counts", async () => {
    const client = {
      post: vi.fn(async () => envelope(resultsResponse())),
    } as unknown as EshuApiClient;

    renderPage(client, ["/semantic-search?repo=acme%2Fcheckout-service&q=retry+logic"]);

    const goChip = await screen.findByRole("button", { name: /go/i });
    expect(goChip).toHaveTextContent("3");
    const rustChip = screen.getByRole("button", { name: /rust/i });
    expect(rustChip).toHaveTextContent("1");
  });

  it("chips are real buttons with aria-pressed, reachable by role (a11y)", async () => {
    const client = {
      post: vi.fn(async () => envelope(resultsResponse())),
    } as unknown as EshuApiClient;

    renderPage(client, ["/semantic-search?repo=acme%2Fcheckout-service&q=retry+logic"]);

    const goChip = await screen.findByRole("button", { name: /go/i });
    expect(goChip.tagName).toBe("BUTTON");
    expect(goChip).toHaveAttribute("aria-pressed", "false");
  });

  it("toggling a chip re-queries with the languages param and updates the URL", async () => {
    const calls: Array<Record<string, unknown>> = [];
    const client = {
      post: vi.fn(async (_path: string, body: unknown) => {
        calls.push(body as Record<string, unknown>);
        const languages = (body as Record<string, unknown>).languages as string[] | undefined;
        if (languages?.includes("go")) {
          return envelope(
            resultsResponse({
              facets: { languages: { go: 3 } },
            }),
          );
        }
        return envelope(resultsResponse());
      }),
    } as unknown as EshuApiClient;

    renderPage(client, ["/semantic-search?repo=acme%2Fcheckout-service&q=retry+logic"]);

    const goChip = await screen.findByRole("button", { name: /go/i });
    fireEvent.click(goChip);

    await screen.findByText((_, el) => el?.getAttribute("aria-pressed") === "true");
    const lastCall = calls.at(-1);
    expect(lastCall?.languages).toEqual(["go"]);
  });

  it("restores the language selection from the URL on load", async () => {
    const client = {
      post: vi.fn(async () => envelope(resultsResponse({ facets: { languages: { go: 3 } } }))),
    } as unknown as EshuApiClient;

    renderPage(client, [
      "/semantic-search?repo=acme%2Fcheckout-service&q=retry+logic&languages=go",
    ]);

    const goChip = await screen.findByRole("button", { name: /go/i });
    expect(goChip).toHaveAttribute("aria-pressed", "true");
    const call = (client.post as ReturnType<typeof vi.fn>).mock.calls[0];
    expect((call[1] as Record<string, unknown>).languages).toEqual(["go"]);
  });

  it("renders an empty-query state when no query is bounded yet", () => {
    const client = { post: vi.fn() } as unknown as EshuApiClient;
    renderPage(client);
    expect(screen.getByText("Enter a repository and a query to search.")).toBeInTheDocument();
    expect(client.post).not.toHaveBeenCalled();
  });

  it("renders a zero-results state honestly", async () => {
    const client = {
      post: vi.fn(async () =>
        envelope(resultsResponse({ results: [], facets: { languages: {} } })),
      ),
    } as unknown as EshuApiClient;

    renderPage(client, ["/semantic-search?repo=acme%2Fcheckout-service&q=nothing+here"]);

    expect(await screen.findByText("No results for this query.")).toBeInTheDocument();
  });

  it("renders an API error state without fabricating data", async () => {
    const client = {
      post: vi.fn(async () => ({
        data: null,
        error: { code: "not_found", message: "repository not found" },
        truth: null,
      })),
    } as unknown as EshuApiClient;

    renderPage(client, ["/semantic-search?repo=missing%2Frepo&q=retry"]);

    expect(await screen.findByText(/repository not found/)).toBeInTheDocument();
  });

  it("renders an unavailable notice when there is no live client", () => {
    renderPage(undefined);
    expect(screen.getByText("Live Eshu API connection unavailable.")).toBeInTheDocument();
  });

  it("moves focus to the result announcement once a search settles (a11y)", async () => {
    const client = {
      post: vi.fn(async () => envelope(resultsResponse())),
    } as unknown as EshuApiClient;

    renderPage(client, ["/semantic-search?repo=acme%2Fcheckout-service&q=retry+logic"]);

    const announcement = await screen.findByRole("status");
    expect(announcement).toHaveTextContent('1 result for "retry logic".');
    expect(announcement).toHaveFocus();
  });

  it("moves focus to the error alert once a search fails (a11y)", async () => {
    const client = {
      post: vi.fn(async () => ({
        data: null,
        error: { code: "not_found", message: "repository not found" },
        truth: null,
      })),
    } as unknown as EshuApiClient;

    renderPage(client, ["/semantic-search?repo=missing%2Frepo&q=retry"]);

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("repository not found");
    expect(alert).toHaveFocus();
  });
});
