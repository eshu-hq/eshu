import type { GraphEdge, GraphNode } from "../console/types";

export function ImpactSelectedEdges({
  edges,
  nodes,
  selectedID,
}: {
  readonly edges: readonly GraphEdge[];
  readonly nodes: readonly GraphNode[];
  readonly selectedID: string;
}): React.JSX.Element {
  const nodesByID = new Map(nodes.map((node) => [node.id, node]));
  if (edges.length === 0) return <p className="empty">No edges selected yet.</p>;
  return (
    <ul className="impact-edge-list">
      {edges.map((edge, index) => {
        const sourceLabel = nodeLabel(nodesByID, edge.s);
        const targetLabel = nodeLabel(nodesByID, edge.t);
        return (
          <li
            aria-label={`${sourceLabel} ${humanRelationshipVerb(edge.verb)} ${targetLabel}`}
            className="insp-evi-row"
            key={`${edge.s}:${edge.t}:${edge.verb}:${index}`}
          >
            <span>{sourceLabel}</span>
            <span>{humanRelationshipVerb(edge.verb)}</span>
            <span>{targetLabel}</span>
            <span className="t-mut">{edge.s === selectedID ? "outgoing" : "incoming"}</span>
            <span className="mono t-mut">
              {edge.s} → {edge.t}
            </span>
          </li>
        );
      })}
    </ul>
  );
}

function nodeLabel(nodesByID: ReadonlyMap<string, GraphNode>, id: string): string {
  return nodesByID.get(id)?.label ?? id;
}

function humanRelationshipVerb(verb: string): string {
  return verb.toLowerCase().replace(/_/g, " ");
}
