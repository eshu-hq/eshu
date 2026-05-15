import type { DeploymentGraph } from "./mockData";
import type {
  ServiceConsumer,
  ServiceDependency,
  ServiceDeploymentLane
} from "./serviceSpotlight";

export function deploymentGraph(
  name: string,
  lanes: readonly ServiceDeploymentLane[],
  dependencies: readonly ServiceDependency[],
  consumers: readonly ServiceConsumer[]
): DeploymentGraph {
  const laneEntries = lanes.length > 0 ? lanes : [fallbackLane()];
  const nodes = lanes.flatMap((lane, index) => {
    const laneKey = `${lane.label}:${index}`;
    const sourceLabel = lane.sourceRepos[0] ?? lane.label;
    const envLabel = lane.environments.slice(0, 3).join(", ");
    return [
      {
        column: 0,
        id: `service:${laneKey}`,
        kind: "service" as const,
        label: name,
        lane: laneKey
      },
      {
        column: 1,
        detail: `${lane.evidenceCount} evidence item(s)`,
        id: `source:${laneKey}`,
        kind: "repository" as const,
        label: sourceLabel,
        lane: laneKey
      },
      {
        column: 2,
        id: `runtime:${laneKey}`,
        kind: "environment" as const,
        label: lane.label,
        lane: laneKey
      },
      {
        column: 3,
        id: `env:${laneKey}`,
        kind: "artifact" as const,
        label: envLabel || "environment",
        lane: laneKey
      }
    ];
  });
  const fallbackNodes = lanes.length > 0
    ? []
    : [{
      column: 0,
      detail: "Service selected from API context.",
      id: "service:Runtime:0",
      kind: "service" as const,
      label: name,
      lane: "Runtime:0"
    }];
  const links = laneEntries.flatMap((lane, index) => {
    const laneKey = `${lane.label}:${index}`;
    if (lanes.length === 0) {
      return [];
    }
    return [
      { label: "uses", source: `service:${laneKey}`, target: `source:${laneKey}` },
      { label: "runs on", source: `source:${laneKey}`, target: `runtime:${laneKey}` },
      { label: "materializes", source: `runtime:${laneKey}`, target: `env:${laneKey}` }
    ];
  });
  const serviceAnchor = `service:${laneEntries[0]?.label ?? "Runtime"}:0`;
  const dependencyNodes = dependencies.slice(0, 2).map((dependency, index) => ({
    column: 1,
    detail: dependency.rationale,
    id: `dependency:${index}`,
    kind: "relationship" as const,
    label: dependency.targetName,
    lane: `dependency:${index}`
  }));
  const consumerNodes = consumers.slice(0, 2).map((consumer, index) => ({
    column: 3,
    detail: consumer.samplePaths[0],
    id: `consumer:${index}`,
    kind: "repository" as const,
    label: consumer.repository,
    lane: `consumer:${index}`
  }));
  const relationshipLinks = [
    ...dependencyNodes.map((node) => ({
      label: "depends on",
      source: serviceAnchor,
      target: node.id
    })),
    ...consumerNodes.map((node) => ({
      label: "consumed by",
      source: serviceAnchor,
      target: node.id
    }))
  ];
  return {
    links: [...links, ...relationshipLinks],
    nodes: [...nodes, ...fallbackNodes, ...dependencyNodes, ...consumerNodes]
  };
}

function fallbackLane(): ServiceDeploymentLane {
  return {
    environments: [],
    evidenceCount: 0,
    label: "Runtime",
    relationshipTypes: [],
    resolvedCount: 0,
    sourceRepos: []
  };
}
