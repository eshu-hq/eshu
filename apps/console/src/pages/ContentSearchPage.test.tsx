import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";

import { ContentSearchPage } from "./ContentSearchPage";
import type { EshuApiClient } from "../api/client";

function answer(data: Record<string, unknown>, level = "partial") {
  return { data, truth: { level }, error: null };
}

function partial(progressed = true) {
  return {
    results: [
      {
        repo_id: "repo-a",
        relative_path: "a.go",
        content: "matching source",
        line_count: 4,
        language: "go",
      },
    ],
    count: 1,
    limit: 50,
    offset: 0,
    truncated: true,
    documents_not_searched: 90,
    partial: {
      reason: "candidate_budget_exceeded",
      rows_scanned_in_order: 10,
      rows_matched: 1,
      cursor: { repo_id: "repo-a", relative_path: "a.go" },
      budget_ms: 800,
      elapsed_ms: 780,
      overrun_ms: 0,
      progressed,
      hint: "add repo_id to scope the search",
    },
  };
}

function renderPage(post: ReturnType<typeof vi.fn>) {
  const client = { post } as unknown as EshuApiClient;
  return render(
    <MemoryRouter>
      <ContentSearchPage client={client} />
    </MemoryRouter>,
  );
}

describe("ContentSearchPage", () => {
  it("discloses the first and resumed pass separately and keeps the ordered prefix", async () => {
    const post = vi
      .fn()
      .mockResolvedValueOnce(answer(partial()))
      .mockResolvedValueOnce(
        answer(
          {
            results: [
              {
                repo_id: "repo-b",
                relative_path: "b.go",
                content: "another match",
                line_count: 2,
              },
            ],
            count: 1,
            limit: 49,
            offset: 0,
            truncated: false,
            documents_not_searched: 0,
          },
          "derived",
        ),
      );
    renderPage(post);
    fireEvent.change(screen.getByLabelText("Content pattern"), { target: { value: "token" } });
    fireEvent.click(screen.getByRole("button", { name: "Fresh search" }));
    expect(await screen.findByText(/scanned 10 indexed files of 100/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Resume search" }));
    expect(await screen.findByText(/Exact page/)).toBeInTheDocument();
    expect(screen.getByText("a.go")).toBeInTheDocument();
    expect(screen.getByText("b.go")).toBeInTheDocument();
    expect(post).toHaveBeenLastCalledWith("/api/v0/content/files/search", {
      query: "token",
      limit: 49,
      offset: 0,
      cursor: { repo_id: "repo-a", relative_path: "a.go" },
    });
  });

  it("does not offer an unchanged resume when no cursor progress was made", async () => {
    const post = vi.fn().mockResolvedValue(answer(partial(false)));
    renderPage(post);
    fireEvent.change(screen.getByLabelText("Content pattern"), { target: { value: "token" } });
    fireEvent.click(screen.getByRole("button", { name: "Fresh search" }));
    expect(await screen.findByText(/This pass made no progress/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Resume search" })).not.toBeInTheDocument();
  });

  it("clears stale results when the query changes during an in-flight search", async () => {
    let resolve: ((value: unknown) => void) | undefined;
    const post = vi.fn().mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    renderPage(post);
    fireEvent.change(screen.getByLabelText("Content pattern"), { target: { value: "first" } });
    fireEvent.click(screen.getByRole("button", { name: "Fresh search" }));
    fireEvent.change(screen.getByLabelText("Content pattern"), { target: { value: "second" } });
    await act(async () => {
      resolve?.(answer(partial()));
    });
    expect(screen.queryByText("a.go")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Resume search" })).not.toBeInTheDocument();
  });

  it("labels truth-partial without cursor metadata as incomplete", async () => {
    const post = vi.fn().mockResolvedValue(
      answer({
        results: [],
        count: 0,
        limit: 50,
        offset: 0,
        truncated: true,
        documents_not_searched: -1,
      }),
    );
    renderPage(post);
    fireEvent.change(screen.getByLabelText("Content pattern"), { target: { value: "token" } });
    fireEvent.click(screen.getByRole("button", { name: "Fresh search" }));
    await waitFor(() =>
      expect(screen.getByText(/No resume cursor was returned/)).toBeInTheDocument(),
    );
    expect(screen.queryByRole("button", { name: "Resume search" })).not.toBeInTheDocument();
  });
});
