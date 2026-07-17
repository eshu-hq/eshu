import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";

import { SemanticSearchPage } from "./SemanticSearchPage";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";
import { readyRepositoryCatalog } from "../repositoryCatalogLifecycle";

const catalog = readyRepositoryCatalog([
  repository("repository:r_checkout", "checkout-service", "acme/checkout-service"),
  repository("repository:r_payments", "payments-api", "acme/payments-api"),
]);

describe("SemanticSearchPage repository transitions", () => {
  it("clears results from the prior repository when the selection draft changes", async () => {
    const client = clientReturningResults();
    renderPage(client);

    expect(await screen.findByText("retry.go")).toBeInTheDocument();
    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), {
      target: { value: "repository:r_payments" },
    });

    expect(screen.queryByText("retry.go")).not.toBeInTheDocument();
    expect(screen.getByText("Run a search to see results here.")).toBeInTheDocument();
  });

  it("runs a new canonical search after switching from an existing result", async () => {
    const client = clientReturningResults();
    renderPage(client);

    expect(await screen.findByText("retry.go")).toBeInTheDocument();
    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), {
      target: { value: "repository:r_payments" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));

    expect(await screen.findByText("payments.go")).toBeInTheDocument();
    expect(client.post).toHaveBeenCalledTimes(2);
  });

  it("clears an ambiguity warning after the user explicitly selects a repository", async () => {
    const client = { post: vi.fn() } as unknown as EshuApiClient;
    const ambiguousCatalog = readyRepositoryCatalog([
      repository("repository:r_one", "shared-service", "team-one/shared-service"),
      repository("repository:r_two", "shared-service", "team-two/shared-service"),
    ]);
    render(
      <MemoryRouter initialEntries={["/semantic-search?repo=shared-service&q=retry+logic"]}>
        <SemanticSearchPage client={client} repositoryCatalog={ambiguousCatalog} />
      </MemoryRouter>,
    );

    expect(
      await screen.findByText(/matches multiple authorized repositories/i),
    ).toBeInTheDocument();
    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), {
      target: { value: "repository:r_one" },
    });

    expect(screen.queryByText(/matches multiple authorized repositories/i)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Search" })).toBeEnabled();
  });
});

function clientReturningResults(): EshuApiClient {
  return {
    post: vi.fn(async (_path: string, body: unknown) => {
      const repoId = (body as { readonly repo_id: string }).repo_id;
      return envelope(repoId, repoId === "repository:r_payments" ? "payments.go" : "retry.go");
    }),
  } as unknown as EshuApiClient;
}

function envelope(repoId: string, title: string): unknown {
  return {
    data: {
      corpus_limit: 1000,
      corpus_may_be_truncated: false,
      facets: { languages: { go: 1 } },
      indexed_document_count: 1,
      limit: 20,
      mode: "hybrid",
      query: "retry logic",
      repo_id: repoId,
      results: [
        {
          document: {
            context_text: "Retained result.",
            id: `document:${repoId}`,
            labels: ["language:go"],
            path: "main.go",
            repo_id: repoId,
            source_kind: "repository_file",
            title,
            updated_at: "2026-07-17T00:00:00Z",
          },
          freshness: { state: "fresh" },
          rank: 1,
          score: 0.9,
          search_method: "hybrid",
          truth_scope: { basis: "content_index", level: "derived" },
        },
      ],
      retrieval_state: "ready",
      search_mode: "hybrid",
      timeout_ms: 8000,
      truncated: false,
    },
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

function renderPage(client: EshuApiClient): void {
  render(
    <MemoryRouter initialEntries={["/semantic-search?repo=repository%3Ar_checkout&q=retry+logic"]}>
      <SemanticSearchPage client={client} repositoryCatalog={catalog} />
    </MemoryRouter>,
  );
}

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
