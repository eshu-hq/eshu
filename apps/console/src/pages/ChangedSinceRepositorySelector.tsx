import { useMemo, useState } from "react";

import type { RepoListItem } from "../api/repoCatalog";

export function ChangedSinceRepositorySelector({
  onChange,
  repositories,
  selectedRepositoryId,
}: {
  readonly onChange: (repositoryId: string) => void;
  readonly repositories: readonly RepoListItem[];
  readonly selectedRepositoryId: string;
}): React.JSX.Element {
  const [query, setQuery] = useState("");
  const labels = useMemo(() => repositoryLabels(repositories), [repositories]);
  const visible = useMemo(
    () => filterRepositories(repositories, query, selectedRepositoryId),
    [query, repositories, selectedRepositoryId],
  );

  return (
    <div className="changed-since-repository-selector">
      <label>
        <span>Search repositories</span>
        <input
          aria-label="Search repositories"
          className="popover-input"
          disabled={repositories.length === 0}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Name, slug, or canonical ID"
          type="search"
          value={query}
        />
      </label>
      <label>
        <span>Repository</span>
        <select
          aria-label="Repository"
          className="popover-input mono"
          disabled={repositories.length === 0}
          onChange={(event) => onChange(event.target.value)}
          value={selectedRepositoryId}
        >
          <option value="">Choose a repository</option>
          {visible.map((repository) => (
            <option key={repository.id} value={repository.id}>
              {labels.get(repository.id) ?? repository.name}
            </option>
          ))}
        </select>
      </label>
    </div>
  );
}

export function changedSinceRepositoryLabel(
  repositories: readonly RepoListItem[],
  repositoryId: string,
): string {
  const repository = repositories.find((candidate) => candidate.id === repositoryId);
  if (!repository) return repositoryId;
  const primary = repository.name.trim() || repository.repoSlug.trim() || repository.id;
  return primary === repository.id ? primary : `${primary} · ${repository.id}`;
}

function filterRepositories(
  repositories: readonly RepoListItem[],
  query: string,
  selectedRepositoryId: string,
): readonly RepoListItem[] {
  const needle = query.trim().toLowerCase();
  if (needle === "") return repositories;
  return repositories.filter((repository) => {
    if (repository.id === selectedRepositoryId) return true;
    return [repository.name, repository.repoSlug, repository.id].some((value) =>
      value.toLowerCase().includes(needle),
    );
  });
}

function repositoryLabels(repositories: readonly RepoListItem[]): ReadonlyMap<string, string> {
  return new Map(
    repositories.map((repository) => {
      const name = repository.name.trim() || repository.repoSlug.trim() || repository.id;
      const label = name === repository.id ? name : `${name} · ${repository.id}`;
      return [repository.id, label];
    }),
  );
}
