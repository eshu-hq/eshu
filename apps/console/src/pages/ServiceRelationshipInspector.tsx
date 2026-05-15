import { useState } from "react";
import type { ServiceSpotlight } from "../api/serviceSpotlight";
import {
  technologyLabel,
  type RelationshipEdge,
  type RelationshipNode
} from "./serviceRelationshipGraphModel";

export interface SelectedGraphItem {
  readonly edge?: RelationshipEdge;
  readonly node?: RelationshipNode;
}

interface InspectorDossier {
  readonly facts: readonly InspectorFact[];
  readonly paths: readonly string[];
  readonly summary: string;
  readonly title: string;
  readonly type: string;
}

interface InspectorFact {
  readonly label: string;
  readonly value: string;
}

type InspectorTab = "summary" | "facts" | "paths";

export function RelationshipSelectionSummary({
  edge
}: {
  readonly edge: RelationshipEdge;
}): React.JSX.Element {
  return (
    <div
      aria-label="Selected relationship"
      className="relationship-map-selection-summary"
      role="status"
    >
      <strong>{edge.label}</strong>
      <span>{`${readableEndpoint(edge.source)} -> ${readableEndpoint(edge.target)}`}</span>
    </div>
  );
}

export function RelationshipInspector({
  selected,
  spotlight
}: {
  readonly selected: SelectedGraphItem;
  readonly spotlight: ServiceSpotlight;
}): React.JSX.Element {
  if (selected.edge !== undefined) {
    return <EdgeInspector edge={selected.edge} spotlight={spotlight} />;
  }
  const dossier = nodeDossier(selected.node, spotlight);
  return (
    <InspectorShell
      facts={dossier.facts}
      key={`node:${selected.node?.id ?? "empty"}`}
      paths={dossier.paths}
      summary={dossier.summary}
      title={dossier.title}
      type={dossier.type}
    />
  );
}

function EdgeInspector({
  edge,
  spotlight
}: {
  readonly edge: RelationshipEdge;
  readonly spotlight: ServiceSpotlight;
}): React.JSX.Element {
  const evidence = edgeEvidence(edge, spotlight);
  const facts = compactFacts([
    { label: "Verb", value: edge.label },
    { label: "Source", value: readableEndpoint(edge.source) },
    { label: "Target", value: readableEndpoint(edge.target) },
    { label: "Evidence kind", value: joinList(evidence.evidenceKinds) },
    { label: "Environments", value: joinList(evidence.environments) }
  ]);
  return (
    <InspectorShell
      facts={facts}
      key={`edge:${edge.source}:${edge.target}:${edge.label}`}
      paths={evidence.paths}
      summary={edgeSummary(edge)}
      title="Selected relationship"
      type={edge.label}
    />
  );
}

function InspectorShell({
  facts,
  paths,
  summary,
  title,
  type
}: {
  readonly facts: readonly InspectorFact[];
  readonly paths: readonly string[];
  readonly summary: string;
  readonly title: string;
  readonly type: string;
}): React.JSX.Element {
  const [activeTab, setActiveTab] = useState<InspectorTab>("summary");
  return (
    <aside className="relationship-inspector" aria-label="Relationship inspector">
      <h4>{title}</h4>
      <span>{type}</span>
      <div className="relationship-inspector-tabs" role="tablist" aria-label="Relationship inspector views">
        {inspectorTabs.map((tab) => (
          <button
            aria-controls={`relationship-inspector-${tab.id}`}
            aria-selected={activeTab === tab.id}
            id={`relationship-inspector-tab-${tab.id}`}
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            role="tab"
            type="button"
          >
            {tab.label}
          </button>
        ))}
      </div>
      <div
        aria-labelledby={`relationship-inspector-tab-${activeTab}`}
        className="relationship-inspector-tab-panel"
        id={`relationship-inspector-${activeTab}`}
        role="tabpanel"
      >
        {activeTab === "summary" ? <p>{summary}</p> : null}
        {activeTab === "facts" ? <InspectorFacts facts={facts} /> : null}
        {activeTab === "paths" ? <InspectorPaths paths={paths} /> : null}
      </div>
    </aside>
  );
}

