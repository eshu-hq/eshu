import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { EshuApiClient } from "../api/client";
import { loadCatalogRows, loadCatalogServiceRows } from "../api/liveData";
import type { CatalogRow, EntityKind } from "../api/mockData";
import { loadConsoleEnvironment } from "../config/environment";

type CatalogFilter = EntityKind | "all";

export function CatalogPage(): React.JSX.Element {
  const [activeKind, setActiveKind] = useState<CatalogFilter>("all");
  const [rows, setRows] = useState<readonly CatalogRow[]>([]);
  const [query, setQuery] = useState("");
  const [loadState, setLoadState] = useState<"loading" | "ready" | "unavailable">(
    "loading"
  );

  useEffect(() => {
    let cancelled = false;
    const environment = loadConsoleEnvironment();
    const client =
      environment.mode === "private"
        ? new EshuApiClient({
          apiKey: environment.apiKey,
          baseUrl: environment.apiBaseUrl
        })
        : undefined;
    void loadCatalogRows({ client, mode: environment.mode })
      .then((loadedRows) => {
        if (cancelled) {
          return;
        }
        setRows(loadedRows);
        setLoadState("ready");
        if (client !== undefined && catalogNeedsStoryFallback(loadedRows)) {
          void loadCatalogServiceRows({
            client,
            mode: environment.mode,
            onRows: (serviceRows) => {
              if (!cancelled) {
                setRows((currentRows) => mergeCatalogRows(serviceRows, currentRows));
              }
            }
          })
            .then((serviceRows) => {
              if (!cancelled) {
                setRows((currentRows) => mergeCatalogRows(serviceRows, currentRows));
              }
            })
            .catch(() => undefined);
        }
      })
      .catch(() => {
        if (cancelled) {
          return;
        }
        setRows([]);
        setLoadState("unavailable");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const filteredRows = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    return rows.filter((row) => {
      if (activeKind !== "all" && row.kind !== activeKind) {
        return false;
      }
      if (normalized.length === 0) {
        return true;
      }
      return `${row.name} ${row.kind} ${row.coverage}`.toLowerCase().includes(normalized);
    });
  }, [activeKind, query, rows]);
  const counts = useMemo(() => catalogCounts(rows), [rows]);
  const pageState = useMemo(() => catalogPageState(rows), [rows]);

  return (
    <section className="page-shell">
      <div className="page-intro">
        <h1>Catalog</h1>
        <p>Browse repositories, services, and workload identities from the live graph.</p>
      </div>
      {loadState === "loading" ? <p className="inline-state">Loading live data.</p> : null}
      {loadState === "unavailable" ? (
        <p className="inline-state">Local Eshu API unavailable.</p>
      ) : null}
      <div aria-label="Catalog entity types" className="catalog-kind-tabs">
        {catalogFilters.map((filter) => (
          <button
            aria-pressed={activeKind === filter.kind}
            key={filter.kind}
            onClick={() => setActiveKind(filter.kind)}
            type="button"
          >
            <span>{filter.label}</span>
            <strong>{filter.count(counts)}</strong>
          </button>
        ))}
      </div>
      <div className="table-toolbar">
        <label>
          <span>Search catalog</span>
          <input
            aria-label="Search catalog"
            onChange={(event) => setQuery(event.currentTarget.value)}
            placeholder="Filter by name, kind, or evidence"
            value={query}
          />
        </label>
        <div className="catalog-page-state" aria-label="Catalog paging state">
          <span>Offset {pageState.offset}</span>
          <span>Limit {pageState.limit}</span>
          <strong>{pageState.truncated ? "More available" : `${filteredRows.length} shown`}</strong>
        </div>
      </div>
      <table className="data-table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Kind</th>
            <th>Freshness</th>
            <th>Coverage</th>
          </tr>
        </thead>
        <tbody>
          {filteredRows.map((row) => (
            <tr key={`${row.kind}:${row.id}`}>
              <td>
                <Link
                  aria-label={`Open ${row.name} workspace`}
                  to={`/workspace/${row.kind}/${encodeURIComponent(row.id)}`}
                >
                  {row.name}
                </Link>
              </td>
              <td>{row.kind}</td>
              <td>{row.freshness}</td>
              <td>{row.coverage}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

function catalogPageState(rows: readonly CatalogRow[]): {
  readonly limit: number;
  readonly offset: number;
  readonly truncated: boolean;
} {
  const row = rows.find((candidate) =>
    candidate.limit !== undefined ||
    candidate.offset !== undefined ||
    candidate.truncated !== undefined
  );
  return {
    limit: row?.limit ?? rows.length,
    offset: row?.offset ?? 0,
    truncated: row?.truncated ?? false
  };
}

interface CatalogCounts {
  readonly all: number;
  readonly repositories: number;
  readonly services: number;
  readonly workloads: number;
}

const catalogFilters: readonly {
  readonly count: (counts: CatalogCounts) => number;
  readonly kind: CatalogFilter;
  readonly label: string;
}[] = [
  { count: (counts) => counts.all, kind: "all", label: "All" },
  { count: (counts) => counts.repositories, kind: "repositories", label: "Repositories" },
  { count: (counts) => counts.services, kind: "services", label: "Services" },
  { count: (counts) => counts.workloads, kind: "workloads", label: "Workloads" }
];

function catalogCounts(rows: readonly CatalogRow[]): CatalogCounts {
  return {
    all: rows.length,
    repositories: rows.filter((row) => row.kind === "repositories").length,
    services: rows.filter((row) => row.kind === "services").length,
    workloads: rows.filter((row) => row.kind === "workloads").length
  };
}

function mergeCatalogRows(
  newRows: readonly CatalogRow[],
  currentRows: readonly CatalogRow[]
): readonly CatalogRow[] {
  const seen = new Set<string>();
  const merged: CatalogRow[] = [];
  for (const row of [...newRows, ...currentRows]) {
    const key = `${row.kind}:${row.id}`;
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    merged.push(row);
  }
  return merged;
}

function catalogNeedsStoryFallback(rows: readonly CatalogRow[]): boolean {
  return !rows.some((row) => row.kind === "services" || row.kind === "workloads");
}
