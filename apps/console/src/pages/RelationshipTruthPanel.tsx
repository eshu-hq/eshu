import type { CodeRelationshipStoryCoverage } from "../api/eshuGraph";
import type { GraphModel } from "../console/types";

export function RelationshipTruthPanel({ graph, coverage }: {
  readonly graph: GraphModel;
  readonly coverage?: CodeRelationshipStoryCoverage;
}): React.JSX.Element {
  const rows = graph.edges.filter((edge) => edge.confidenceTier || edge.truthState);
  return (
    <>
      <div className="section-label" style={{ marginTop: 16 }}>Relationship truth</div>
      {rows.length ? (
        <div className="conn-list">
          {rows.map((edge) => (
            <div className="dead-row" key={`${edge.s}:${edge.verb}:${edge.t}`}>
              <span className="mono">{edge.verb}</span>
              <span className="t-mut">{edge.confidenceTier ?? "unsupported"} · {edge.truthState ?? "unsupported"}</span>
              <span className="t-mut">source {edge.sourceFamily ?? "unsupported"} · method {edge.method ?? "unsupported"}</span>
            </div>
          ))}
        </div>
      ) : (
        <p className="empty" style={{ padding: "6px 0", textAlign: "left" }}>No relationship truth labels returned.</p>
      )}
      {coverage ? (
        <div className="kv-list" style={{ marginTop: 8 }}>
          <div className="kv"><span>Missing-edge reason</span><strong>{coverage.missing_edge_reason ?? "complete"}</strong></div>
          <div className="kv"><span>Truncation</span><strong>{coverage.truncation_state ?? "none"}</strong></div>
          {coverage.evidence_explanation ? (
            <p className="t-mut" style={{ fontSize: ".78rem", margin: "6px 0 0" }}>{coverage.evidence_explanation}</p>
          ) : null}
        </div>
      ) : null}
    </>
  );
}