function InspectorFacts({
  facts
}: {
  readonly facts: readonly InspectorFact[];
}): React.JSX.Element {
  if (facts.length === 0) {
    return <p className="relationship-inspector-empty">No structured facts are published yet.</p>;
  }
  return (
    <dl className="relationship-inspector-facts">
      {facts.map((fact) => (
        <div key={fact.label}>
          <dt>{fact.label}</dt>
          <dd>{fact.value}</dd>
        </div>
      ))}
    </dl>
  );
}

function InspectorPaths({
  paths
}: {
  readonly paths: readonly string[];
}): React.JSX.Element {
  if (paths.length === 0) {
    return <p className="relationship-inspector-empty">No source paths are published for this selection.</p>;
  }
  return (
    <div className="relationship-inspector-paths">
      <strong>Evidence paths</strong>
      <ul>
        {paths.map((path) => (
          <li key={path}>{path}</li>
        ))}
      </ul>
    </div>
  );
}

const inspectorTabs: readonly {
  readonly id: InspectorTab;
  readonly label: string;
}[] = [
  { id: "summary", label: "Summary" },
  { id: "facts", label: "Facts" },
  { id: "paths", label: "Evidence paths" }
];

function edgeSummary(edge: RelationshipEdge): string {
  const source = readableEndpoint(edge.source);
  const target = readableEndpoint(edge.target);
  if (edge.label === "DEPLOYS_FROM") {
    return `${source} is deployment evidence for ${target}. Use the evidence paths to inspect the exact source file.`;
  }
  if (edge.label === "PROVISIONS_DEPENDENCY_FOR") {
    return `${source} provisions infrastructure or runtime dependencies for ${target}.`;
  }
  if (edge.label === "READS_CONFIG_FROM") {
    return `${source} reads or grants access to configuration associated with ${target}.`;
  }
  if (edge.label === "RUNS_ON") {
    return `${source} is connected to the ${target} runtime lane.`;
  }
  return edge.detail;
}

function edgeEvidence(
  edge: RelationshipEdge,
  spotlight: ServiceSpotlight
): {
  readonly environments: readonly string[];
  readonly evidenceKinds: readonly string[];
  readonly paths: readonly string[];
} {
  const source = readableEndpoint(edge.source);
  const target = readableEndpoint(edge.target);
  if (edge.label === "RUNS_ON") {
    const lane = spotlight.lanes.find((candidate) => candidate.label === target);
    return {
      environments: lane?.environments ?? [],
      evidenceKinds: [],
      paths: []
    };
  }
  const repository = edge.source.startsWith("repo:") ? source : target;
  const evidence = repositoryEvidence(repository, spotlight).filter((item) =>
    item.relationshipTypes.includes(edge.label)
  );
  const lanes = spotlight.lanes.filter((lane) =>
    lane.sourceRepos.includes(repository) && lane.relationshipTypes.includes(edge.label)
  );
  return {
    environments: unique(lanes.flatMap((lane) => lane.environments)),
    evidenceKinds: unique(evidence.flatMap((item) => item.evidenceKinds)),
    paths: unique(evidence.flatMap((item) => item.paths)).slice(0, 6)
  };
}

function nodeDossier(
  node: RelationshipNode | undefined,
  spotlight: ServiceSpotlight
): InspectorDossier {
  if (node === undefined) {
    return {
      facts: [],
      paths: [],
      summary: "Select a node or relationship to inspect the supporting evidence.",
      title: "Select a node",
      type: "Relationship evidence"
    };
  }
  if (node.kind === "service") {
    return serviceDossier(node, spotlight);
  }
  if (node.kind === "runtime") {
    return runtimeDossier(node, spotlight);
  }
  return repositoryDossier(node, spotlight);
}

