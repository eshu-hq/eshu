import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "./App";
import type { SourceState } from "./components/SourceControls";
import { emptyConsoleModel } from "./console/liveModel";

const bootMocks = vi.hoisted(() => ({
  bootFromKey: vi.fn(),
  bootFromSession: vi.fn(),
}));

vi.mock("./appBoot", () => bootMocks);
vi.mock("./appRoutes", async () => {
  const { AskPage } = await import("./pages/AskPage");
  return {
    AppRoutes: ({ source }: { readonly source: SourceState }) => <AskPage source={source} />,
  };
});
vi.mock("./config/environment", () => ({
  loadConsoleEnvironment: () => ({
    apiKey: "configured-shared-key",
    apiBaseUrl: "/eshu-api/",
    mode: "private",
    recentApiBaseUrls: ["/eshu-api/"],
  }),
  saveConsoleEnvironment: vi.fn(),
}));

function requestUrl(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  return input instanceof URL ? input.href : input.url;
}

describe("App configured-key boot", () => {
  afterEach(() => {
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("{}", { status: 200 })),
    );
  });

  it("falls back to the configured key when the saved source has no browser session", async () => {
    bootMocks.bootFromSession.mockResolvedValue(null);
    bootMocks.bootFromKey.mockResolvedValue({
      client: {},
      model: emptyConsoleModel(),
      repositoryCatalog: Promise.resolve({
        completeness: "complete",
        kind: "ready",
        repositories: [],
        warning: "",
      }),
      session: null,
    });

    render(
      <MemoryRouter initialEntries={["/"]}>
        <App />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(bootMocks.bootFromKey).toHaveBeenCalledWith("/eshu-api/", "configured-shared-key");
    });
    expect(screen.getByRole("button", { name: "Live" })).toHaveClass("src-connected");
    await waitFor(() => expect(fetch).toHaveBeenCalled());
    const request = vi
      .mocked(fetch)
      .mock.calls.find(([url]) => requestUrl(url).includes("status/answer-narration"));
    expect(request?.[1]?.headers).toMatchObject({ Authorization: "Bearer configured-shared-key" });
  });

  it("clears the configured key after key boot creates a browser session", async () => {
    bootMocks.bootFromSession.mockResolvedValue(null);
    bootMocks.bootFromKey.mockResolvedValue({
      client: {},
      model: emptyConsoleModel(),
      repositoryCatalog: Promise.resolve({
        completeness: "complete",
        kind: "ready",
        repositories: [],
        warning: "",
      }),
      session: { auth: { mode: "browser_session", all_scopes: true } },
    });

    render(
      <MemoryRouter initialEntries={["/"]}>
        <App />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(bootMocks.bootFromKey).toHaveBeenCalledWith("/eshu-api/", "configured-shared-key");
    });
    await waitFor(() => expect(fetch).toHaveBeenCalled());
    const request = vi
      .mocked(fetch)
      .mock.calls.find(([url]) => requestUrl(url).includes("status/answer-narration"));
    expect(request?.[1]?.headers).not.toHaveProperty("Authorization");
  });
});
