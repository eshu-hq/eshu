import type {
  ConsumerRecord,
  DeploymentArtifactRecord,
  ServiceContextResponse,
  ServiceRelationshipCluster,
  ServiceRelationshipRepository,
  ServiceTechnologyKind
} from "./serviceSpotlight";

interface ClusterDraft {
  readonly description: string;
  readonly kind: string;
  readonly label: string;
  readonly repositories: Map<string, RepositoryDraft>;
  readonly relationshipTypes: Set<string>;
  readonly technology: ServiceTechnologyKind;
}

interface RepositoryDraft {
  confidence?: number;
  readonly evidenceKinds: Set<string>;
  evidenceCount?: number;
  readonly paths: Set<string>;
  provenanceMethod?: string;
  rationale?: string;
  readonly relationshipTypes: Set<string>;
  readonly repository: string;
  state?: string;
  technology: ServiceTechnologyKind;
}

interface RelationshipDefinition {
  readonly description: string;
  readonly kind: string;
  readonly label: string;
  readonly technology: ServiceTechnologyKind;
}

export function relationshipClusters(
  context: ServiceContextResponse
): readonly ServiceRelationshipCluster[] {
  const clusters = new Map<string, ClusterDraft>();
  for (const artifact of context.deployment_evidence?.artifacts ?? []) {
    const relationshipType = nonEmpty(artifact.relationship_type, "OBSERVED_REFERENCE");
    const repository = nonEmpty(artifact.source_repo_name, artifact.target_repo_name);
    if (repository.length === 0) {
      continue;
    }
    addRepositoryEvidence(clusters, relationshipType, repository, {
      confidence: artifact.confidence,
      confidenceBasis: artifact.confidence_basis,
      evidenceCount: artifact.evidence_count,
      evidenceKind: artifact.evidence_kind,
      outcome: artifact.outcome,
      path: artifact.path,
      provenanceOnly: artifact.provenance_only,
      rationale: artifact.rationale,
      resolutionSource: artifact.resolution_source,
      sourceFreshness: artifact.source_freshness,
      state: artifact.state,
      technology: technologyForArtifact(artifact)
    });
  }
  for (const consumer of [
    ...(context.consumer_repositories ?? []),
    ...(context.graph_dependents ?? [])
  ]) {
    addConsumerEvidence(clusters, consumer);
  }
  return orderClusters([...clusters.values()].map(finalizeCluster));
}

function addConsumerEvidence(clusters: Map<string, ClusterDraft>, consumer: ConsumerRecord): void {
  const repository = nonEmpty(consumer.repo_name, consumer.repository);
  if (repository.length === 0) {
    return;
  }
  const relationshipTypes = consumer.relationship_types ?? consumer.graph_relationship_types ?? [];
  for (const relationshipType of relationshipTypes.length > 0 ? relationshipTypes : ["OBSERVED_REFERENCE"]) {
    addRepositoryEvidence(clusters, relationshipType, repository, {
      confidence: consumer.confidence,
      confidenceBasis: consumer.confidence_basis,
      evidenceCount: consumer.evidence_count,
      evidenceKind: consumer.evidence_kinds?.[0] ?? consumer.consumer_kinds?.[0],
      outcome: consumer.outcome,
      path: consumer.sample_paths?.[0],
      provenanceOnly: consumer.provenance_only,
      rationale: consumer.rationale,
      resolutionSource: consumer.resolution_source,
      sourceFreshness: consumer.source_freshness,
      state: consumer.state,
      technology: technologyForConsumer(consumer)
    });
  }
}

