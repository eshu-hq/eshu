import { useContext, useEffect, useMemo, useState } from "react";
import { UNSAFE_NavigationContext as NavigationContext, useSearchParams } from "react-router";
import type { Navigator } from "react-router";

import { candidateIdFromParam } from "./CodeGraphPageSupport";
import type { EshuApiClient } from "../api/client";
import { loadCodeGraphInventory } from "../api/codeGraphLoader";
import type { RepoListItem } from "../api/repoCatalog";
import type { ConsoleModel, FindingRow } from "../console/types";
import type { RepositoryCatalogState } from "../repositoryCatalogLifecycle";

export interface CodeGraphSelection {
  readonly error: string;
  readonly loading: boolean;
  readonly repositories: readonly RepoListItem[];
  readonly repository: RepoListItem | undefined;
  readonly retry: () => void;
  readonly selectEntity: (entityId: string) => void;
  readonly selectRepository: (repoId: string) => void;
  readonly selected: FindingRow | undefined;
  readonly symbols: readonly FindingRow[];
  readonly truncated: boolean;
}

interface InventoryState {
  readonly error: string;
  readonly repoId: string;
  readonly status: "idle" | "loading" | "ready" | "error";
  readonly symbols: readonly FindingRow[];
  readonly truncated: boolean;
}

const emptyInventoryState: InventoryState = {
  error: "",
  repoId: "",
  status: "idle",
  symbols: [],
  truncated: false,
};

