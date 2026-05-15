import type {
  DeploymentArtifactRecord,
  ServiceContextResponse,
  ServiceDeploymentLane
} from "./serviceSpotlight";

export function deploymentLanes(context: ServiceContextResponse): readonly ServiceDeploymentLane[] {
  if ((context.deployment_lanes?.length ?? 0) > 0) {
    return (context.deployment_lanes ?? []).map((lane) => {
      const kind = normalizedPlatform(lane.lane_type);
      const artifactEvidenceCount = laneEvidenceCount(context, kind);
      const artifactRelationshipTypes = laneRelationshipTypes(context, kind);
      const artifactSourceRepos = laneSourceRepos(context, kind);
      return {
        confidence: lane.max_confidence,
        environments: lane.environments ?? [],
        evidenceCount: artifactEvidenceCount > 0 ? artifactEvidenceCount : lane.resolved_ids?.length ?? 0,
        label: laneLabel(kind),
        relationshipTypes: artifactRelationshipTypes.length > 0
          ? artifactRelationshipTypes
          : lane.relationship_types ?? [],
        resolvedCount: artifactEvidenceCount > 0 ? artifactEvidenceCount : lane.resolved_ids?.length ?? 0,
        sourceRepos: artifactSourceRepos.length > 0 ? artifactSourceRepos : lane.source_repositories ?? []
      };
    });
  }
  const platforms = new Map<string, Set<string>>();
  for (const instance of context.instances ?? []) {
    for (const platform of instance.platforms ?? []) {
      const kind = normalizedPlatform(platform.platform_kind);
      if (kind.length === 0) {
        continue;
      }
      const envs = platforms.get(kind) ?? new Set<string>();
      envs.add(nonEmpty(instance.environment, platform.platform_name, "runtime"));
      platforms.set(kind, envs);
    }
  }
  return Array.from(platforms.entries()).map(([kind, environments]) => {
    const evidenceCount = laneEvidenceCount(context, kind);
    return {
      environments: Array.from(environments),
      evidenceCount,
      label: laneLabel(kind),
      relationshipTypes: laneRelationshipTypes(context, kind),
      resolvedCount: evidenceCount,
      sourceRepos: laneSourceRepos(context, kind)
    };
  });
}

function normalizedPlatform(kind: string | undefined): string {
  const value = nonEmpty(kind).toLowerCase();
  if (value.includes("ecs") && value.includes("terraform")) {
    return "ecs_terraform";
  }
  if (value.includes("gitops")) {
    return "k8s_gitops";
  }
  if (value.includes("ecs")) {
    return "ecs";
  }
  if (value.includes("kubernetes") || value.includes("eks")) {
    return "kubernetes";
  }
  return value;
}

function laneLabel(kind: string): string {
  switch (kind) {
    case "ecs_terraform":
      return "ECS Terraform";
    case "k8s_gitops":
      return "Kubernetes GitOps";
    case "ecs":
      return "ECS";
    case "kubernetes":
      return "Kubernetes";
    default:
      return kind;
  }
}

function laneEvidenceCount(context: ServiceContextResponse, kind: string): number {
  return laneArtifacts(context, kind).length;
}

function laneSourceRepos(context: ServiceContextResponse, kind: string): readonly string[] {
  return unique(laneArtifacts(context, kind).map((artifact) => nonEmpty(artifact.source_repo_name)));
}

function laneRelationshipTypes(
  context: ServiceContextResponse,
  kind: string
): readonly string[] {
  return unique(laneArtifacts(context, kind).map((artifact) => nonEmpty(artifact.relationship_type)));
}

function laneArtifacts(
  context: ServiceContextResponse,
  kind: string
): readonly DeploymentArtifactRecord[] {
  const artifacts = context.deployment_evidence?.artifacts ?? [];
  if (kind.includes("ecs")) {
    return artifacts.filter(isEcsDeploymentArtifact);
  }
  return artifacts.filter(isKubernetesDeploymentArtifact);
}

function isEcsDeploymentArtifact(artifact: DeploymentArtifactRecord): boolean {
  const evidenceKind = nonEmpty(artifact.evidence_kind).toUpperCase();
  const family = nonEmpty(artifact.artifact_family).toLowerCase();
  return family === "terraform" && evidenceKind.startsWith("TERRAFORM_ECS_");
}

function isKubernetesDeploymentArtifact(artifact: DeploymentArtifactRecord): boolean {
  const family = nonEmpty(artifact.artifact_family).toLowerCase();
  const relationshipType = nonEmpty(artifact.relationship_type).toUpperCase();
  return ["argocd", "helm", "kustomize", "kubernetes"].includes(family) &&
    relationshipType === "DEPLOYS_FROM";
}

function unique(values: readonly string[]): readonly string[] {
  return [...new Set(values.filter((value) => value.trim().length > 0))];
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}
