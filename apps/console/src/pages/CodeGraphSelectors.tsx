import { useEffect, useMemo, useState } from "react";

import { symbolFromFinding } from "./CodeGraphPageSupport";
import type { RepoListItem } from "../api/repoCatalog";
import type { FindingRow } from "../console/types";

export function CodeGraphSelectors({
  loading,
  onEntityChange,
  onRepositoryChange,
  repositories,
  selectedEntityId,
  selectedRepositoryId,
  symbols,
}: {
  readonly loading: boolean;
  readonly onEntityChange: (entityId: string) => void;
  readonly onRepositoryChange: (repoId: string) => void;
  readonly repositories: readonly RepoListItem[];
  readonly selectedEntityId: string;
  readonly selectedRepositoryId: string;
  readonly symbols: readonly FindingRow[];
}): React.JSX.Element {
  const [repositoryQuery, setRepositoryQuery] = useState("");
  const [symbolQuery, setSymbolQuery] = useState("");
  useEffect(() => setSymbolQuery(""), [selectedRepositoryId]);
  const visibleRepositories = useMemo(
    () =>
      filterWithSelection(
        repositories,
        repositoryQuery,
        selectedRepositoryId,
        (repository) => `${repository.name} ${repository.repoSlug} ${repository.id}`,
        (repository) => repository.id,
      ),
    [repositories, repositoryQuery, selectedRepositoryId],
  );
  const visibleSymbols = useMemo(
    () =>
      filterWithSelection(
        symbols,
        symbolQuery,
        selectedEntityId,
        (symbol) => `${symbolFromFinding(symbol)} ${symbol.filePath ?? ""}`,
        (symbol) => symbol.entityId ?? symbol.id,
      ),
    [selectedEntityId, symbolQuery, symbols],
  );
  return (
    <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
      <input
        aria-label="Search repositories"
        className="code-repo-select mono"
        placeholder="Search repositories…"
        type="search"
        value={repositoryQuery}
        onChange={(event) => setRepositoryQuery(event.target.value)}
      />
      <select
        aria-label="Repository"
        className="code-repo-select mono"
        value={selectedRepositoryId}
        onChange={(event) => {
          setSymbolQuery("");
          onRepositoryChange(event.target.value);
        }}
      >
        {repositories.length === 0 ? <option value="">No repositories available</option> : null}
        {repositories.length > 0 && !selectedRepositoryId ? (
          <option value="">Requested repository unavailable</option>
        ) : null}
        {visibleRepositories.map((repository) => (
          <option key={repository.id} value={repository.id}>
            {repository.name}
          </option>
        ))}
      </select>
      <input
        aria-label="Search symbols"
        className="code-repo-select mono"
        disabled={!selectedRepositoryId || loading || symbols.length === 0}
        placeholder="Search symbols…"
        type="search"
        value={symbolQuery}
        onChange={(event) => setSymbolQuery(event.target.value)}
      />
      <select
        aria-label="Symbol"
        className="code-repo-select mono"
        disabled={!selectedRepositoryId || loading || symbols.length === 0}
        value={selectedEntityId}
        onChange={(event) => onEntityChange(event.target.value)}
      >
        {loading ? <option value="">Loading symbols…</option> : null}
        {!loading && symbols.length === 0 ? <option value="">No symbols available</option> : null}
        {!loading && symbols.length > 0 && !selectedEntityId ? (
          <option value="">Requested symbol unavailable</option>
        ) : null}
        {visibleSymbols.map((symbol) => (
          <option key={symbol.entityId ?? symbol.id} value={symbol.entityId ?? symbol.id}>
            {symbolFromFinding(symbol)}
          </option>
        ))}
      </select>
    </div>
  );
}

function filterWithSelection<T>(
  rows: readonly T[],
  query: string,
  selectedId: string,
  searchText: (row: T) => string,
  rowId: (row: T) => string,
): readonly T[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return rows;
  return rows.filter(
    (row) => rowId(row) === selectedId || searchText(row).toLowerCase().includes(needle),
  );
}
