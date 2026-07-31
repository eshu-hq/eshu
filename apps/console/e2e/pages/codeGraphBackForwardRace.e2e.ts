import type { Page } from "playwright";
import type { PageTest } from "../types.ts";

// Regression for #5833: a canonicalize effect in useCodeGraphSelection that
// is scheduled (queued) but has not yet flushed when a real browser back
// navigation lands must not clobber the restored repository scope.
//
// This exercises the PRODUCTION router (BrowserRouter via main.tsx), not
// MemoryRouter -- CodeGraphSelectionNavigationRace.test.tsx already proves
// the fix under MemoryRouter, where `navigate(-1)` mutates `history.location`
// synchronously. Under BrowserRouter, `history.go()` calls the native,
// asynchronous `window.history.go()`, and `history.location` is a live read
// of `window.location`, which only updates when the browser actually
// completes the traversal (at the same moment `popstate` fires) -- a
// genuinely later point than the JS call that requested the navigation.
//
// A MutationObserver fires `window.history.back()` the instant React commits
// the DOM for the newly-selected repository's first symbol -- that commit
// happens synchronously; the observer callback runs as a microtask right
// after it, strictly BEFORE React's deferred passive-effect (`useEffect`)
// flush. That is exactly the window where the canonicalize effect is queued
// but has not yet run, which is the mechanism the original bug report
// (intermittent `CodeGraphSelectionStates.test.tsx` failures) and the fix
// target.
//
// The race does not fire on every attempt (browser task-queue timing
// varies), so this runs several trials and fails on the first reproduction.
// Proven against a reverted (pre-fix) copy of useCodeGraphSelection.ts in a
// scratch checkout: ~27% of trials (8/30) reproduced the clobber; the fixed
// hook in this worktree was 0/30. TRIALS below is chosen so a real regression
// at that rate is caught with better than 99% probability in one run
// ((1 - 0.27)^TRIALS < 0.01).

const TRIALS = 15;
const REPO_A = "repository:checkout-service";
const REPO_B = "repository:payments-api";
const INVENTORY_PATH = "/api/v0/code/structure/inventory";

function inventoryEnvelope(repoId: string, entityId: string, name: string): unknown {
  return {
    data: {
      results: [
        {
          repo_id: repoId,
          entity_id: entityId,
          entity_name: name,
          entity_type: "function",
          file_path: `src/${name}.ts`,
          language: "TypeScript",
          start_line: 1,
          end_line: 10,
        },
      ],
      next_offset: null,
      truncated: false,
    },
    error: null,
    truth: {
      basis: "e2e_mock",
      capability: "code.structure.inventory",
      freshness: { state: "fresh" },
      level: "exact",
      profile: "e2e",
    },
  };
}

type RouteHandler = Parameters<Page["route"]>[1];

// Returns the handler function so callers can `page.unroute(pattern, handler)`
// with that EXACT reference. `page.unroute(pattern)` with no handler removes
// EVERY handler registered for the pattern -- including the shared
// `installMockApi` handler this test layers on top of -- which would starve
// every subsequent page test in the same run of its API mock.
function inventoryMockHandler(): RouteHandler {
  return async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname.startsWith("/eshu-api")
      ? url.pathname.slice("/eshu-api".length)
      : url.pathname;
    if (request.method() === "POST" && path === INVENTORY_PATH) {
      let body: { repo_id?: string } = {};
      try {
        body = request.postDataJSON() as { repo_id?: string };
      } catch {
        body = {};
      }
      const repoId = body.repo_id ?? "";
      const entityName = repoId === REPO_A ? "raceAlphaSymbol" : "raceBetaSymbol";
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(
          inventoryEnvelope(repoId, `content-entity:${repoId}:${entityName}`, entityName),
        ),
      });
    }
    return route.fallback();
  };
}

async function runTrial(page: Page): Promise<string> {
  await page.goto(page.url().split("?")[0] ?? "/code-graph", { waitUntil: "domcontentloaded" });
  await page.waitForSelector(".page-shell", { timeout: 10000 });

  const repoSelect = page.getByRole("combobox", { name: "Repository" });
  await page.waitForFunction(
    () => new URL(window.location.href).searchParams.get("repo_id") === "repository:checkout-service",
    { timeout: 10000 },
  );
  await page.waitForFunction(
    () => document.querySelectorAll('select[aria-label="Repository"] option').length >= 2,
    { timeout: 10000 },
  );
  await page.waitForFunction(
    () =>
      (document.querySelector<HTMLSelectElement>('select[aria-label="Symbol"]')?.value ?? "")
        .includes("raceAlphaSymbol"),
    { timeout: 10000 },
  );

  // Arm the MutationObserver BEFORE switching repositories -- it must be
  // watching from before the DOM mutation it needs to catch.
  await page.evaluate(() => {
    const symbolSelect = document.querySelector<HTMLSelectElement>('select[aria-label="Symbol"]');
    if (!symbolSelect) throw new Error("symbol select not found");
    const observer = new MutationObserver(() => {
      if (symbolSelect.value.includes("raceBetaSymbol")) {
        observer.disconnect();
        window.history.back();
      }
    });
    observer.observe(symbolSelect, { attributes: true, childList: true, subtree: true });
  });

  await repoSelect.selectOption(REPO_B);
  await page.waitForTimeout(300);

  const finalUrl = new URL(page.url());
  const finalRepoId = finalUrl.searchParams.get("repo_id");
  const selectedRepoValue = await repoSelect.inputValue();

  if (finalRepoId !== REPO_A || selectedRepoValue !== REPO_A) {
    return (
      `back navigation did not restore ${REPO_A} scope: ` +
      `url repo_id=${finalRepoId ?? "none"}, selector value=${selectedRepoValue}`
    );
  }
  return "";
}

export const pageTest: PageTest = {
  path: "/code-graph",
  label: "Code Graph back/forward race (#5833)",
  area: "graph",
  async assert(page: Page): Promise<void> {
    const handler = inventoryMockHandler();
    await page.route("**/eshu-api/**", handler);
    try {
      for (let trial = 0; trial < TRIALS; trial += 1) {
        const failure = await runTrial(page);
        if (failure) throw new Error(`trial ${trial + 1}/${TRIALS}: ${failure}`);
      }
    } finally {
      await page.unroute("**/eshu-api/**", handler);
    }
  },
};