function serviceDossier(node: RelationshipNode, spotlight: ServiceSpotlight): InspectorDossier {
  return {
    facts: [
      { label: "API", value: `${spotlight.api.endpointCount} endpoints, ${spotlight.api.methodCount} methods` },
      { label: "Deployment lanes", value: joinList(spotlight.lanes.map((lane) => lane.label)) },
      { label: "Downstream", value: `${spotlight.relationshipCounts.downstream} observed dependents` }
    ],
    paths: spotlight.api.sourcePaths.slice(0, 4),
    summary: `${spotlight.name} is the selected service. Eshu has API, deployment, dependency, and consumer evidence for this workload.`,
    title: node.label,
    type: "Service workload"
  };
}

function runtimeDossier(node: RelationshipNode, spotlight: ServiceSpotlight): InspectorDossier {
  const lane = spotlight.lanes.find((candidate) => candidate.label === node.label);
  return {
    facts: [
      { label: "Relationship verbs", value: joinList(lane?.relationshipTypes ?? []) },
      { label: "Source repos", value: joinList(lane?.sourceRepos ?? []) },
      { label: "Environments", value: joinList(lane?.environments ?? []) }
    ],
    paths: [],
    summary: `${spotlight.name} has ${lane?.evidenceCount ?? 0} evidence item(s) for ${node.label}.`,
    title: node.label,
    type: "Runtime target"
  };
}

function repositoryDossier(node: RelationshipNode, spotlight: ServiceSpotlight): InspectorDossier {
  const repository = readableEndpoint(node.id);
  const evidence = repositoryEvidence(repository, spotlight);
  const verbs = unique(evidence.flatMap((item) => item.relationshipTypes));
  const evidenceKinds = unique(evidence.flatMap((item) => item.evidenceKinds));
  const paths = unique(evidence.flatMap((item) => item.paths)).slice(0, 6);
  const lanes = spotlight.lanes.filter((lane) => lane.sourceRepos.includes(repository));
  const environments = unique(lanes.flatMap((lane) => lane.environments));
  return {
    facts: compactFacts([
      { label: "Relationship verbs", value: joinList(verbs) },
      { label: "Evidence kinds", value: joinList(evidenceKinds) },
      { label: "Runtime lanes", value: joinList(lanes.map((lane) => lane.label)) },
      { label: "Environments", value: joinList(environments) }
    ]),
    paths,
    summary: repositorySummary(repository, spotlight.name, node, verbs, environments),
    title: node.label,
    type: technologyLabel(node.technology)
  };
}

function repositoryEvidence(repository: string, spotlight: ServiceSpotlight) {
  return spotlight.relationshipClusters.flatMap((cluster) =>
    cluster.repositories
      .filter((candidate) => candidate.repository === repository)
      .map((candidate) => ({
        evidenceKinds: candidate.evidenceKinds,
        paths: candidate.paths,
        relationshipTypes: candidate.relationshipTypes,
        technology: candidate.technology
      }))
  );
}

function repositorySummary(
  repository: string,
  serviceName: string,
  node: RelationshipNode,
  verbs: readonly string[],
  environments: readonly string[]
): string {
  const envText = environments.length > 0 ? ` in ${joinList(environments)}` : "";
  if (verbs.includes("PROVISIONS_DEPENDENCY_FOR")) {
    return `Terraform evidence shows ${repository} provisions runtime dependencies for ${serviceName}${envText}.`;
  }
  if (verbs.includes("READS_CONFIG_FROM")) {
    return `${repository} reads or grants access to configuration used by ${serviceName}${envText}.`;
  }
  if (verbs.includes("DEPLOYS_FROM")) {
    return `${repository} contributes deployment source evidence for ${serviceName}${envText}.`;
  }
  return `${repository} is connected to ${serviceName} as ${technologyLabel(node.technology)} evidence.`;
}

function compactFacts(facts: readonly InspectorFact[]): readonly InspectorFact[] {
  return facts.filter((fact) => fact.value.trim().length > 0 && fact.value !== "Not observed");
}

function joinList(values: readonly string[]): string {
  const uniqueValues = unique(values);
  if (uniqueValues.length === 0) {
    return "Not observed";
  }
  return uniqueValues.join(", ");
}

function unique(values: readonly string[]): readonly string[] {
  return [...new Set(values.filter((value) => value.trim().length > 0))];
}

export function readableEndpoint(id: string): string {
  return id.replace(/^(repo|service|runtime):/, "");
}
