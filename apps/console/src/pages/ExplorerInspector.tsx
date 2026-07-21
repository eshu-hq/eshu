import { Link } from "react-router-dom";

import { currentCenterId, sourceHref, sourceLabel } from "./ExplorerGraphHelpers";
import { Panel, TruthChip } from "../components/atoms";
import { EvidencePanel } from "../components/EvidencePanel";
import type { EvidencePanelData } from "../components/EvidencePanel";
import { graphEdgeEvidencePanelData } from "../components/graphEvidencePanel";
import { KIND_COLOR, LAYER_COLOR, fmt } from "../console/types";
import type { ConsoleModel, GraphModel, GraphNode } from "../console/types";

export function ExplorerInspector({
  base,
  busy,
  evidence,
  live,
  onCenter,
  onEvidenceChange,
  relationships,
  selected,
}: {
  readonly base: GraphModel;
  readonly busy: boolean;
  readonly evidence: EvidencePanelData | null;
  readonly live: boolean;
  readonly onCenter: (node: GraphNode) => void;
  readonly onEvidenceChange: (evidence: EvidencePanelData | null) => void;
  readonly relationships: ConsoleModel["relationships"];
  readonly selected: GraphNode | undefined;
}): React.JSX.Element {
  const nodeLabels = new Map(base.nodes.map((node) => [node.id, node.label]));
  const centerID = currentCenterId(base);

  return (
    <Panel title="Inspector">
      {selected ? (
        <div className="inspector">
          <div className="insp-head">
            <span
              className="cglyph"
              style={{
                borderColor: KIND_COLOR[selected.kind] ?? "#9aa4af",
                color: KIND_COLOR[selected.kind] ?? "#9aa4af",
                height: 30,
                width: 30,
              }}
            >
              {selected.kind.slice(0, 1).toUpperCase()}
            </span>
            <div>
              <div className="insp-kind">{selected.kind}</div>
              <div className="insp-title">{selected.label}</div>
            </div>
          </div>
          {selected.sub ? (
            <div className="t-mut mono" style={{ fontSize: ".82rem" }}>
              {selected.sub}
            </div>
          ) : null}
          {selected.truth ? <TruthChip level={selected.truth} /> : null}
          {sourceHref(selected) ? (
            <div className="kv-list">
              <div className="kv">
                <span>Source</span>
                <Link className="mono" to={sourceHref(selected) ?? ""}>
                  {sourceLabel(selected)}
                </Link>
              </div>
            </div>
          ) : null}
          {live ? (
            <button
              className="btn-ghost"
              disabled={busy || selected.id === centerID}
              style={{ justifyContent: "center", width: "100%" }}
              onClick={() => onCenter(selected)}
            >
              {selected.id === centerID
                ? "Current center"
                : busy
                  ? "Loading…"
                  : "Center graph here →"}
            </button>
          ) : null}
          {sourceHref(selected) ? (
            <Link className="btn-ghost active" to={sourceHref(selected) ?? ""}>
              Open source
            </Link>
          ) : null}
          <div className="section-label">Edges</div>
          <div className="insp-evi">
            {base.edges
              .filter((edge) => edge.s === selected.id || edge.t === selected.id)
              .map((edge, index) => {
                const endpointID = edge.s === selected.id ? edge.t : edge.s;
                const endpointLabel = nodeLabels.get(endpointID) ?? endpointID;
                return (
                  <button
                    className="insp-evi-row insp-evi-btn"
                    key={index}
                    onClick={() =>
                      onEvidenceChange(
                        graphEdgeEvidencePanelData(
                          edge,
                          nodeLabels.get(edge.s) ?? edge.s,
                          nodeLabels.get(edge.t) ?? edge.t,
                        ),
                      )
                    }
                    title={`Inspect ${edge.verb} evidence`}
                    type="button"
                  >
                    {edge.verb} {edge.s === selected.id ? "→" : "←"} {endpointLabel}
                  </button>
                );
              })}
          </div>
        </div>
      ) : (
        <p className="empty">{live ? "Search for an entity to begin." : "Select a node."}</p>
      )}
      {relationships.length ? (
        <>
          <div className="section-label" style={{ marginTop: 16 }}>
            Relationship verbs
          </div>
          <div className="kv-list">
            {relationships.slice(0, 6).map((relationship) => (
              <div className="kv" key={relationship.verb}>
                <span className="mono" style={{ fontSize: ".78rem" }}>
                  <i
                    style={{
                      background: LAYER_COLOR[relationship.layer],
                      borderRadius: 2,
                      display: "inline-block",
                      height: 8,
                      marginRight: 7,
                      width: 8,
                    }}
                  />
                  {relationship.verb}
                </span>
                <strong>{fmt(relationship.count)}</strong>
              </div>
            ))}
          </div>
        </>
      ) : null}
      {evidence !== null ? (
        <div className="insp-evidence-panel">
          <EvidencePanel data={evidence} onClose={() => onEvidenceChange(null)} />
        </div>
      ) : null}
    </Panel>
  );
}
