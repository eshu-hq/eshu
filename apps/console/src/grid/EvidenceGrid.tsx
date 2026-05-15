import { useState } from "react";
import type { EvidenceRow } from "../api/mockData";

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
              <p className="evidence-row-detail">
                Source {row.source} supports this claim through {row.basis}
                {row.detailPath !== undefined ? ` at ${row.detailPath}` : ""}.
              </p>
            ) : null}
          </article>
        );
      })}
    </div>
  );
}

function rowKey(row: EvidenceRow | undefined): string | undefined {
  if (row === undefined) {
    return undefined;
  }
  return `${row.source}:${row.basis}:${row.title ?? row.summary}`;
}
