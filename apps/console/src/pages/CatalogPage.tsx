import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { EshuApiClient } from "../api/client";
import { loadCatalogRows, loadCatalogServiceRows } from "../api/liveData";
import type { CatalogRow, EntityKind } from "../api/mockData";
import { loadConsoleEnvironment } from "../config/environment";

type CatalogFilter = EntityKind | "all";

export function CatalogPage(): React.JSX.Element {
  const [activeKind, setActiveKind] = useState<CatalogFilter>("all");
  const [selectedEnvironment, setSelectedEnvironment] = useState("all");
  const [selectedRowKey, setSelectedRowKey] = useState<string | undefined>(undefined);
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
      if (
        selectedEnvironment !== "all" &&
        !(row.environments ?? []).includes(selectedEnvironment)
      ) {
        return false;
      }
      if (normalized.length === 0) {
        return true;
      }
      return [
        row.name,
        row.kind,
        row.coverage,
        row.ownerRepo,
        row.workloadKind,
        row.materializationStatus,
        ...(row.environments ?? [])
      ]
        .join(" ")
        .toLowerCase()
        .includes(normalized);
    });
  }, [activeKind, query, rows, selectedEnvironment]);
  const counts = useMemo(() => catalogCounts(rows), [rows]);
  const coverage = useMemo(() => catalogCoverage(rows), [rows]);
  const pageState = useMemo(() => catalogPageState(rows), [rows]);
  const selectedRow = useMemo(
    () => filteredRows.find((row) => catalogRowKey(row) === selectedRowKey),
    [filteredRows, selectedRowKey]
  );

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
      <section aria-label="Catalog coverage" className="catalog-coverage-strip">
        <div>
          <span>Entries</span>
          <strong>{coverage.entries} catalog entries</strong>
        </div>
        <div>
          <span>Environments</span>
          <strong>{coverage.environmentCount} environments</strong>
        </div>
        <div>
          <span>Identity coverage</span>
          <strong>{coverage.identityOnlyCount} identity-only</strong>
        </div>
      </section>
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
        <label>
          <span>Environment</span>
          <select
            aria-label="Filter catalog by environment"
            onChange={(event) => setSelectedEnvironment(event.currentTarget.value)}
            value={selectedEnvironment}
          >
            <option value="all">All environments</option>
            {coverage.environments.map((environment) => (
              <option key={environment} value={environment}>
                {environment}
              </option>
            ))}
          </select>
        </label>
        <div className="catalog-page-state" aria-label="Catalog paging state">
          <span>Offset {pageState.offset}</span>
          <span>Limit {pageState.limit}</span>
          <strong>{pageState.truncated ? "More available" : `${filteredRows.length} shown`}</strong>
        </div>
      </div>
      <div className={selectedRow === undefined ? "catalog-workbench catalog-workbench-empty" : "catalog-workbench"}>
        <div className="catalog-results">
          <table className="data-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Kind</th>
                <th>Freshness</th>
                <th>Coverage</th>
                <th>Scope</th>
              </tr>
            </thead>
            <tbody>
              {filteredRows.map((row) => (
                <tr key={catalogRowKey(row)}>
                  <td>
                    <Link
                      aria-label={`Open ${row.name} workspace`}
                      to={catalogWorkspacePath(row)}
                    >
                      {row.name}
                    </Link>
                  </td>
                  <td>{catalogKindLabel(row)}</td>
                  <td>{materializationLabel(row.freshness)}</td>
                  <td>{row.coverage}</td>
                  <td>
                    <div className="catalog-row-scope">
                      <span>{environmentSummary(row.environments)}</span>
                      <button
                        aria-label={`Inspect ${row.name}`}
                        onClick={() => setSelectedRowKey(catalogRowKey(row))}
                        type="button"
                      >
                        Inspect
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {selectedRow !== undefined ? <CatalogDossier row={selectedRow} /> : null}
      </div>
    </section>
  );
}

function CatalogDossier({ row }: { readonly row: CatalogRow }): React.JSX.Element {
  const environments = environmentSummary(row.environments);
  return (
    <aside aria-label="Catalog drilldown" className="catalog-dossier">
      <h2>Selected catalog entry</h2>
      <dl>
        <div>
          <dt>Name</dt>
          <dd>{row.name}</dd>
        </div>
        <div>
          <dt>Kind</dt>
          <dd>{catalogKindLabel(row)}</dd>
        </div>
        {row.ownerRepo !== undefined && row.ownerRepo.length > 0 ? (
          <div>
            <dt>Owner repository</dt>
            <dd>{row.ownerRepo}</dd>
          </div>
        ) : null}
        <div>
          <dt>Environments</dt>
          <dd>{environments}</dd>
        </div>
        <div>
          <dt>Materialization</dt>
          <dd>{materializationLabel(row.materializationStatus ?? row.freshness)}</dd>
        </div>
        {row.instanceCount !== undefined ? (
          <div>
            <dt>Instances</dt>
            <dd>{row.instanceCount}</dd>
          </div>
        ) : null}
      </dl>
      <Link to={catalogWorkspacePath(row)}>
        Open workspace
      </Link>
    </aside>
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

interface CatalogCoverage {
  readonly entries: number;
  readonly environmentCount: number;
  readonly environments: readonly string[];
  readonly identityOnlyCount: number;
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

function catalogCoverage(rows: readonly CatalogRow[]): CatalogCoverage {
  const environments = Array.from(
    new Set(rows.flatMap((row) => row.environments ?? []))
  ).sort((left, right) => left.localeCompare(right));
  return {
    entries: rows.length,
    environmentCount: environments.length,
    environments,
    identityOnlyCount: rows.filter((row) =>
      normalizedToken(row.materializationStatus ?? row.freshness).includes("identity-only")
    ).length
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

function catalogKindLabel(row: CatalogRow): string {
  if (row.workloadKind !== undefined && row.workloadKind.length > 0) {
    return row.kind === "services" ? `service ${row.workloadKind}` : row.workloadKind;
  }
  return row.kind;
}

function catalogRowKey(row: CatalogRow): string {
  return `${row.kind}:${row.id}`;
}

function catalogWorkspacePath(row: CatalogRow): string {
  return `/workspace/${row.kind}/${encodeURIComponent(row.id)}`;
}

function environmentSummary(environments: readonly string[] | undefined): string {
  if (environments === undefined || environments.length === 0) {
    return "No environment";
  }
  return environments.join(", ");
}

function materializationLabel(value: string): string {
  return normalizedToken(value).replaceAll("-", " ");
}

function normalizedToken(value: string): string {
  return value.trim().toLowerCase().replaceAll("_", "-");
}
