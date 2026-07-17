// pages/SemanticSearchPage.test.tsx
// Verifies SemanticSearchPage:
//   - runs a query and renders results (rank, title/path, snippet, truth/freshness)
//   - renders language chips from facets.languages with counts
//   - toggling a chip re-queries with the languages param and reflects the URL
//   - reloading with ?languages=... in the URL restores the selection
//   - empty query / zero results / API error states render honestly
//   - language chips are real buttons with aria-pressed (a11y)
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import { MemoryRouter, useLocation, useNavigate } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { SemanticSearchPage } from "./SemanticSearchPage";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";
import { readyRepositoryCatalog, type RepositoryCatalogState } from "../repositoryCatalogLifecycle";

const checkoutRepository = repository(
  "repository:r_checkout",
  "checkout-service",
  "acme/checkout-service",
);

const defaultCatalog = readyRepositoryCatalog([
  checkoutRepository,
  repository("repository:r_payments", "payments-api", "acme/payments-api"),
]);

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
    repo_id: "repository:r_checkout",
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
          repo_id: "repository:r_checkout",
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
  repositoryCatalog: RepositoryCatalogState = defaultCatalog,
) {
  return render(
    <MemoryRouter initialEntries={initialEntries as string[]}>
      <LocationProbe />
      <SemanticSearchPage client={client} repositoryCatalog={repositoryCatalog} />
    </MemoryRouter>,
  );
}

