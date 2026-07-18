import type { VisualizationEdge, VisualizationNode } from "../api/answerVisualization";
import type { EvidenceSelection } from "./EvidenceDrawer";

const RELATIONSHIP_VERBS: Readonly<Record<string, string>> = {
  CONSUMED_BY: "is consumed by",
  DEPENDS_ON: "depends on",
  DEPLOYS_FROM: "deploys from",
  PROVISIONING_SOURCE_CHAIN: "provisions",
  READS_CONFIG_FROM: "reads config from",
  RUNS_AS: "runs as",
};

const NODE_ROLE_LABELS: Readonly<Record<string, string>> = {
  deployment_configuration: "deployment configuration repository",
  downstream_consumer: "downstream repository",
  runtime_instance: "runtime instance",
  source_repository: "source repository",
  workload: "workload service",
};

export function ServiceStoryRelationshipList({
  edges,
  nodes,
  onSelect,
  selected,
}: {
  readonly edges: readonly VisualizationEdge[];
  readonly nodes: readonly VisualizationNode[];
  readonly onSelect: (selection: EvidenceSelection | null) => void;
  readonly selected: EvidenceSelection | null;
}): React.JSX.Element | null {
  if (edges.length === 0) {
    return null;
  }
  return (
    <section className="seg-edges" aria-label="Relationships">
      <h3>Relationships</h3>
      <ul>
        {edges.map((edge) => {
          const active = selected?.kind === "edge" && selected.id === edge.id;
          return (
            <li key={edge.id}>
              <button
                className={`seg-edge${active ? " is-active" : ""}`}
                onClick={() => onSelect({ kind: "edge", id: edge.id })}
                type="button"
              >
                <span className="seg-edge-narrative">{relationshipNarrative(nodes, edge)}</span>
                <span className="seg-edge-verb">{edge.relationship || "RELATED"}</span>
                {edge.truthLabel.length > 0 ? (
                  <span className="seg-edge-truth">{edge.truthLabel}</span>
                ) : null}
                <span className="mono seg-edge-diagnostic" aria-label="Relationship diagnostic IDs">
                  {edge.source} → {edge.target}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function relationshipNarrative(
  nodes: readonly VisualizationNode[],
  edge: VisualizationEdge,
): string {
  const source = nodes.find((node) => node.id === edge.source);
  const target = nodes.find((node) => node.id === edge.target);
  const verb =
    (RELATIONSHIP_VERBS[edge.relationship] ??
      edge.relationship.toLowerCase().replaceAll("_", " ")) ||
    "relates to";
  return `${relationshipEndpoint(source, edge.source, relationshipRole(edge, "source"))} ${verb} ${relationshipEndpoint(target, edge.target, relationshipRole(edge, "target"))}`;
}

function relationshipRole(
  edge: VisualizationEdge,
  endpoint: "source" | "target",
): string | undefined {
  if (edge.relationship === "CONSUMED_BY") {
    return endpoint === "source" ? "workload service" : "downstream repository";
  }
  if (edge.relationship === "RUNS_AS") {
    return endpoint === "source" ? "workload service" : "runtime instance";
  }
  return undefined;
}

function relationshipEndpoint(
  node: VisualizationNode | undefined,
  fallback: string,
  relationshipRoleLabel?: string,
): string {
  if (node === undefined) {
    return fallback;
  }
  const role =
    (relationshipRoleLabel ??
      NODE_ROLE_LABELS[node.role] ??
      [node.category, node.type].filter((value) => value.length > 0).join(" ")) ||
    "entity";
  return `${node.label || node.id} (${role})`;
}
