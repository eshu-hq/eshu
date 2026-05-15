import type { EshuApiClient } from "./client";
import type { DeploymentConfigInfluence } from "./deploymentConfigInfluence";
import type { DeploymentGraph } from "./mockData";
import {
  normalizeServiceInvestigation,
  type ServiceInvestigation,
  type ServiceInvestigationResponse
} from "./serviceInvestigation";
import { deploymentGraph } from "./serviceSpotlightGraph";
import {
  buildServiceTrafficPaths,
  type ServiceTrafficPath,
  type ServiceTrafficPathContext
} from "./serviceTrafficPath";

export interface ServiceSpotlight {
  readonly api: {
    readonly endpointCount: number;
    readonly endpoints: readonly ServiceEndpoint[];
    readonly methodCount: number;
    readonly sourcePaths: readonly string[];
  };
  readonly consumers: readonly ServiceConsumer[];
  readonly configInfluence?: DeploymentConfigInfluence;
  readonly dependencies: readonly ServiceDependency[];
  readonly deploymentGraph: DeploymentGraph;
  readonly graphDependents: readonly ServiceConsumer[];
  readonly hostnames: readonly ServiceHostname[];
  readonly investigation: ServiceInvestigation;
  readonly lanes: readonly ServiceDeploymentLane[];
  readonly name: string;
  readonly relationshipCounts: {
    readonly downstream: number;
    readonly graphDependents: number;
    readonly references: number;
    readonly upstream: number;
  };
  readonly repoName: string;
  readonly summary: string;
  readonly trafficPaths?: readonly ServiceTrafficPath[];
}

export interface ServiceEndpoint {
  readonly methods: readonly string[];
  readonly operationIds: readonly string[];
  readonly path: string;
  readonly sourcePaths: readonly string[];
}

export interface ServiceHostname {
  readonly environment: string;
  readonly hostname: string;
  readonly path: string;
}

export interface ServiceDeploymentLane {
  readonly confidence?: number;
  readonly environments: readonly string[];
  readonly evidenceCount: number;
  readonly label: string;
  readonly relationshipTypes: readonly string[];
  readonly resolvedCount: number;
  readonly sourceRepos: readonly string[];
}

export interface ServiceDependency {
  readonly confidence?: number;
  readonly evidenceCount?: number;
  readonly rationale: string;
  readonly resolvedId?: string;
  readonly targetName: string;
  readonly type: string;
}

export interface ServiceConsumer {
  readonly consumerKinds: readonly string[];
  readonly matchedValues: readonly string[];
  readonly relationshipTypes: readonly string[];
  readonly repository: string;
  readonly samplePaths: readonly string[];
}

interface RepositoryRecord {
  readonly id?: string;
  readonly name?: string;
  readonly repo_slug?: string;
}

export interface ServiceContextResponse extends ServiceTrafficPathContext {
  readonly api_surface?: {
    readonly endpoint_count?: number;
    readonly endpoints?: readonly EndpointRecord[];
    readonly method_count?: number;
    readonly source_paths?: readonly string[];
  };
  readonly consumer_repositories?: readonly ConsumerRecord[];
  readonly content_consumers?: readonly ConsumerRecord[];
  readonly dependencies?: readonly DependencyRecord[];
  readonly downstream_counts?: {
    readonly graphDependents?: number;
    readonly references?: number;
  };
  readonly deployment_evidence?: {
    readonly artifacts?: readonly DeploymentArtifactRecord[];
  };
  readonly deployment_lanes?: readonly ServiceDeploymentLaneRecord[];
  readonly graph_dependents?: readonly ConsumerRecord[];
  readonly hostnames?: readonly HostnameRecord[];
  readonly investigation?: ServiceInvestigationResponse;
  readonly instances?: readonly InstanceRecord[];
  readonly kind?: string;
  readonly name?: string;
  readonly provisioning_source_chains?: readonly ProvisioningRecord[];
  readonly repo_name?: string;
  readonly result_limits?: {
    readonly downstream_count?: number;
    readonly upstream_count?: number;
  };
}

interface EndpointRecord {
  readonly methods?: readonly string[];
  readonly operation_ids?: readonly string[] | null;
  readonly path?: string;
  readonly source_paths?: readonly string[];
}

interface ConsumerRecord {
  readonly consumer_kinds?: readonly string[];
  readonly graph_relationship_types?: readonly string[];
  readonly matched_values?: readonly string[];
  readonly repo_name?: string;
  readonly repository?: string;
  readonly relationship_types?: readonly string[];
  readonly sample_paths?: readonly string[];
}

interface DependencyRecord {
  readonly confidence?: number;
  readonly evidence_count?: number;
  readonly rationale?: string;
  readonly resolved_id?: string;
  readonly target_name?: string;
  readonly type?: string;
}

interface DeploymentArtifactRecord {
  readonly artifact_family?: string;
  readonly evidence_kind?: string;
  readonly path?: string;
  readonly relationship_type?: string;
  readonly resolved_id?: string;
  readonly source_repo_name?: string;
  readonly target_repo_name?: string;
}

interface HostnameRecord {
  readonly environment?: string;
  readonly hostname?: string;
  readonly relative_path?: string;
}