export function useCodeGraphSelection({
  client,
  deadCandidates,
  model,
  repositories,
  repositoryCatalog,
}: {
  readonly client?: EshuApiClient;
  readonly deadCandidates: readonly FindingRow[];
  readonly model: ConsoleModel;
  readonly repositories?: readonly RepoListItem[];
  readonly repositoryCatalog?: RepositoryCatalogState;
}): CodeGraphSelection {
  const fallbackRepositories = useMemo(
    () => repositoriesFromFindings(deadCandidates),
    [deadCandidates],
  );
  const availableRepositories = repositories ?? fallbackRepositories;
  const { navigator } = useContext(NavigationContext);
  const [searchParams, setSearchParams] = useSearchParams();
  const legacyCandidateParam = searchParams.get("candidate") ?? searchParams.get("q") ?? "";
  const legacyCandidateId = candidateIdFromParam(deadCandidates, legacyCandidateParam);
  const legacyCandidate = deadCandidates.find((finding) => finding.id === legacyCandidateId);
  const repoParam = searchParams.get("repo_id") ?? "";
  const entityParam = searchParams.get("entity_id") ?? "";
  const legacyRepositories = legacyCandidate
    ? availableRepositories.filter((repo) => findingMatchesRepository(legacyCandidate, repo))
    : [];
  const legacyRepository = legacyRepositories.length === 1 ? legacyRepositories[0] : undefined;
  const invalidLegacyCandidate =
    legacyCandidateParam.trim() !== "" && legacyCandidate === undefined && repoParam === "";
  const unavailableLegacyRepository =
    legacyCandidateParam.trim() !== "" &&
    legacyCandidate !== undefined &&
    legacyRepository === undefined &&
    legacyRepositories.length === 0 &&
    repoParam === "";
  const ambiguousLegacyRepository =
    legacyCandidateParam.trim() !== "" &&
    legacyCandidate !== undefined &&
    legacyRepositories.length > 1 &&
    repoParam === "";
  const requestedRepoId =
    repoParam ||
    legacyRepository?.id ||
    (invalidLegacyCandidate || unavailableLegacyRepository || ambiguousLegacyRepository
      ? ""
      : availableRepositories[0]?.id) ||
    "";
  const repository = availableRepositories.find(
    (repo) => repo.id === requestedRepoId || (repoParam !== "" && repo.name === requestedRepoId),
  );
  const [inventory, setInventory] = useState<InventoryState>(emptyInventoryState);
  const [retryNonce, setRetryNonce] = useState(0);

  useEffect(() => {
    let cancelled = false;
    if (!repository) {
      setInventory(emptyInventoryState);
      return () => {
        cancelled = true;
      };
    }
    if (!client || model.source !== "live") {
      setInventory({
        error: "",
        repoId: repository.id,
        status: "ready",
        symbols: deadCandidates
          .filter((finding) => findingMatchesRepository(finding, repository))
          .map((finding) => ({ ...finding, repoId: finding.repoId ?? repository.id })),
        truncated: false,
      });
      return () => {
        cancelled = true;
      };
    }
    setInventory({ ...emptyInventoryState, repoId: repository.id, status: "loading" });
    void loadCodeGraphInventory(client, repository.id, repository.name)
      .then((inventory) => {
        if (cancelled) return;
        setInventory({
          error: "",
          repoId: repository.id,
          status: "ready",
          symbols: inventory.symbols,
          truncated: inventory.truncated,
        });
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setInventory({
            error: error instanceof Error ? error.message : "failed to load code inventory",
            repoId: repository.id,
            status: "error",
            symbols: [],
            truncated: false,
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [client, deadCandidates, model.source, repository, retryNonce]);

  const activeInventory =
    repository && inventory.repoId === repository.id
      ? inventory
      : repository
        ? { ...emptyInventoryState, repoId: repository.id, status: "loading" as const }
        : emptyInventoryState;
  const symbols = activeInventory.symbols;
  const loading = activeInventory.status === "loading";

  const legacyEntityId =
    legacyCandidate && repository && findingMatchesRepository(legacyCandidate, repository)
      ? (legacyCandidate.entityId ?? legacyCandidate.id)
      : "";
  const requestedEntityId = entityParam || legacyEntityId;
  const selected = useMemo(
    () =>
      symbols.find(
        (symbol) => symbol.entityId === requestedEntityId || symbol.id === requestedEntityId,
      ) ??
      (entityParam
        ? (deadCandidates.find(
            (finding) =>
              (finding.entityId === entityParam || finding.id === entityParam) &&
              repository !== undefined &&
              findingRepoId(finding) === repository.id,
          ) ??
          (repository && activeInventory.status === "ready"
            ? requestedEntityFinding(repository, entityParam)
            : undefined))
        : symbols[0]),
    [activeInventory.status, deadCandidates, entityParam, repository, requestedEntityId, symbols],
  );

  useEffect(() => {
    if (!repository || loading) return;
    if (entityParam && !selected) return;
    const entityId = selected?.entityId ?? selected?.id ?? "";
    const canonical = new URLSearchParams(searchParams);
    canonical.set("repo_id", repository.id);
    if (entityId) canonical.set("entity_id", entityId);
    else canonical.delete("entity_id");
    canonical.delete("candidate");
    canonical.delete("q");
    if (canonical.toString() === searchParams.toString()) return;
    // `setSearchParams(..., { replace: true })` replaces whichever history
    // entry the navigator considers current AT THE MOMENT IT RUNS -- not the
    // entry that was current when this effect's closure captured
    // `repository`/`selected`/`searchParams`. If a different navigation
    // (browser back/forward, or picking a different repository/entity) has
    // already landed since this effect's own snapshot was taken, writing
    // `canonical` now would replace that freshly-navigated-to entry with this
    // effect's stale one.
    //
    // Guard against that by stamping the write with the exact `searchParams`
    // it was computed from and comparing it against the navigator's OWN
    // current location, read fresh, right here, right before writing.
    // `searchParams` (the value from `useSearchParams`) only updates when
    // THIS component re-renders. Whether the live read below is itself fresh
    // is router-mode-dependent: under the `MemoryRouter` used in tests,
    // `createMemoryHistory.go(delta)` updates `.location` synchronously,
    // before notifying listeners, so a same-tick read is never stale. Under
    // the `BrowserRouter` this console actually renders with
    // (`apps/console/src/main.tsx`), `.location` is a live read of
    // `window.location`, and `go(n)` calls the native `history.go(n)`, whose
    // traversal is asynchronous -- MDN documents `popstate` firing
    // "asynchronously ... outside the event handler" that invoked
    // back()/go(). Measurement in real Chromium found a consistent
    // ~0.3-2ms gap between the `history.back()` call and both
    // `window.location` updating and `popstate` firing, and this effect can
    // run inside that gap, so the guard's own decision to write is
    // sometimes wrong -- it can compare against a location that has not yet
    // caught up with a pending back/forward traversal.
    //
    // The comparison below is a guard against the common case, not a proof
    // that a stale write is impossible under `BrowserRouter`. What keeps the
    // final URL correct even when the guard's decision is wrong: native
    // `history.replaceState` replaces whichever entry is current AT CALL
    // TIME -- the entry the pending traversal is about to abandon, not the
    // entry it is about to land on. That is the leading explanation for why
    // the outcome stays correct even in the stale-read case; it is supported
    // by measurement, not by a cited spec or react-router guarantee, so
    // treat it as empirical, not as a proof.
    //
    // Empirical outcome in real Chromium against production `BrowserRouter`:
    // the reverted (pre-fix) hook clobbers the URL in 8/30 rapid
    // back/forward trials (~27%); this fixed hook clobbers in 0/30.
    // (`Navigator`, the public type for `navigator`, omits `.location`
    // specifically to steer application code away from reading it to decide
    // what to RENDER, where it could tear across a single render pass under
    // Suspense; this reads it once, inside a post-commit effect, purely to
    // gate an imperative write, which does not have that render-time tearing
    // hazard.)
    const liveLocationSearch = readLiveLocationSearch(navigator);
    if (liveLocationSearch === undefined || liveLocationSearch !== searchParams.toString()) return;
    setSearchParams(canonical, { replace: true });
  }, [entityParam, loading, navigator, repository, searchParams, selected, setSearchParams]);

  const error =
    repositoryError(repository, repoParam, repositoryCatalog) ||
    (invalidLegacyCandidate
      ? `Legacy Code Graph candidate ${legacyCandidateParam} is not available.`
      : "") ||
    (unavailableLegacyRepository
      ? `Legacy Code Graph candidate ${legacyCandidateParam} repository is not present in this session catalog.`
      : "") ||
    (ambiguousLegacyRepository
      ? `Legacy Code Graph candidate ${legacyCandidateParam} repository label matches multiple repositories.`
      : "") ||
    activeInventory.error;
  return {
    error,
    loading,
    repositories: availableRepositories,
    repository,
    retry: (): void => setRetryNonce((current) => current + 1),
    selectEntity: (entityId: string): void => {
      const next = new URLSearchParams(searchParams);
      if (entityId) next.set("entity_id", entityId);
      else next.delete("entity_id");
      next.delete("candidate");
      next.delete("q");
      setSearchParams(next);
    },
    selectRepository: (repoId: string): void => {
      const next = new URLSearchParams(searchParams);
      if (repoId) next.set("repo_id", repoId);
      else next.delete("repo_id");
      next.delete("entity_id");
      next.delete("candidate");
      next.delete("q");
      setSearchParams(next);
    },
    selected,
    symbols,
    truncated: activeInventory.truncated,
  };
}

/**
 * Reads the navigator's current location search string, normalized the same
 * way `useSearchParams`'s `searchParams.toString()` is, so the two are
 * directly comparable.
 *
 * `Navigator` (react-router's public type for the value handed down through
 * `NavigationContext`) deliberately does not declare `.location`, to steer
 * render-time code away from reading it -- a direct read used to decide what
 * to render could tear across a single render pass under Suspense-driven
 * concurrent rendering. This function is only ever called from inside a
 * post-commit effect to gate an imperative write against a stale snapshot,
 * never to decide what to render, so that tearing hazard does not apply.
 *
 * Freshness relative to `searchParams` (from `useSearchParams`, which only
 * updates once this component re-renders) is router-mode-dependent, not a
 * blanket guarantee. Under `MemoryRouter` (used in tests),
 * `createMemoryHistory.go(delta)` updates `.location` synchronously, before
 * notifying listeners, so a same-tick read is never stale. Under
 * `BrowserRouter` (what this console actually renders with, see
 * `apps/console/src/main.tsx`), `.location` is a live read of
 * `window.location`, and `go(n)` calls the native `history.go(n)`; that
 * native traversal is asynchronous (`popstate` fires "asynchronously ...
 * outside the event handler" that invoked it, per MDN), so this read can
 * observe a stale value for the few milliseconds before the browser catches
 * up. The caller's comparison against this value is therefore a guard
 * against the common case, not a proof that a stale read is impossible
 * under `BrowserRouter` -- see the comment at the call site for the
 * measured gap and the empirical 0/30-vs-8/30 result that motivates the
 * guard anyway.
 *
 * Whether the object behind `navigator` actually HAS `.location` depends on
 * the router mode -- verified against pinned react-router 8.3.0's own source
 * (`lib/components.js`), not assumed. The three declarative routers --
 * `BrowserRouter`, `MemoryRouter`, `HashRouter` -- pass their underlying
 * `history` instance straight through as `navigator`, and `history` always
 * carries `.location`. `RouterProvider` (the data-router mode from
 * `createBrowserRouter` / `createMemoryRouter`, react-router's recommended
 * API since v6.4 and required for loaders/actions/lazy routes) instead passes
 * a plain memoized object -- `{ createHref, encodeLocation, go, push,
 * replace }` -- with NO `.location` property at all
 * (`lib/components.js:256-271`). This console currently renders through
 * `<BrowserRouter>` (`apps/console/src/main.tsx`) and tests through
 * `<MemoryRouter>`, so `.location` is present today, but nothing pins the
 * console to that router mode forever.
 *
 * Because of that, this function treats an absent `.location` as "freshness
 * cannot be verified" and returns `undefined` instead of dereferencing it and
 * throwing. The caller below skips the canonicalizing write whenever this
 * returns `undefined`, the same fail-safe direction already taken when the
 * live location is present but simply does not match.
 */
function readLiveLocationSearch(navigator: Navigator): string | undefined {
  const liveNavigator = navigator as unknown as { readonly location?: { readonly search: string } };
  if (!("location" in liveNavigator) || liveNavigator.location === undefined) return undefined;
  return new URLSearchParams(liveNavigator.location.search).toString();
}

function findingRepoId(finding: FindingRow): string {
  return finding.repoId?.trim() || finding.entity.trim();
}

function findingMatchesRepository(finding: FindingRow, repository: RepoListItem): boolean {
  const scope = findingRepoId(finding);
  return scope === repository.id || scope === repository.name || scope === repository.repoSlug;
}

function requestedEntityFinding(repository: RepoListItem, entityId: string): FindingRow {
  return {
    detail: "source metadata pending relationship lookup",
    entity: repository.name,
    entityId,
    id: entityId,
    repoId: repository.id,
    title: entityId,
    truth: "derived",
    type: "Code symbol",
  };
}

function repositoriesFromFindings(findings: readonly FindingRow[]): readonly RepoListItem[] {
  const repos = new Map<string, RepoListItem>();
  for (const finding of findings) {
    const id = findingRepoId(finding);
    if (!id || repos.has(id)) continue;
    repos.set(id, {
      groupKey: "snapshot",
      groupKind: "snapshot",
      groupReason: "code graph compatibility",
      groupSource: "console snapshot",
      groupTruth: finding.truth,
      id,
      isDependency: false,
      name: finding.entity || id,
      remoteUrl: "",
      repoSlug: "",
    });
  }
  return [...repos.values()];
}

function repositoryError(
  repository: RepoListItem | undefined,
  repoParam: string,
  catalog: RepositoryCatalogState | undefined,
): string {
  if (repoParam && !repository)
    return `Repository ${repoParam} is not present in this session catalog.`;
  if (catalog?.kind === "loading") return "Repository catalog is still loading.";
  if (catalog?.kind === "unavailable") return `Repository catalog unavailable: ${catalog.error}`;
  if (catalog?.kind === "ready" && catalog.repositories.length === 0) {
    return "No authorized repositories are available in this session.";
  }
  return "";
}
