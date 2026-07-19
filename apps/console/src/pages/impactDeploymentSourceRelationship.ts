import type { DeploymentTraceResult, DeploymentTraceSource } from "../api/impactReviewTypes";

interface DeploymentSourceRelationshipView {
  readonly family: string;
  readonly source: string;
  readonly target: string;
  readonly verb: string;
  readonly canonicalSource?: string;
  readonly canonicalTarget?: string;
}

export function deploymentSourceRelationship(
  source: DeploymentTraceSource,
  trace: DeploymentTraceResult,
): DeploymentSourceRelationshipView {
  if (source.relationshipType === "DEPLOYS_FROM") {
    return {
      family: "DEPLOYS_FROM",
      source: source.name,
      target: trace.repoName || trace.repoId,
      verb: "deploys from",
      canonicalSource: source.sourceId,
      canonicalTarget: source.targetId,
    };
  }
  if (source.relationshipType === "DEPLOYMENT_SOURCE") {
    return {
      family: "DEPLOYMENT_SOURCE",
      source: deploymentSourceInstanceLabel(source, trace),
      target: source.name,
      verb: "deployment source",
      canonicalSource: source.sourceId,
      canonicalTarget: source.targetId ?? source.id,
    };
  }
  return {
    family: "relationship family unavailable",
    source: source.sourceId ?? "source identity unavailable",
    target: source.targetId ?? source.name,
    verb: "relationship unavailable",
    canonicalSource: source.sourceId,
    canonicalTarget: source.targetId,
  };
}

function deploymentSourceInstanceLabel(
  source: DeploymentTraceSource,
  trace: DeploymentTraceResult,
): string {
  const service = trace.serviceName || trace.workloadId || "service identity unavailable";
  const instance = trace.instances.find((candidate) => candidate.id === source.sourceId);
  if (instance === undefined) {
    return `${service} (runtime instance not returned)`;
  }
  const environment = instance.environment?.trim();
  return environment
    ? `${service} (${environment} runtime instance)`
    : `${service} (runtime instance)`;
}
