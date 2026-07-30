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
    // Make that impossible by construction instead of by timing: stamp the
    // write with the exact `searchParams` it was computed from, and check
    // that against the navigator's OWN current location, read fresh, right
    // here, right before writing. `searchParams` (the value from
    // `useSearchParams`) only updates when THIS component re-renders; the
    // navigator's `location` is mutated synchronously by any push, replace,
    // or back/forward the instant it is issued, independent of whether or
    // when React has re-rendered this component for it. So this comparison
    // can never be stale relative to this effect's own timing -- if the live
    // location no longer matches what `canonical` was derived from, some
    // other navigation has unconditionally already happened, and skipping is
    // correct regardless of which of the two ran "first". (`Navigator`, the
    // public type for `navigator`, omits `.location` specifically to steer
    // application code away from reading it to decide what to RENDER, where
    // it could tear across a single render pass under Suspense; this reads
    // it once, inside a post-commit effect, purely to gate an imperative
    // write, which does not have that render-time tearing hazard.)
    if (readLiveLocationSearch(navigator) !== searchParams.toString()) return;
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
 * `NavigationContext`) deliberately does not declare `.location`: the object
 * behind it always has one (every history instance -- browser or memory --
 * conforms to the fuller `History` interface `Navigator` is trimmed from),
 * but the trimmed type steers render-time code away from reading it, since a
 * direct read used to decide what to render could tear across a single
 * render pass under Suspense-driven concurrent rendering. This function is
 * only ever called from inside a post-commit effect to gate an imperative
 * write against a stale snapshot, never to decide what to render, so that
 * tearing hazard does not apply -- and unlike `searchParams` from
 * `useSearchParams` (which only updates once this component re-renders), the
 * navigator's location is mutated synchronously the instant any push,
 * replace, or history.go is issued, so this read is never behind React's own
 * render timing.
 */
function readLiveLocationSearch(navigator: Navigator): string {
  const liveNavigator = navigator as unknown as { readonly location: { readonly search: string } };
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
