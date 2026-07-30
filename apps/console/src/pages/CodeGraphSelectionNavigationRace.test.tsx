import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useContext, useMemo } from "react";
import {
  MemoryRouter,
  UNSAFE_NavigationContext as NavigationContext,
  useLocation,
  useNavigate,
} from "react-router";
import type { Navigator } from "react-router";

import { CodeGraphPage } from "./CodeGraphPage";
import {
  clientWithCodeGraphInventory,
  codeGraphInventory,
  codeGraphRepository,
  deferred,
  type Deferred,
} from "./codeGraphTestFixtures";
import type { RepoListItem } from "../api/repoCatalog";
import { demoModel } from "../console/demoModel";

// A faithful stub of the `navigator` react-router's `RouterProvider`
// (data-router mode: `createBrowserRouter` + `<RouterProvider>`) hands down
// through `NavigationContext` -- see `lib/components.js` in the pinned
// react-router 8.3.0 source: `{ createHref, encodeLocation, go, push,
// replace }`, with NO `.location` property. `MemoryRouter`/`BrowserRouter`
// pass the full `history` object instead, which always has `.location`; this
// stub exercises the router mode that does not.
const navigatorWithoutLocation = {
  createHref: () => "",
  go: () => undefined,
  push: () => undefined,
  replace: () => undefined,
} satisfies Navigator;

const repositories: readonly RepoListItem[] = [
  codeGraphRepository("repository:r1", "service-one"),
  codeGraphRepository("repository:r2", "service-two"),
];

describe("CodeGraphPage repository scope through browser navigation", () => {
  it("restores repository scope through browser back and forward", async () => {
    const client = clientWithCodeGraphInventory((repoId) =>
      Promise.resolve(
        codeGraphInventory(repoId, repoId === "repository:r1" ? "alphaSymbol" : "betaSymbol"),
      ),
    );
    render(
      <MemoryRouter initialEntries={["/code-graph?repo_id=repository%3Ar1"]}>
        <CodeGraphPage
          client={client}
          model={{ ...demoModel, findings: [], source: "live" }}
          repositories={repositories}
        />
        <HistoryControls />
      </MemoryRouter>,
    );
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Symbol" })).toHaveTextContent("alphaSymbol"),
    );
    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), {
      target: { value: "repository:r2" },
    });
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Symbol" })).toHaveTextContent("betaSymbol"),
    );

    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("repository:r1"),
    );
    expect(await screen.findByRole("combobox", { name: "Symbol" })).toHaveTextContent(
      "alphaSymbol",
    );
    fireEvent.click(screen.getByRole("button", { name: "Forward" }));
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("repository:r2"),
    );
  });

  it("does not let a still-pending canonicalize write clobber a back navigation that lands first", async () => {
    const pending = new Map<string, Deferred<unknown>>();
    const requestCounts = new Map<string, number>();
    const client = clientWithCodeGraphInventory((repoId) => {
      requestCounts.set(repoId, (requestCounts.get(repoId) ?? 0) + 1);
      const request = deferred<unknown>();
      pending.set(repoId, request);
      return request.promise;
    });
    render(
      <MemoryRouter initialEntries={["/code-graph?repo_id=repository%3Ar1"]}>
        <CodeGraphPage
          client={client}
          model={{ ...demoModel, findings: [], source: "live" }}
          repositories={repositories}
        />
        <HistoryControls />
      </MemoryRouter>,
    );
    await waitFor(() => expect(pending.has("repository:r1")).toBe(true));
    await act(async () => {
      pending.get("repository:r1")?.resolve(codeGraphInventory("repository:r1", "alphaSymbol"));
    });
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Symbol" })).toHaveTextContent("alphaSymbol"),
    );

    fireEvent.change(screen.getByRole("combobox", { name: "Repository" }), {
      target: { value: "repository:r2" },
    });
    await waitFor(() => expect(pending.has("repository:r2")).toBe(true));

    // Resolve repository:r2's inventory (which makes `selected` resolve to
    // betaSymbol and schedules the URL-canonicalize effect to add
    // entity_id=betaSymbol to the CURRENT, repository:r2 history entry) and
    // click "Back" inside the SAME act() batch, with only bare microtask
    // yields (no `waitFor`/real-timer wait) in between. This forces the two
    // updates -- the still-pending canonicalize write computed from the
    // repository:r2 render, and the back-navigation's own commit -- into the
    // same flush window without giving the test's own synchronization
    // machinery a chance to serialize them the way `waitFor` normally would.
    await act(async () => {
      pending.get("repository:r2")?.resolve(codeGraphInventory("repository:r2", "betaSymbol"));
      await Promise.resolve();
      await Promise.resolve();
      fireEvent.click(screen.getByRole("button", { name: "Back" }));
    });

    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Repository" })).toHaveValue("repository:r1"),
    );
    // Navigating back to repository:r1 re-triggers its inventory fetch (a
    // fresh request, independent of the one already resolved above); resolve
    // it so the assertion below observes the settled symbol rather than the
    // loading placeholder.
    await waitFor(() => expect(requestCounts.get("repository:r1")).toBe(2));
    await act(async () => {
      pending.get("repository:r1")?.resolve(codeGraphInventory("repository:r1", "alphaSymbol"));
    });
    expect(await screen.findByRole("combobox", { name: "Symbol" })).toHaveTextContent(
      "alphaSymbol",
    );
  });

  it("does not throw and skips the canonicalize write when the navigator has no .location", async () => {
    const client = clientWithCodeGraphInventory((repoId) =>
      Promise.resolve(codeGraphInventory(repoId, "alphaSymbol")),
    );
    render(
      <MemoryRouter initialEntries={["/code-graph?repo_id=repository%3Ar1"]}>
        <NavigationContextWithoutLocation>
          <CodeGraphPage
            client={client}
            model={{ ...demoModel, findings: [], source: "live" }}
            repositories={repositories}
          />
          <LocationProbe />
        </NavigationContextWithoutLocation>
      </MemoryRouter>,
    );

    // The hook auto-selects the first symbol once the repository:r1 inventory
    // resolves, which schedules the URL-canonicalize effect to add
    // `entity_id=content-entity:repository:r1:alphaSymbol` -- exercising the
    // exact `readLiveLocationSearch` call this test targets.
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Symbol" })).toHaveTextContent("alphaSymbol"),
    );
    // Give the effect a further tick to run to completion; if `.location`
    // were dereferenced unguarded on a navigator that lacks it, this would
    // throw a synchronous `TypeError` inside the effect and fail the test.
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(screen.getByTestId("location-search")).toHaveTextContent("repo_id=repository%3Ar1");
    expect(screen.getByTestId("location-search")).not.toHaveTextContent("entity_id");
  });
});

function HistoryControls(): React.JSX.Element {
  const navigate = useNavigate();
  return (
    <>
      <button type="button" onClick={() => navigate(-1)}>
        Back
      </button>
      <button type="button" onClick={() => navigate(1)}>
        Forward
      </button>
    </>
  );
}

function NavigationContextWithoutLocation({
  children,
}: {
  readonly children: React.ReactNode;
}): React.JSX.Element {
  const ctx = useContext(NavigationContext);
  const value = useMemo(() => ({ ...ctx, navigator: navigatorWithoutLocation }), [ctx]);
  return <NavigationContext.Provider value={value}>{children}</NavigationContext.Provider>;
}

function LocationProbe(): React.JSX.Element {
  const location = useLocation();
  return <span data-testid="location-search">{location.search}</span>;
}