function addRepositoryEvidence(
  clusters: Map<string, ClusterDraft>,
  relationshipType: string,
  repository: string,
  evidence: {
    readonly confidence?: number;
    readonly confidenceBasis?: string;
    readonly evidenceCount?: number;
    readonly evidenceKind?: string;
    readonly outcome?: string;
    readonly path?: string;
    readonly provenanceOnly?: boolean;
    readonly rationale?: string;
    readonly resolutionSource?: string;
    readonly sourceFreshness?: string;
    readonly state?: string;
    readonly technology: ServiceTechnologyKind;
  }
): void {
  const definition = definitionForRelationship(relationshipType, evidence.technology);
  const cluster = clusters.get(definition.kind) ?? {
    description: definition.description,
    kind: definition.kind,
    label: definition.label,
    repositories: new Map<string, RepositoryDraft>(),
    relationshipTypes: new Set<string>(),
    technology: definition.technology
  };
  cluster.relationshipTypes.add(relationshipType);
  const draft = cluster.repositories.get(repository) ?? {
    evidenceKinds: new Set<string>(),
    paths: new Set<string>(),
    relationshipTypes: new Set<string>(),
    repository,
    technology: evidence.technology
  };
  draft.relationshipTypes.add(relationshipType);
  draft.confidence = maxOptionalNumber(draft.confidence, evidence.confidence);
  draft.evidenceCount = maxOptionalNumber(draft.evidenceCount, evidence.evidenceCount);
  draft.provenanceMethod = firstNonEmpty(
    draft.provenanceMethod,
    evidence.confidenceBasis,
    evidence.resolutionSource,
    evidence.evidenceKind
  );
  draft.rationale = firstNonEmpty(draft.rationale, evidence.rationale);
  draft.state = relationshipState(draft.state, evidence);
  if (evidence.evidenceKind !== undefined && evidence.evidenceKind.trim().length > 0) {
    draft.evidenceKinds.add(evidence.evidenceKind);
  }
  if (evidence.path !== undefined && evidence.path.trim().length > 0) {
    draft.paths.add(evidence.path);
  }
  if (technologyRank(evidence.technology) > technologyRank(draft.technology)) {
    draft.technology = evidence.technology;
  }
  cluster.repositories.set(repository, draft);
  clusters.set(definition.kind, cluster);
}

function definitionForRelationship(
  relationshipType: string,
  technology: ServiceTechnologyKind
): RelationshipDefinition {
  switch (relationshipType.toUpperCase()) {
    case "DEPLOYS_FROM":
      return {
        description: "Repos and artifacts that deploy this service into a runtime.",
        kind: "deployment",
        label: "Deployment sources",
        technology: technology === "terraform" ? "terraform" : "kubernetes"
      };
    case "PROVISIONS_DEPENDENCY_FOR":
      return {
        description: "Infrastructure resources that provision runtime dependencies for this service.",
        kind: "runtime_provisioning",
        label: "Runtime provisioning",
        technology: "terraform"
      };
    case "READS_CONFIG_FROM":
      return {
        description: "Repos that read, grant, or depend on this service's config such as SSM parameters.",
        kind: "configuration_access",
        label: "Configuration access",
        technology: "terraform"
      };
    case "DISCOVERS_CONFIG_IN":
      return {
        description: "Automation that discovers configuration for this service before runtime delivery.",
        kind: "configuration_discovery",
        label: "Configuration discovery",
        technology: "github_actions"
      };
    default:
      return {
        description: "Observed references that are not yet classified as deployment truth.",
        kind: "observed_reference",
        label: "Observed references",
        technology
      };
  }
}

function technologyForArtifact(artifact: DeploymentArtifactRecord): ServiceTechnologyKind {
  const family = nonEmpty(artifact.artifact_family).toLowerCase();
  const evidenceKind = nonEmpty(artifact.evidence_kind).toUpperCase();
  if (family === "terraform" || evidenceKind.startsWith("TERRAFORM_")) {
    return "terraform";
  }
  if (family === "helm") {
    return "helm";
  }
  if (family === "argocd") {
    return "argocd";
  }
  if (family === "kustomize" || family === "kubernetes") {
    return "kubernetes";
  }
  if (evidenceKind.includes("GITHUB_ACTIONS")) {
    return "github_actions";
  }
  return "repository";
}

