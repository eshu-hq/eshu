import { useEffect, useMemo, useState } from "react";
import { EshuApiClient } from "../api/client";
import { loadFindingRows } from "../api/liveData";
import type { FindingRow } from "../api/mockData";
import { loadConsoleEnvironment } from "../config/environment";

export function FindingsPage(): React.JSX.Element {
  const [rows, setRows] = useState<readonly FindingRow[]>([]);
  const [query, setQuery] = useState("");
  const [loadState, setLoadState] = useState<"loading" | "ready" | "unavailable">(
    "loading"
  );

  useEffect(() => {
    const environment = loadConsoleEnvironment();
    const client =
      environment.mode === "private"
        ? new EshuApiClient({ baseUrl: environment.apiBaseUrl })
        : undefined;
    void loadFindingRows({ client, mode: environment.mode })
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
      `${row.findingType} ${row.name} ${row.entity} ${row.location}`
        .toLowerCase()
        .includes(normalized)
    );
  }, [query, rows]);

  return (
    <section className="page-shell">
      <h1>Findings</h1>
      <p>Search cleanup candidates by repo, symbol, file, or finding type.</p>
      {loadState === "loading" ? <p className="inline-state">Loading live data.</p> : null}
      {loadState === "unavailable" ? (
        <p className="inline-state">Local Eshu API unavailable.</p>
      ) : null}
      <div className="table-toolbar">
        <label>
          <span>Search findings</span>
          <input
            aria-label="Search findings"
            onChange={(event) => setQuery(event.currentTarget.value)}
            placeholder="Filter by repo, symbol, or path"
            value={query}
          />
        </label>
        <strong>{rows.length} findings</strong>
      </div>
      <table className="data-table">
        <thead>
          <tr>
            <th>Type</th>
            <th>Name</th>
            <th>Entity</th>
            <th>Location</th>
            <th>Truth</th>
          </tr>
        </thead>
        <tbody>
          {filteredRows.map((row) => (
            <tr key={`${row.findingType}:${row.name}`}>
              <td>{row.findingType}</td>
              <td>{row.name}</td>
              <td>{row.entity}</td>
              <td>{row.location}</td>
              <td>{row.truthLevel}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
