import { act, render, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "./App";
import type { SourceState } from "./components/SourceControls";
import type * as EnvironmentModule from "./config/environment";

const configuredKey = "configured-shared-key";
// bootFromKey persists whatever base a failed popover attempt used, so this is
// the state left behind after an operator types a hostile origin and connects.
const persistedHostileBase = "https://elsewhere.example/";

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
vi.mock("./config/environment", async (importOriginal) => {
  const actual = await importOriginal<typeof EnvironmentModule>();
  return {
    ...actual,
    loadConsoleEnvironment: () => ({
      apiKey: configuredKey,
      apiBaseUrl: persistedHostileBase,
      mode: "private",
      recentApiBaseUrls: [persistedHostileBase],
    }),
    saveConsoleEnvironment: vi.fn(),
  };
});

describe("App boot with an operator-persisted base", () => {
  beforeEach(() => {
    bootMocks.bootFromSession.mockResolvedValue(null);
    bootMocks.bootFromKey.mockResolvedValue(null);
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("{}", { status: 200 })),
    );
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  // The saved base is not a trust anchor. Booting straight into the bearer
  // fallback would send Authorization: Bearer <seed> to that origin with no
  // user action at all.
  it("never sends the build-time seed to a saved base that is not the build's own", async () => {
    render(
      <MemoryRouter initialEntries={["/"]}>
        <App />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(bootMocks.bootFromSession).toHaveBeenCalledWith(persistedHostileBase),
    );
    // Let the boot promise chain settle. Any bearer fallback fires inside that
    // chain, so once it has drained a leaking call would already be recorded.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    const leaked = bootMocks.bootFromKey.mock.calls.some(([, key]) => key === configuredKey);
    expect(leaked).toBe(false);
  });
});
