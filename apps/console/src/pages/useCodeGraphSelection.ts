import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

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
  const [searchParams, setSearchParams] = useSearchParams();
  const legacyCandidateParam = searchParams.get("candidate") ?? searchParams.get("q") ?? "";
  const legacyCandidateId = candidateIdFromParam(deadCandidates, legacyCandidateParam);
  const legacyCandidate = deadCandidates.find((finding) => finding.id === legacyCandidateId);
  const repoParam = searchParams.get("repo_id") ?? "";
  const entityParam = searchParams.get("entity_id") ?? "";
  const invalidLegacyCandidate =
    legacyCandidateParam.trim() !== "" && legacyCandidate === undefined && repoParam === "";
  const requestedRepoId =
    repoParam ||
    legacyCandidate?.repoId ||
    (invalidLegacyCandidate ? "" : availableRepositories[0]?.id) ||
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
    if (canonical.toString() !== searchParams.toString())
      setSearchParams(canonical, { replace: true });
  }, [entityParam, loading, repository, searchParams, selected, setSearchParams]);

  const error =
    repositoryError(repository, repoParam, repositoryCatalog) ||
    (invalidLegacyCandidate
      ? `Legacy Code Graph candidate ${legacyCandidateParam} is not available.`
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
