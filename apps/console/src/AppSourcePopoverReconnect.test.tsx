import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "./App";
import type { SourceState } from "./components/SourceControls";
import { emptyConsoleModel } from "./console/liveModel";

const configuredKey = "configured-shared-key";

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
    apiKey: configuredKey,
    apiBaseUrl: "/eshu-api/",
    mode: "private",
    recentApiBaseUrls: ["/eshu-api/"],
  }),
  saveConsoleEnvironment: vi.fn(),
}));

function bearerBootResult() {
  return {
    client: {},
    model: emptyConsoleModel(),
    repositoryCatalog: Promise.resolve({
      completeness: "complete",
      kind: "ready",
      repositories: [],
      warning: "",
    }),
    session: null,
  };
}

// The deployment under test has no browser session, so bootFromKey only
// succeeds when it is handed the build-time dev seed. An empty key reproduces
// the real no-session outcome: no data, no session, null.
function keyOnlyBoot(_base: string, key: string): unknown {
  return key.trim().length > 0 ? bearerBootResult() : null;
}

describe("App data-source reconnect", () => {
  beforeEach(() => {
    bootMocks.bootFromSession.mockResolvedValue(null);
    bootMocks.bootFromKey.mockImplementation(async (base: string, key: string) =>
      keyOnlyBoot(base, key),
    );
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("{}", { status: 200 })),
    );
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  it("reuses the configured key when reconnecting from the data source popover", async () => {
    render(
      <MemoryRouter initialEntries={["/"]}>
        <App />
      </MemoryRouter>,
    );

    const pill = await screen.findByRole("button", { name: "Live" });
    await waitFor(() => expect(pill).toHaveClass("src-connected"));

    fireEvent.click(pill);
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    await waitFor(() => {
      const lastCall = bootMocks.bootFromKey.mock.calls.at(-1);
      expect(lastCall).toEqual(["/eshu-api/", configuredKey]);
    });
    await waitFor(() => expect(pill).toHaveClass("src-connected"));
  });
});
