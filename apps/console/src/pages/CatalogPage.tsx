import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { EshuApiClient } from "../api/client";
import { loadCatalogRows } from "../api/liveData";
import type { CatalogRow } from "../api/mockData";
import { loadConsoleEnvironment } from "../config/environment";

export function CatalogPage(): React.JSX.Element {
  const [rows, setRows] = useState<readonly CatalogRow[]>([]);
  const [query, setQuery] = useState("");
  const [loadState, setLoadState] = useState<"loading" | "ready" | "unavailable">(
    "loading"
  );

  useEffect(() => {
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
        setRows(loadedRows);
        setLoadState("ready");
      })
      .catch(() => {
        setRows([]);
        setLoadState("unavailable");
      });
  }, []);

  const filteredRows = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (normalized.length === 0) {
      return rows;
    }
    return rows.filter((row) =>
      `${row.name} ${row.kind} ${row.coverage}`.toLowerCase().includes(normalized)
    );
  }, [query, rows]);

  return (
    <section className="page-shell">
      <div className="page-intro">
        <h1>Catalog</h1>
        <p>Browse indexed repositories and open the workspace behind each one.</p>
      </div>
      {loadState === "loading" ? <p className="inline-state">Loading live data.</p> : null}
      {loadState === "unavailable" ? (
        <p className="inline-state">Local Eshu API unavailable.</p>
      ) : null}
      <div className="table-toolbar">
        <label>
          <span>Search catalog</span>
          <input
            aria-label="Search catalog"
            onChange={(event) => setQuery(event.currentTarget.value)}
            placeholder="Filter by repo, kind, or path"
            value={query}
          />
        </label>
        <strong>{filteredRows.length} shown</strong>
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