function technologyForConsumer(consumer: ConsumerRecord): ServiceTechnologyKind {
  const evidence = [
    ...(consumer.evidence_kinds ?? []),
    ...(consumer.consumer_kinds ?? []),
    ...(consumer.sample_paths ?? [])
  ].join(" ").toLowerCase();
  if (evidence.includes("terraform") || evidence.includes(".tf")) {
    return "terraform";
  }
  if (evidence.includes("helm")) {
    return "helm";
  }
  if (evidence.includes("argocd")) {
    return "argocd";
  }
  if (evidence.includes("github_actions")) {
    return "github_actions";
  }
  return "repository";
}

function finalizeCluster(cluster: ClusterDraft): ServiceRelationshipCluster {
  const repositories = [...cluster.repositories.values()].map(finalizeRepository);
  return {
    description: cluster.description,
    evidenceCount: repositories.reduce((total, repository) =>
      total + Math.max(1, repository.paths.length + repository.evidenceKinds.length), 0),
    kind: cluster.kind,
    label: cluster.label,
    relationshipTypes: [...cluster.relationshipTypes].sort(),
    repositories,
    technology: cluster.technology
  };
}

function finalizeRepository(repository: RepositoryDraft): ServiceRelationshipRepository {
  return {
    confidence: repository.confidence,
    evidenceKinds: [...repository.evidenceKinds].sort(),
    evidenceCount: repository.evidenceCount,
    paths: [...repository.paths].sort().slice(0, 4),
    provenanceMethod: repository.provenanceMethod,
    rationale: repository.rationale,
    relationshipTypes: [...repository.relationshipTypes].sort(),
    repository: repository.repository,
    state: repository.state,
    technology: repository.technology
  };
}

function orderClusters(
  clusters: readonly ServiceRelationshipCluster[]
): readonly ServiceRelationshipCluster[] {
  const order = [
    "deployment",
    "runtime_provisioning",
    "configuration_access",
    "configuration_discovery",
    "observed_reference"
  ];
  return [...clusters].sort((left, right) =>
    order.indexOf(left.kind) - order.indexOf(right.kind) ||
    right.repositories.length - left.repositories.length
  );
}

function technologyRank(technology: ServiceTechnologyKind): number {
  switch (technology) {
    case "terraform":
      return 6;
    case "helm":
      return 5;
    case "argocd":
      return 4;
    case "kubernetes":
      return 3;
    case "github_actions":
      return 2;
    case "config":
      return 1;
    default:
      return 0;
  }
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}

function firstNonEmpty(...values: readonly (string | undefined)[]): string | undefined {
  const value = nonEmpty(...values);
  return value.length > 0 ? value : undefined;
}

function maxOptionalNumber(left: number | undefined, right: number | undefined): number | undefined {
  if (right === undefined) {
    return left;
  }
  if (left === undefined) {
    return right;
  }
  return Math.max(left, right);
}

function relationshipState(
  current: string | undefined,
  evidence: {
    readonly outcome?: string;
    readonly provenanceOnly?: boolean;
    readonly sourceFreshness?: string;
    readonly state?: string;
  }
): string | undefined {
  const next = normalizedRelationshipState(evidence);
  if (next === undefined) {
    return current;
  }
  if (current === undefined || relationshipStateRank(next) > relationshipStateRank(current)) {
    return next;
  }
  return current;
}

function normalizedRelationshipState(evidence: {
  readonly outcome?: string;
  readonly provenanceOnly?: boolean;
  readonly sourceFreshness?: string;
  readonly state?: string;
}): string | undefined {
  const freshness = nonEmpty(evidence.sourceFreshness).toLowerCase();
  if (freshness === "stale") {
    return "stale";
  }
  const state = nonEmpty(evidence.state, evidence.outcome).toLowerCase();
  if (state === "stale" || state === "ambiguous" || state === "unresolved" || state === "rejected") {
    return state;
  }
  if (evidence.provenanceOnly === true) {
    return "provenance_only";
  }
  if (state === "exact" || state === "derived") {
    return state;
  }
  return undefined;
}

function relationshipStateRank(state: string): number {
  switch (state) {
    case "stale":
      return 6;
    case "ambiguous":
      return 5;
    case "unresolved":
    case "rejected":
      return 4;
    case "provenance_only":
      return 3;
    case "derived":
      return 2;
    case "exact":
      return 1;
    default:
      return 0;
  }
}
