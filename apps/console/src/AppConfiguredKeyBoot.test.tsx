import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const bootMocks = vi.hoisted(() => ({
  bootFromKey: vi.fn(),
  bootFromSession: vi.fn(),
}));

vi.mock("./appBoot", () => bootMocks);
vi.mock("./appRoutes", async () => {
  const { AskPage } = await import("./pages/AskPage");
  return {
    AppRoutes: ({
      source,
    }: {
      readonly source: import("./components/SourceControls").SourceState;
    }) => <AskPage source={source} />,
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

import { App } from "./App";
import { emptyConsoleModel } from "./console/liveModel";

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
      client: undefined,
      model: emptyConsoleModel(),
      repositories: [],
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
      .mock.calls.find(([url]) => String(url).includes("status/answer-narration"));
    expect(request?.[1]?.headers).toMatchObject({ Authorization: "Bearer configured-shared-key" });
  });

  it("clears the configured key after key boot creates a browser session", async () => {
    bootMocks.bootFromSession.mockResolvedValue(null);
    bootMocks.bootFromKey.mockResolvedValue({
      client: undefined,
      model: emptyConsoleModel(),
      repositories: [],
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
      .mock.calls.find(([url]) => String(url).includes("status/answer-narration"));
    expect(request?.[1]?.headers).not.toHaveProperty("Authorization");
  });
});
