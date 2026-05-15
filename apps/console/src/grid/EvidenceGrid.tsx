import { useState } from "react";
import type { EvidenceDrilldown, EvidenceRow } from "../api/mockData";

interface EvidenceGridProps {
  readonly rows: readonly EvidenceRow[];
}

export function EvidenceGrid({ rows }: EvidenceGridProps): React.JSX.Element {
  const [inspectedRow, setInspectedRow] = useState<string | undefined>(
    rowKey(rows[0])
  );

  if (rows.length === 0) {
    return (
      <p className="inline-state">
        Eshu has not published evidence rows for this entity yet.
      </p>
    );
  }

  return (
    <div className="evidence-story-list">
      {rows.map((row) => {
        const key = rowKey(row);
        const isInspected = inspectedRow === key;
        return (
          <article
            aria-label={`${row.title ?? row.source} evidence`}
            className={isInspected ? "evidence-row evidence-row-active" : "evidence-row"}
            key={key}
          >
            <div>
              <p className="evidence-category">{row.category ?? row.source}</p>
              <h3>{row.title ?? row.source}</h3>
              <p>{row.summary}</p>
            </div>
            <dl>
              <div>
                <dt>Source</dt>
                <dd>{row.source}</dd>
              </div>
              <div>
                <dt>Basis</dt>
                <dd>{row.basis}</dd>
              </div>
              {row.detailPath !== undefined ? (
                <div>
                  <dt>Path</dt>
                  <dd>{row.detailPath}</dd>
                </div>
              ) : null}
            </dl>
            <button
              aria-expanded={isInspected}
              onClick={() => setInspectedRow(isInspected ? undefined : key)}
              type="button"
            >
              Inspect evidence
            </button>
            {isInspected ? (
              <EvidenceDrilldownPanel row={row} />
            ) : null}
          </article>
        );
      })}
    </div>
  );
}

function EvidenceDrilldownPanel({ row }: { readonly row: EvidenceRow }): React.JSX.Element {
  if (row.drilldown === undefined) {
    return (
      <p className="evidence-row-detail">
        Source {row.source} supports this claim through {row.basis}
        {row.detailPath !== undefined ? ` at ${row.detailPath}` : ""}.
      </p>
    );
  }

  return (
    <div className="evidence-row-detail evidence-drilldown">
      {row.drilldown.summary !== undefined ? <p>{row.drilldown.summary}</p> : null}
      <EvidenceMetrics drilldown={row.drilldown} />
      <EvidenceTableView drilldown={row.drilldown} />
    </div>
  );
}

function EvidenceMetrics({
  drilldown
}: {
  readonly drilldown: EvidenceDrilldown;
}): React.JSX.Element | null {
  if (drilldown.metrics === undefined || drilldown.metrics.length === 0) {
    return null;
  }
  return (
    <dl className="evidence-drilldown-metrics">
      {drilldown.metrics.map((metric) => (
        <div key={`${metric.label}:${metric.value}`}>
          <dt>{metric.label}</dt>
          <dd title={metric.detail}>{metric.value}</dd>
        </div>
      ))}
    </dl>
  );
}

function EvidenceTableView({
  drilldown
}: {
  readonly drilldown: EvidenceDrilldown;
}): React.JSX.Element | null {
  if (drilldown.table === undefined || drilldown.table.rows.length === 0) {
    return null;
  }
  return (
    <div className="evidence-drilldown-table-wrap">
      <table aria-label={drilldown.table.ariaLabel} className="evidence-drilldown-table">
        <thead>
          <tr>
            {drilldown.table.columns.map((column) => (
              <th key={column.key} scope="col">
                {column.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {drilldown.table.rows.map((tableRow) => (
            <tr key={tableRow.id}>
              {drilldown.table?.columns.map((column) => (
                <td key={`${tableRow.id}:${column.key}`}>
                  {tableRow.cells[column.key] ?? ""}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function rowKey(row: EvidenceRow | undefined): string | undefined {
  if (row === undefined) {
    return undefined;
  }
  return `${row.source}:${row.basis}:${row.title ?? row.summary}`;
}