describe("SemanticSearchPage", () => {
  it("runs a query on submit and renders results with truth/freshness labels", async () => {
    const client = {
      post: vi.fn(async () => envelope(resultsResponse())),
    } as unknown as EshuApiClient;

    renderPage(client);

    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), {
      target: { value: "repository:r_checkout" },
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
    expect((call[1] as Record<string, unknown>).repo_id).toBe("repository:r_checkout");
    await waitFor(() =>
      expect(screen.getByTestId("location-search")).toHaveTextContent(
        "repo=repository%3Ar_checkout",
      ),
    );
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

  it("resolves a unique repository slug to its authorized canonical ID", async () => {
    const client = {
      post: vi.fn(async () => envelope(resultsResponse())),
    } as unknown as EshuApiClient;

    renderPage(client, ["/semantic-search?repo=acme%2Fcheckout-service&q=retry+logic"]);

    await screen.findByText("retry.go");
    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue(
      "repository:r_checkout",
    );
    expect(screen.getByRole("option", { name: "checkout-service" })).toBeInTheDocument();
    const call = (client.post as ReturnType<typeof vi.fn>).mock.calls[0];
    expect((call[1] as Record<string, unknown>).repo_id).toBe("repository:r_checkout");
  });

  it("fails closed when a repository label is ambiguous and requires an explicit choice", async () => {
    const client = { post: vi.fn() } as unknown as EshuApiClient;
    const catalog = readyRepositoryCatalog([
      repository("repository:r_one", "shared-service", "team-one/shared-service"),
      repository("repository:r_two", "shared-service", "team-two/shared-service"),
    ]);

    renderPage(client, ["/semantic-search?repo=shared-service&q=retry+logic"], catalog);

    expect(
      await screen.findByText(/matches multiple authorized repositories/i),
    ).toBeInTheDocument();
    expect(client.post).not.toHaveBeenCalled();
    const selector = screen.getByRole("combobox", { name: "Repository" });
    expect(
      within(selector).getByRole("option", { name: "shared-service — team-one/shared-service" }),
    ).toBeInTheDocument();
    expect(
      within(selector).getByRole("option", { name: "shared-service — team-two/shared-service" }),
    ).toBeInTheDocument();
  });

  it("does not search an unavailable repository or mislabel it as zero results", async () => {
    const client = { post: vi.fn() } as unknown as EshuApiClient;

    renderPage(client, ["/semantic-search?repo=repository%3Ar_missing&q=retry+logic"]);

    expect(
      await screen.findByText(/not present in this authorized session catalog/i),
    ).toBeInTheDocument();
    expect(screen.queryByText("No results for this query.")).not.toBeInTheDocument();
    expect(client.post).not.toHaveBeenCalled();
  });

  it("searches the catalog without dropping the current repository selection", () => {
    const client = { post: vi.fn() } as unknown as EshuApiClient;
    renderPage(client);

    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), {
      target: { value: "repository:r_checkout" },
    });
    fireEvent.change(screen.getByRole("searchbox", { name: "Search repositories" }), {
      target: { value: "payments" },
    });

    expect(screen.getByRole("option", { name: "checkout-service" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "payments-api" })).toBeInTheDocument();
  });

  it("labels the repository search and selector independently", () => {
    const client = { post: vi.fn() } as unknown as EshuApiClient;
    renderPage(client);

    expect(
      screen.getByRole("searchbox", { name: "Search repositories" }).closest("label"),
    ).toBeNull();
    expect(screen.getByRole("combobox", { name: "Repository" }).closest("label")).toBeNull();
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

    renderPage(client, ["/semantic-search?repo=repository%3Ar_checkout&q=retry"]);

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
    // announceRef.current?.focus() runs in a useEffect keyed on result.status
    // (SemanticSearchPage.tsx), so it commits one tick after the "status" node
    // itself lands in the DOM. findByRole above only waits for the node to
    // appear, not for that follow-up effect to run, so a synchronous
    // toHaveFocus() here races it. Locally that race never loses, but issue
    // #5151 reproduced it flaking in CI on a constrained runner (same
    // CI-slow-runner timing class as the OperationsPage Suspense flake fixed
    // in #5140) — jsdom's focus-effect scheduling fell behind the assertion.
    // waitFor with a generous timeout, scoped to this assertion only, lets
    // the effect catch up instead of asserting focus synchronously.
    await waitFor(() => expect(announcement).toHaveFocus(), { timeout: 5000 });
  });

  it("moves focus to the error alert once a search fails (a11y)", async () => {
    const client = {
      post: vi.fn(async () => ({
        data: null,
        error: { code: "not_found", message: "repository not found" },
        truth: null,
      })),
    } as unknown as EshuApiClient;

    renderPage(client, ["/semantic-search?repo=repository%3Ar_checkout&q=retry"]);

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("repository not found");
    // Same focus-effect race as the ready-path assertion above (#5151): the
    // useEffect that calls .focus() (SemanticSearchPage.tsx:98-102) fires on
    // both "ready" and "error" status and commits one tick after the "alert"
    // node lands in the DOM, so a synchronous toHaveFocus() here races it too.
    // waitFor with the same generous timeout, scoped to this assertion only,
    // lets the effect catch up instead of asserting focus synchronously.
    await waitFor(() => expect(alert).toHaveFocus(), { timeout: 5000 });
  });
  it("ignores a stale in-flight response after leaving bounded state (#4024)", async () => {
    let resolvePost: ((value: unknown) => void) | undefined;
    const client = {
      post: vi.fn(
        () =>
          new Promise((resolve) => {
            resolvePost = resolve;
          }),
      ),
    } as unknown as EshuApiClient;

    function BackToUnbounded(): React.JSX.Element {
      const navigate = useNavigate();
      return (
        <button type="button" onClick={() => navigate("/semantic-search")}>
          leave-bounded
        </button>
      );
    }

    render(
      <MemoryRouter
        initialEntries={["/semantic-search?repo=acme%2Fcheckout-service&q=retry+logic"]}
      >
        <BackToUnbounded />
        <SemanticSearchPage client={client} repositoryCatalog={defaultCatalog} />
      </MemoryRouter>,
    );

    // The bounded URL fires a search that stays in flight (its resolver is captured).
    await waitFor(() => expect(resolvePost).toBeDefined());

    // Navigate to the unbounded URL while that search is still in flight.
    fireEvent.click(screen.getByRole("button", { name: "leave-bounded" }));
    expect(
      await screen.findByText("Enter a repository and a query to search."),
    ).toBeInTheDocument();

    // The stale response resolves AFTER leaving bounded state; the latestLoad
    // bump in the else branch must keep it from overwriting the idle page with
    // results for the previous query.
    resolvePost?.(envelope(resultsResponse()));
    await waitFor(() =>
      expect(screen.getByText("Enter a repository and a query to search.")).toBeInTheDocument(),
    );
    expect(screen.queryByText("retry.go")).not.toBeInTheDocument();
  });
});

function repository(id: string, name: string, repoSlug: string): RepoListItem {
  return {
    groupKey: "source",
    groupKind: "source",
    groupReason: "fixture",
    groupSource: "fixture",
    groupTruth: "exact",
    id,
    isDependency: false,
    name,
    remoteUrl: "",
    repoSlug,
  };
}

function LocationProbe(): React.JSX.Element {
  const location = useLocation();
  return <span data-testid="location-search">{location.search}</span>;
}
