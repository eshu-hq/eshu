import type {
  DeploymentGraph,
  DeploymentGraphLink,
  DeploymentGraphNode
} from "./mockData";
import type {
  ContextConsumer,
  ContextResponse,
  DeploymentEvidenceArtifact,
  StoryResponse
} from "./repository";

interface EvidenceGroup {
  readonly artifacts: readonly DeploymentEvidenceArtifact[];
  readonly family: string;
  readonly relationshipType: string;
  readonly sourceRepo: string;
}

export function deploymentGraphFromStory(
  story: StoryResponse,
  context: ContextResponse | undefined
): DeploymentGraph {
  const repoName = titleFromSubject(story.subject, story.repository?.name ?? "repository");
  const serviceName = story.deployment_overview?.workloads?.[0] ?? repoName;
  const nodes = new Map<string, DeploymentGraphNode>();
  const links: DeploymentGraphLink[] = [];

  addNode(nodes, {
    column: 0,
    detail: story.repository?.local_path,
    id: "source:repo",
    kind: "repository",
    label: `${repoName} repo`,
    lane: "build"
  });
  addNode(nodes, {
    column: 3,
    detail: `${serviceName} service deployment context`,
    id: "target:service",
    kind: "service",
    label: `${serviceName} service`,
    lane: "service"
  });

  const evidenceGroups = groupDeploymentEvidence(context?.deployment_evidence?.artifacts ?? []);
  const graphGroups = evidenceGroups.length > 0 ? evidenceGroups : groupConsumerEvidence(context);
  for (const [index, group] of graphGroups.entries()) {
    const lane = `${group.relationshipType}:${group.family}:${group.sourceRepo}`;
    const evidenceID = `evidence:${group.family}:${group.sourceRepo}:${group.relationshipType}`;
    addNode(nodes, {
      column: 0,
      detail: group.sourceRepo,
      id: `source:${group.sourceRepo}`,
      kind: "repository",
      label: group.sourceRepo,
      lane
    });
    addNode(nodes, {
      column: 1,
      detail: evidenceDetail(group.artifacts),
      id: evidenceID,
      kind: "evidence",
      label: evidenceLabel(group.family),
      lane
    });
    links.push({
      detail: evidenceDetail(group.artifacts),
      label: group.relationshipType,
      source: `source:${group.sourceRepo}`,
      target: evidenceID
    });

    const environment = firstEnvironment(group.artifacts);
    if (environment.length > 0) {
      const environmentID = `environment:${group.sourceRepo}:${environment}`;
      addNode(nodes, {
        column: 2,
        detail: "Observed from deployment evidence",
        id: environmentID,
        kind: "environment",
        label: environment,
        lane
      });
      links.push({ label: "targets", source: evidenceID, target: environmentID });
      links.push({
        detail: `${group.sourceRepo} ${group.relationshipType} ${serviceName}`,
        label: group.relationshipType,
        source: environmentID,
        target: "target:service"
      });
    } else {
      links.push({
        detail: `${group.sourceRepo} ${group.relationshipType} ${serviceName}`,
        label: group.relationshipType,
        source: evidenceID,
        target: "target:service"
      });
    }

    if (index >= 3) {
      break;
    }
  }

  const buildPath = primaryBuildPath(story);
  if (buildPath !== undefined) {
    addNode(nodes, {
      column: 1,
      detail: buildPath.path ?? buildPath.relative_path,
      id: "build:workflow",
      kind: "workflow",
      label: `GitHub Actions: ${nonEmpty(buildPath.workflow_name, "workflow")}`,
      lane: "build"
    });
    addNode(nodes, {
      column: 2,
      detail: `Builds ${joinShort(buildPath.delivery_command_families ?? [])}`,
      id: "build:artifact",
      kind: "artifact",
      label: buildArtifactLabel(buildPath.delivery_command_families ?? []),
      lane: "build"
    });
    links.push({ label: "builds", source: "source:repo", target: "build:workflow" });
    links.push({ label: "publishes", source: "build:workflow", target: "build:artifact" });
    links.push({ label: "image for", source: "build:artifact", target: "target:service" });
  }

  if (nodes.size <= 2) {
    for (const family of story.deployment_overview?.infrastructure_families?.slice(0, 4) ?? []) {
      const familyID = `family:${family}`;
      addNode(nodes, {
        column: 1,
        id: familyID,
        kind: "artifact",
        label: family,
        lane: family
      });
      links.push({ label: "contains", source: "source:repo", target: familyID });
      links.push({ label: "describes", source: familyID, target: "target:service" });
    }
  }

  return {
    links: dedupeLinks(links),
    nodes: Array.from(nodes.values())
  };
}

