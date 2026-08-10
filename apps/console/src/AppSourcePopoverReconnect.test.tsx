import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "./App";
import type { SourceState } from "./components/SourceControls";
import { emptyConsoleModel } from "./console/liveModel";

const configuredKey = "configured-shared-key";
const configuredBase = "/eshu-api/";

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
    apiBaseUrl: configuredBase,
    mode: "private",
    recentApiBaseUrls: [configuredBase],
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
// succeeds when it is handed a key. An empty key reproduces the real
// no-session outcome: no data, no session, null.
function keyOnlyBoot(_base: string, key: string): unknown {
  return key.trim().length > 0 ? bearerBootResult() : null;
}

// Drives the app to its connected state via the mount-path seed fallback, then
// clears the mock so later assertions can only see calls the click produced.
async function renderConnectedThenResetMock(): Promise<HTMLElement> {
  render(
    <MemoryRouter initialEntries={["/"]}>
      <App />
    </MemoryRouter>,
  );
  const pill = await screen.findByRole("button", { name: "Live" });
  await waitFor(() => expect(pill).toHaveClass("src-connected"));
  await waitFor(() => expect(bootMocks.bootFromKey).toHaveBeenCalled());
  bootMocks.bootFromKey.mockClear();
  return pill;
}

function bootCalls(): unknown[][] {
  return bootMocks.bootFromKey.mock.calls as unknown[][];
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

  it("retries the configured base with the seed after a credential-less connect", async () => {
    const pill = await renderConnectedThenResetMock();

    fireEvent.click(pill);
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    // Both calls must be new, so a no-op Connect handler cannot satisfy this.
    await waitFor(() => expect(bootCalls()).toHaveLength(2));
    expect(bootCalls()[0]).toEqual([configuredBase, ""]);
    expect(bootCalls()[1]).toEqual([configuredBase, configuredKey]);
    await waitFor(() => expect(pill).toHaveClass("src-connected"));
  });

  // A build-time seed is trusted for the configured deployment only. Retrying
  // an operator-typed origin with it would send Authorization: Bearer <seed>
  // to a server the operator never configured.
  it("never sends the seed to a base typed into the popover", async () => {
    const pill = await renderConnectedThenResetMock();
    const typedBase = "https://elsewhere.example/";

    fireEvent.click(pill);
    fireEvent.change(screen.getByPlaceholderText("/eshu-api/"), {
      target: { value: typedBase },
    });
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    await waitFor(() => expect(bootCalls()).toHaveLength(1));
    expect(bootCalls()[0]).toEqual([typedBase, ""]);
    expect(bootCalls().some(([, key]) => key === configuredKey)).toBe(false);
  });
});