interface ServiceDeploymentLaneRecord {
  readonly environments?: readonly string[];
  readonly lane_type?: string;
  readonly max_confidence?: number;
  readonly relationship_types?: readonly string[];
  readonly resolved_ids?: readonly string[];
  readonly source_repositories?: readonly string[];
}

interface InstanceRecord {
  readonly environment?: string;
  readonly platforms?: readonly PlatformRecord[];
}

interface PlatformRecord {
  readonly platform_kind?: string;
  readonly platform_name?: string;
}

interface ProvisioningRecord {
  readonly repository?: string;
  readonly sample_paths?: readonly string[];
}

export async function loadServiceSpotlight(
  client: EshuApiClient,
  repositories: readonly RepositoryRecord[]
): Promise<ServiceSpotlight | undefined> {
  const candidates = selectServiceCandidates(repositories);
  const loaded = await Promise.all(
    candidates.map(async (candidate) => loadCandidate(client, candidate))
  );
  return loaded
    .filter((candidate): candidate is ServiceSpotlight => candidate !== undefined)
    .sort((left, right) => scoreSpotlight(right) - scoreSpotlight(left))[0];
}

async function loadCandidate(
  client: EshuApiClient,
  serviceName: string
): Promise<ServiceSpotlight | undefined> {
  try {
    const context = await client.getJson<ServiceContextResponse>(
      `/api/v0/services/${encodeURIComponent(serviceName)}/context`
    );
    const spotlight = serviceSpotlightFromContext(context, serviceName);
    return scoreSpotlight(spotlight) > 0 ? spotlight : undefined;
  } catch {
    return undefined;
  }
}

export function serviceSpotlightFromContext(
  context: ServiceContextResponse,
  fallbackName: string,
  configInfluence?: DeploymentConfigInfluence
): ServiceSpotlight {
  const name = nonEmpty(context.name, fallbackName);
  const endpoints = endpointRows(context.api_surface?.endpoints ?? []);
  const lanes = deploymentLanes(context);
  const rawReferences = context.content_consumers ?? context.consumer_repositories ?? [];
  const rawGraphDependents = context.graph_dependents ?? [];
  const rawConsumers = context.consumer_repositories ?? [...rawReferences, ...rawGraphDependents];
  const dependencies = dependencyRows(context.dependencies ?? []);
  const consumers = consumerRows(rawReferences);
  const graphDependents = consumerRows(rawGraphDependents);
  const allConsumers = consumerRows(rawConsumers);
  const relationshipCounts = {
    downstream: context.result_limits?.downstream_count ?? rawConsumers.length,
    graphDependents: context.downstream_counts?.graphDependents ?? rawGraphDependents.length,
    references: context.downstream_counts?.references ?? rawReferences.length,
    upstream: context.result_limits?.upstream_count ?? context.dependencies?.length ?? dependencies.length
  };
  return {
    api: {
      endpointCount: context.api_surface?.endpoint_count ?? endpoints.length,
      endpoints,
      methodCount: context.api_surface?.method_count ?? countMethods(endpoints),
      sourcePaths: context.api_surface?.source_paths ?? []
    },
    consumers,
    configInfluence,
    dependencies,
    deploymentGraph: deploymentGraph(name, lanes, dependencies, allConsumers),
    graphDependents,
    hostnames: hostnameRows(context.hostnames ?? []),
    investigation: normalizeServiceInvestigation(context.investigation),
    lanes,
    name,
    relationshipCounts,
    repoName: nonEmpty(context.repo_name, name),
    summary: spotlightSummary(
      name,
      context.api_surface?.endpoint_count ?? endpoints.length,
      lanes.length,
      relationshipCounts.upstream,
      relationshipCounts.downstream
    ),
    trafficPaths: buildServiceTrafficPaths(context, name, lanes)
  };
}

function selectServiceCandidates(
  repositories: readonly RepositoryRecord[]
): readonly string[] {
  return repositories
    .map((repository, index) => ({
      index,
      name: nonEmpty(repository.name, repository.repo_slug, repository.id),
      score: candidateScore(repository)
    }))
    .filter((candidate) => candidate.name.length > 0 && candidate.score > 0)
    .sort((left, right) => right.score - left.score || left.index - right.index)
    .slice(0, 60)
    .map((candidate) => candidate.name);
}

function candidateScore(repository: RepositoryRecord): number {
  const name = nonEmpty(repository.name, repository.repo_slug, repository.id).toLowerCase();
  const tokens = name.split(/[^a-z0-9]+/).filter((token) => token.length > 0);
  let score = 0;
  for (const token of ["api", "service", "app", "web", "server"]) {
    if (tokens.includes(token)) {
      score += 4;
    }
  }
  if (tokens[0] === "api") {
    score += 6;
  }
  for (const token of ["terraform", "helm", "argocd", "chart", "infra", "iac"]) {
    if (tokens.includes(token)) {
      score -= 3;
    }
  }
  return score;
}

