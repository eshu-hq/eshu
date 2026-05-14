import type { ServiceContextResponse } from "./serviceSpotlight";

export interface ServiceStoryDossierResponse {
  readonly api_surface?: ServiceContextResponse["api_surface"];
  readonly deployment_evidence?: ServiceContextResponse["deployment_evidence"];
  readonly deployment_lanes?: ServiceContextResponse["deployment_lanes"];
  readonly hostnames?: ServiceContextResponse["hostnames"];
  readonly investigation?: ServiceContextResponse["investigation"];
  readonly downstream_consumers?: {
    readonly content_consumer_count?: number;
    readonly content_consumers?: readonly ServiceStoryConsumer[];
    readonly graph_dependent_count?: number;
    readonly graph_dependents?: readonly ServiceStoryConsumer[];
  };
  readonly result_limits?: ServiceContextResponse["result_limits"];
  readonly service_identity?: {
    readonly repo_name?: string;
    readonly service_name?: string;
  };
  readonly service_name?: string;
  readonly upstream_dependencies?: readonly ServiceStoryDependency[];
}

interface ServiceStoryConsumer {
  readonly consumer_kinds?: readonly string[];
  readonly evidence_kinds?: readonly string[];
  readonly graph_relationship_types?: readonly string[];
  readonly matched_values?: readonly string[];
  readonly relationship_types?: readonly string[];
  readonly repo_name?: string;
  readonly repository?: string;
  readonly sample_paths?: readonly string[];
}

interface ServiceStoryDependency {
  readonly confidence?: number;
  readonly evidence_count?: number;
  readonly rationale?: string;
  readonly relationship_type?: string;
  readonly resolved_id?: string;
  readonly source?: string;
  readonly target?: string;
}

export function serviceContextFromStoryDossier(
  story: ServiceStoryDossierResponse,
  fallbackName: string
): ServiceContextResponse {
  const serviceName = nonEmpty(
    story.service_identity?.service_name,
    story.service_name,
    fallbackName
  );
  const rawArtifacts = story.deployment_evidence?.artifacts ?? [];
  const laneArtifacts = rawArtifacts.length > 0
    ? []
    : (story.deployment_lanes ?? []).flatMap((lane) =>
      syntheticArtifactsFromLane(lane)
    );
  return {
    api_surface: story.api_surface,
    consumer_repositories: consumerRows(story.downstream_consumers),
    content_consumers: consumerRows({
      content_consumers: story.downstream_consumers?.content_consumers
    }),
    dependencies: (story.upstream_dependencies ?? []).map((dependency) => ({
      confidence: dependency.confidence,
      evidence_count: dependency.evidence_count,
      rationale: dependency.rationale,
      resolved_id: dependency.resolved_id,
      target_name: nonEmpty(dependency.target, dependency.source),
      type: nonEmpty(dependency.relationship_type, "DEPENDS_ON")
    })),
    deployment_evidence: {
      artifact_count: rawArtifacts.length + laneArtifacts.length,
      artifacts: [...rawArtifacts, ...laneArtifacts]
    },
    deployment_lanes: story.deployment_lanes,
    downstream_counts: {
      graphDependents: story.downstream_consumers?.graph_dependent_count,
      references: story.downstream_consumers?.content_consumer_count
    },
    graph_dependents: consumerRows({
      content_consumers: story.downstream_consumers?.graph_dependents
    }),
    hostnames: story.hostnames,
    investigation: story.investigation,
    instances: (story.deployment_lanes ?? []).flatMap((lane) =>
      instancesFromLane(lane)
    ),
    name: serviceName,
    repo_name: nonEmpty(story.service_identity?.repo_name, serviceName),
    result_limits: story.result_limits
  };
}

function syntheticArtifactsFromLane(
  lane: NonNullable<ServiceStoryDossierResponse["deployment_lanes"]>[number]
): NonNullable<ServiceContextResponse["deployment_evidence"]>["artifacts"] {
  const family = artifactFamily(lane.lane_type);
  const evidenceKind = family === "terraform" ? "TERRAFORM_ECS_SERVICE" : "ARGOCD_GITOPS";
  return (lane.source_repositories ?? []).map((repository, index) => ({
    artifact_family: family,
    environment: lane.environments?.[0],
    evidence_kind: evidenceKind,
    relationship_type: lane.relationship_types?.[0],
    resolved_id: lane.resolved_ids?.[index] ?? lane.resolved_ids?.[0],
    source_repo_name: repository
  }));
}

function instancesFromLane(
  lane: NonNullable<ServiceStoryDossierResponse["deployment_lanes"]>[number]
): ServiceContextResponse["instances"] {
  const environments = lane.environments?.length === 0 ? ["observed"] : lane.environments;
  return (environments ?? ["observed"]).map((environment) => ({
    environment,
    platforms: [
      {
        platform_kind: nonEmpty(lane.lane_type, platformKind(lane.lane_type)),
        platform_name: environment
      }
    ]
  }));
}

function consumerRows(
  consumers: ServiceStoryDossierResponse["downstream_consumers"]
): ServiceContextResponse["consumer_repositories"] {
  return [
    ...(consumers?.content_consumers ?? []),
    ...(consumers?.graph_dependents ?? [])
  ].map((consumer) => ({
    consumer_kinds: consumer.consumer_kinds,
    evidence_kinds: consumer.evidence_kinds,
    graph_relationship_types: consumer.graph_relationship_types,
    matched_values: consumer.matched_values,
    relationship_types: consumer.relationship_types,
    repo_name: nonEmpty(consumer.repo_name, consumer.repository),
    sample_paths: consumer.sample_paths
  }));
}

function artifactFamily(laneType: string | undefined): string {
  return nonEmpty(laneType).includes("ecs") ? "terraform" : "argocd";
}

function platformKind(laneType: string | undefined): string {
  return nonEmpty(laneType).includes("ecs") ? "ecs" : "kubernetes";
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}
