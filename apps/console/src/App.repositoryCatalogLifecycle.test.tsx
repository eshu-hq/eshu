import { act, render, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { App } from "./App";

describe("App repository catalog lifecycle", () => {
  afterEach(() => {
    window.localStorage.clear();
    vi.unstubAllGlobals();
  });

  it("renders from the snapshot while one session-owned catalog load is still pending", async () => {
    window.localStorage.setItem(
      "eshu.console.environment",
      JSON.stringify({
        apiBaseUrl: "/eshu-api/",
        mode: "private",
        recentApiBaseUrls: ["/eshu-api/"],
      }),
    );
    let releaseCatalog: (() => void) | undefined;
    const catalogGate = new Promise<void>((resolve) => {
      releaseCatalog = resolve;
    });
    let fullCatalogCalls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const request = new Request(input);
        const url = new URL(request.url);
        if (url.pathname === "/eshu-api/api/v0/auth/browser-session") {
          return Response.json({ auth: { all_scopes: true, mode: "browser_session" } });
        }
        if (url.pathname === "/eshu-api/api/v0/repositories") {
          if (url.searchParams.get("limit") === "1") {
            return Response.json({ total: 1 });
          }
          fullCatalogCalls += 1;
          await catalogGate;
          return Response.json({
            data: {
              offset: 0,
              repositories: [
                {
                  group_key: "Platform",
                  group_kind: "source",
                  group_reason: "derived from repository slug namespace",
                  group_source: "repo_slug_namespace",
                  group_truth: "derived",
                  id: "repository:checkout-api",
                  is_dependency: false,
                  name: "checkout-api",
                  repo_slug: "platform/checkout-api",
                },
              ],
              truncated: false,
            },
            error: null,
            truth: null,
          });
        }
        if (url.pathname === "/eshu-api/api/v0/catalog") {
          return Response.json({ data: { services: [] }, error: null, truth: null });
        }
        if (url.pathname === "/eshu-api/api/v0/index-status") {
          return Response.json({ repository_count: 1, status: "ready" });
        }
        if (url.pathname === "/eshu-api/api/v0/status/ingesters") {
          return Response.json({ ingesters: [] });
        }
        return Response.json({ data: {}, error: null, truth: null });
      }),
    );

    render(
      <StrictMode>
        <MemoryRouter initialEntries={["/repositories"]}>
          <App />
        </MemoryRouter>
      </StrictMode>,
    );

    await waitFor(() => expect(fullCatalogCalls).toBe(1));
    expect(
      await screen.findByRole("heading", { level: 2, name: "Repositories" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Loading repositories…")).toBeInTheDocument();

    await act(async () => {
      releaseCatalog?.();
      await catalogGate;
    });

    expect(await screen.findByText("checkout-api")).toBeInTheDocument();
    expect(fullCatalogCalls).toBe(1);
  });
});