function endpointRows(records: readonly EndpointRecord[]): readonly ServiceEndpoint[] {
  return records.slice(0, 50).map((record) => ({
    methods: record.methods ?? [],
    operationIds: record.operation_ids ?? [],
    path: nonEmpty(record.path, "/"),
    sourcePaths: record.source_paths ?? []
  }));
}

function deploymentLanes(context: ServiceContextResponse): readonly ServiceDeploymentLane[] {
  if ((context.deployment_lanes?.length ?? 0) > 0) {
    return (context.deployment_lanes ?? []).map((lane) => ({
      confidence: lane.max_confidence,
      environments: lane.environments ?? [],
      evidenceCount: lane.resolved_ids?.length ?? laneEvidenceCount(context, normalizedPlatform(lane.lane_type)),
      label: laneLabel(normalizedPlatform(lane.lane_type)),
      relationshipTypes: lane.relationship_types ?? [],
      resolvedCount: lane.resolved_ids?.length ?? 0,
      sourceRepos: lane.source_repositories ?? []
    }));
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
  return Array.from(platforms.entries()).map(([kind, environments]) => ({
    environments: Array.from(environments),
    evidenceCount: laneEvidenceCount(context, kind),
    label: laneLabel(kind),
    relationshipTypes: laneRelationshipTypes(context, kind),
    resolvedCount: laneEvidenceCount(context, kind),
    sourceRepos: laneSourceRepos(context, kind)
  }));
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
  const artifacts = context.deployment_evidence?.artifacts ?? [];
  if (kind.includes("ecs")) {
    return artifacts.filter((artifact) => nonEmpty(artifact.evidence_kind).startsWith("TERRAFORM_")).length;
  }
  return artifacts.filter((artifact) =>
    ["argocd", "helm", "kustomize"].includes(nonEmpty(artifact.artifact_family).toLowerCase())
  ).length;
}

function laneSourceRepos(context: ServiceContextResponse, kind: string): readonly string[] {
  if (kind.includes("ecs")) {
    return unique([
      ...((context.deployment_evidence?.artifacts ?? [])
        .filter((artifact) => nonEmpty(artifact.evidence_kind).startsWith("TERRAFORM_"))
        .map((artifact) => nonEmpty(artifact.source_repo_name))),
      ...((context.provisioning_source_chains ?? [])
        .map((record) => nonEmpty(record.repository)))
    ]);
  }
  return unique((context.deployment_evidence?.artifacts ?? [])
    .filter((artifact) =>
      ["argocd", "helm", "kustomize"].includes(nonEmpty(artifact.artifact_family).toLowerCase())
    )
    .map((artifact) => nonEmpty(artifact.source_repo_name)));
}

function laneRelationshipTypes(
  context: ServiceContextResponse,
  kind: string
): readonly string[] {
  const artifacts = context.deployment_evidence?.artifacts ?? [];
  const families = kind.includes("ecs")
    ? artifacts.filter((artifact) => nonEmpty(artifact.evidence_kind).startsWith("TERRAFORM_"))
    : artifacts.filter((artifact) =>
      ["argocd", "helm", "kustomize"].includes(nonEmpty(artifact.artifact_family).toLowerCase())
    );
  return unique(families.map((artifact) => nonEmpty(artifact.relationship_type)));
}

function dependencyRows(records: readonly DependencyRecord[]): readonly ServiceDependency[] {
  return records.slice(0, 12).map((record) => ({
    confidence: record.confidence,
    evidenceCount: record.evidence_count,
    rationale: nonEmpty(record.rationale, "Relationship evidence observed."),
    resolvedId: record.resolved_id,
    targetName: nonEmpty(record.target_name, "dependency"),
    type: nonEmpty(record.type, "DEPENDS_ON")
  }));
}

function hostnameRows(records: readonly HostnameRecord[]): readonly ServiceHostname[] {
  return records.slice(0, 12).map((record) => ({
    environment: nonEmpty(record.environment, "observed"),
    hostname: nonEmpty(record.hostname, "hostname pending"),
    path: nonEmpty(record.relative_path)
  }));
}

function consumerRows(records: readonly ConsumerRecord[]): readonly ServiceConsumer[] {
  return records.slice(0, 25).map((record) => ({
    consumerKinds: record.consumer_kinds ?? [],
    matchedValues: record.matched_values ?? [],
    relationshipTypes: record.relationship_types ?? record.graph_relationship_types ?? [],
    repository: nonEmpty(record.repo_name, record.repository, "consumer"),
    samplePaths: record.sample_paths ?? []
  }));
}

function scoreSpotlight(spotlight: ServiceSpotlight): number {
  return spotlight.api.endpointCount * 5 +
    spotlight.lanes.length * 8 +
    spotlight.dependencies.length * 3 +
    spotlight.consumers.length * 3;
}

function countMethods(endpoints: readonly ServiceEndpoint[]): number {
  return endpoints.reduce((total, endpoint) => total + endpoint.methods.length, 0);
}

function spotlightSummary(
  name: string,
  endpoints: number,
  lanes: number,
  dependencies: number,
  consumers: number
): string {
  return `${name} exposes ${endpoints} endpoint(s), runs through ${lanes} deployment lane(s), has ${dependencies} upstream relationship(s), and ${consumers} downstream relationship(s).`;
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
