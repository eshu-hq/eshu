import { useEffect, useMemo, useState } from "react";

import type { RepoListItem } from "../api/repoCatalog";
import type { RepositoryCatalogState } from "../repositoryCatalogLifecycle";

export function SemanticRepositorySelector({
  catalog,
  onChange,
  searchHint,
  selectedRepositoryId,
}: {
  readonly catalog: RepositoryCatalogState;
  readonly onChange: (repoId: string) => void;
  readonly searchHint: string;
  readonly selectedRepositoryId: string;
}): React.JSX.Element {
  const [query, setQuery] = useState(searchHint);
  useEffect(() => setQuery(searchHint), [searchHint]);

  const visibleRepositories = useMemo(
    () => filterRepositories(catalog.repositories, query, selectedRepositoryId),
    [catalog.repositories, query, selectedRepositoryId],
  );
  const labels = useMemo(() => repositoryLabels(catalog.repositories), [catalog.repositories]);
  const disabled = catalog.kind !== "ready" || catalog.repositories.length === 0;

  return (
    <div className="semantic-repository-selector">
      <input
        aria-label="Search repositories"
        className="popover-input mono"
        disabled={disabled}
        placeholder="Search repositories…"
        type="search"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
      />
      <select
        aria-label="Repository"
        className="popover-input mono"
        disabled={disabled}
        value={selectedRepositoryId}
        onChange={(event) => onChange(event.target.value)}
      >
        <option value="">{placeholder(catalog, selectedRepositoryId)}</option>
        {visibleRepositories.map((repository) => (
          <option key={repository.id} value={repository.id}>
            {labels.get(repository.id) ?? repository.name}
          </option>
        ))}
      </select>
    </div>
  );
}

function filterRepositories(
  repositories: readonly RepoListItem[],
  query: string,
  selectedRepositoryId: string,
): readonly RepoListItem[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return repositories;
  return repositories.filter(
    (repository) =>
      repository.id === selectedRepositoryId ||
      `${repository.name} ${repository.repoSlug} ${repository.id}`.toLowerCase().includes(needle),
  );
}

function repositoryLabels(repositories: readonly RepoListItem[]): ReadonlyMap<string, string> {
  const nameCounts = countBy(repositories, (repository) => repository.name);
  const detailCounts = countBy(
    repositories,
    (repository) => `${repository.name}\u0000${repository.repoSlug || repository.id}`,
  );
  return new Map(
    repositories.map((repository) => {
      if ((nameCounts.get(repository.name) ?? 0) === 1) return [repository.id, repository.name];
      const detail = repository.repoSlug || repository.id;
      const base = `${repository.name} — ${detail}`;
      return [
        repository.id,
        (detailCounts.get(`${repository.name}\u0000${detail}`) ?? 0) === 1
          ? base
          : `${base} — ${repository.id}`,
      ];
    }),
  );
}

function countBy(
  repositories: readonly RepoListItem[],
  key: (repository: RepoListItem) => string,
): ReadonlyMap<string, number> {
  const counts = new Map<string, number>();
  for (const repository of repositories) {
    const value = key(repository);
    counts.set(value, (counts.get(value) ?? 0) + 1);
  }
  return counts;
}

function placeholder(catalog: RepositoryCatalogState, selectedRepositoryId: string): string {
  if (catalog.kind === "loading") return "Repository catalog loading…";
  if (catalog.kind === "unavailable") return "Repository catalog unavailable";
  if (catalog.repositories.length === 0) return "No authorized repositories available";
  if (selectedRepositoryId === "") return "Select a repository";
  return "Requested repository unavailable";
}
