import type { EvidenceRow } from "../api/mockData";

interface EvidenceGridProps {
  readonly rows: readonly EvidenceRow[];
}

export function EvidenceGrid({ rows }: EvidenceGridProps): React.JSX.Element {
  return (
    <div className="evidence-story-list">
      {rows.map((row) => (
        <article className="evidence-story" key={`${row.source}:${row.basis}:${row.title}`}>
          <div>
            <span>{row.category ?? row.source}</span>
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
          <button type="button">Drill down</button>
        </article>
      ))}
    </div>
  );
}