function groupDeploymentEvidence(
  artifacts: readonly DeploymentEvidenceArtifact[]
): readonly EvidenceGroup[] {
  const groups = new Map<string, DeploymentEvidenceArtifact[]>();
  for (const artifact of artifacts) {
    const family = deploymentFamily(artifact);
    if (family.length === 0) {
      continue;
    }
    const sourceRepo = nonEmpty(artifact.source_repo_name, artifact.source_location?.repo_name);
    const relationshipType = relationshipLabel(artifact);
    const key = `${family}:${sourceRepo}:${relationshipType}`;
    groups.set(key, [...(groups.get(key) ?? []), artifact]);
  }
  return Array.from(groups.entries()).map(([key, artifacts]) => {
    const [family, sourceRepo, relationshipType] = key.split(":");
    return { artifacts, family, relationshipType, sourceRepo };
  });
}

function deploymentFamily(artifact: DeploymentEvidenceArtifact): string {
  const family = nonEmpty(artifact.artifact_family).toLowerCase();
  if (family === "argocd" || family === "helm") {
    return family;
  }
  if (family === "terraform" || nonEmpty(artifact.evidence_kind).startsWith("TERRAFORM_")) {
    return "terraform";
  }
  return "";
}

function groupConsumerEvidence(context: ContextResponse | undefined): readonly EvidenceGroup[] {
  const consumers = [...(context?.consumers ?? []), ...(context?.consumer_repositories ?? [])]
    .filter(isDeploymentConsumer);
  return consumers.map((consumer) => ({
    artifacts: [consumerArtifact(consumer)],
    family: consumerFamily(consumer),
    relationshipType: "DEPLOYS_FROM",
    sourceRepo: consumerName(consumer)
  }));
}

function consumerArtifact(consumer: ContextConsumer): DeploymentEvidenceArtifact {
  return {
    artifact_family: consumerFamily(consumer),
    evidence_kind: consumer.evidence_kinds?.[0],
    path: consumer.sample_paths?.[0],
    relationship_type: consumer.consumer_kinds?.[0],
    source_repo_name: consumerName(consumer)
  };
}

function isDeploymentConsumer(consumer: ContextConsumer): boolean {
  const name = consumerName(consumer).toLowerCase();
  const paths = (consumer.sample_paths ?? []).join(" ").toLowerCase();
  return name.includes("argocd") ||
    name.includes("helm") ||
    paths.includes("applicationsets/") ||
    paths.includes("charts/");
}

function consumerName(consumer: ContextConsumer): string {
  return nonEmpty(consumer.repo_name, consumer.repository, consumer.name, consumer.id);
}

function consumerFamily(consumer: ContextConsumer): string {
  const name = consumerName(consumer).toLowerCase();
  const paths = (consumer.sample_paths ?? []).join(" ").toLowerCase();
  if (name.includes("argocd") || paths.includes("applicationsets/")) {
    return "argocd";
  }
  return "helm";
}

function primaryBuildPath(story: StoryResponse) {
  return story.deployment_overview?.delivery_paths?.find((path) =>
    path.delivery_command_families?.some((family) => family === "docker" || family === "helm")
  );
}

function addNode(nodes: Map<string, DeploymentGraphNode>, node: DeploymentGraphNode): void {
  if (!nodes.has(node.id)) {
    nodes.set(node.id, node);
  }
}

function dedupeLinks(links: readonly DeploymentGraphLink[]): readonly DeploymentGraphLink[] {
  const seen = new Set<string>();
  return links.filter((link) => {
    const key = `${link.source}:${link.target}:${link.label}`;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function evidenceLabel(family: string): string {
  if (family === "argocd") {
    return "ArgoCD ApplicationSet";
  }
  if (family === "helm") {
    return "Helm chart/values";
  }
  if (family === "terraform") {
    return "Terraform ECS";
  }
  return family;
}

function relationshipLabel(artifact: DeploymentEvidenceArtifact | undefined): string {
  return nonEmpty(artifact?.relationship_type, artifact?.evidence_kind, "evidence");
}

function evidenceDetail(artifacts: readonly DeploymentEvidenceArtifact[]): string {
  const paths = artifacts
    .map((artifact) => nonEmpty(artifact.source_location?.path, artifact.path, artifact.name))
    .filter((path) => path.length > 0);
  return `${artifacts.length} evidence item(s): ${joinShort(paths)}`;
}

function firstEnvironment(artifacts: readonly DeploymentEvidenceArtifact[]): string {
  return artifacts.find((artifact) => nonEmpty(artifact.environment).length > 0)?.environment ?? "";
}

function buildArtifactLabel(families: readonly string[]): string {
  if (families.includes("docker")) {
    return "Docker image";
  }
  if (families.includes("helm")) {
    return "Helm artifact";
  }
  return "Build artifact";
}

function titleFromSubject(subject: StoryResponse["subject"], fallback: string): string {
  if (typeof subject === "string" && subject.trim().length > 0) {
    return subject;
  }
  if (typeof subject === "object" && subject !== null) {
    return subject.name ?? subject.id ?? fallback;
  }
  return fallback;
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}

function joinShort(values: readonly string[]): string {
  if (values.length === 0) {
    return "evidence";
  }
  if (values.length <= 2) {
    return values.join(", ");
  }
  return `${values.slice(0, 2).join(", ")} +${values.length - 2} more`;
}
