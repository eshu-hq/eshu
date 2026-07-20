import { normalizeCloudResourceLimits } from "./impactCloudResourceLimits";
import { normalizeK8sResourceLimits } from "./impactK8sResourceLimits";
import type {
  DeploymentSourceLimits,
  DeploymentTraceEntity,
  DeploymentTraceFact,
  DeploymentTraceInstance,
  DeploymentTracePlatform,
  DeploymentTraceResult,
  DeploymentTraceSource,
  DeploymentTraceTopologyBasis,
  DeploymentTraceTopologyEdge,
} from "./impactReviewTypes";
import { normalizeRuntimeTopologyLimits } from "./impactRuntimeTopologyLimits";

export interface DeploymentTraceResponse {
  readonly cloud_resource_limits?: unknown;
  readonly cloud_resources?: readonly Record<string, unknown>[];
  readonly deployment_overview?: Record<string, unknown>;
  readonly deployment_facts?: readonly Record<string, unknown>[];
  readonly deployment_source_limits?: unknown;
  readonly deployment_sources?: readonly Record<string, unknown>[];
  readonly image_refs?: readonly string[];
  readonly k8s_resource_limits?: unknown;
  readonly k8s_resources?: readonly Record<string, unknown>[];
  readonly instances?: readonly Record<string, unknown>[];
  readonly provisioned_platforms?: readonly Record<string, unknown>[];
  readonly repo_id?: string;
  readonly repo_name?: string;
  readonly runtime_topology_limits?: unknown;
  readonly service_name?: string;
  readonly story?: string;
  readonly topology_edges?: readonly unknown[];
  readonly uncorrelated_cloud_resources_truncated?: boolean;
  readonly workload_id?: string;
}

export function normalizeDeploymentTrace(response: DeploymentTraceResponse): DeploymentTraceResult {
  const cloudResources = (response.cloud_resources ?? []).map((record) =>
    normalizeTraceEntity(record, ["name", "id", "resource_type"]),
  );
  const instances = (response.instances ?? []).map(normalizeDeploymentInstance);
  const k8sResources = (response.k8s_resources ?? []).map((record) =>
    normalizeTraceEntity(record, ["entity_name", "name", "kind"]),
  );
  const deploymentSources = (response.deployment_sources ?? []).map(normalizeDeploymentSource);
  const provisionedPlatforms = (response.provisioned_platforms ?? []).map(
    normalizeDeploymentPlatform,
  );
  const topology = normalizeDeploymentTopologyEdges(response.topology_edges);
  const invalidTopologyEdgeCount =
    topology.invalidCount +
    instances.reduce(
      (count, instance) =>
        count +
        instance.platforms.reduce(
          (platformCount, platform) => platformCount + (platform.invalidTopologyEdgeCount ?? 0),
          0,
        ),
      0,
    ) +
    provisionedPlatforms.reduce(
      (count, platform) => count + (platform.invalidTopologyEdgeCount ?? 0),
      0,
    );
  return {
    cloudResourceLimits: normalizeCloudResourceLimits(
      response.cloud_resource_limits,
      cloudResources.length,
    ),
    cloudResources,
    deploymentOverview: response.deployment_overview ?? {},
    deploymentFacts: (response.deployment_facts ?? []).map(normalizeDeploymentFact),
    deploymentSourceLimits: normalizeDeploymentSourceLimits(
      response.deployment_source_limits,
      deploymentSources.length,
    ),
    deploymentSources,
    imageRefs: response.image_refs ?? [],
    invalidTopologyEdgeCount,
    k8sResourceLimits: normalizeK8sResourceLimits(
      response.k8s_resource_limits,
      k8sResources.length,
    ),
    k8sResources,
    instances,
    provisionedPlatforms,
    repoId: nonEmpty(response.repo_id),
    repoName: nonEmpty(response.repo_name),
    runtimeTopologyLimits: normalizeRuntimeTopologyLimits(response.runtime_topology_limits, {
      instances: instances.length,
      platformEdges: instances.reduce((count, instance) => count + instance.platforms.length, 0),
      provisionedPlatforms: provisionedPlatforms.length,
    }),
    serviceName: nonEmpty(response.service_name),
    story: nonEmpty(response.story, "Deployment trace returned no story text."),
    topologyEdges: topology.edges,
    uncorrelatedCloudResourcesTruncated: response.uncorrelated_cloud_resources_truncated === true,
    workloadId: nonEmpty(response.workload_id),
  };
}

function normalizeDeploymentSourceLimits(
  value: unknown,
  deploymentSourceCount: number,
): DeploymentSourceLimits | null {
  if (!isRecord(value)) return null;
  const record = value;

  const canonicalObservedCount = nonNegativeIntegerField(record, "canonical_observed_count");
  const limit = positiveIntegerField(record, "limit");
  const observedCount = nonNegativeIntegerField(record, "observed_count");
  const querySentinelLimit = positiveIntegerField(record, "query_sentinel_limit");
  const repositoryObservedCount = nonNegativeIntegerField(record, "repository_observed_count");
  const returnedCount = nonNegativeIntegerField(record, "returned_count");
  const observedCountIsLowerBound = booleanField(record, "observed_count_is_lower_bound");
  const truncated = booleanField(record, "truncated");
  const ordering = stringArrayField(record, "ordering");

  if (
    canonicalObservedCount === undefined ||
    limit === undefined ||
    observedCount === undefined ||
    querySentinelLimit === undefined ||
    repositoryObservedCount === undefined ||
    returnedCount === undefined ||
    observedCountIsLowerBound === undefined ||
    truncated === undefined ||
    ordering === null
  ) {
    return null;
  }

  const truncationIsConsistent =
    truncated === (observedCountIsLowerBound || observedCount > returnedCount);
  if (
    returnedCount !== deploymentSourceCount ||
    returnedCount > limit ||
    returnedCount > observedCount ||
    querySentinelLimit !== limit + 1 ||
    ordering.length === 0 ||
    observedCount > canonicalObservedCount + repositoryObservedCount ||
    canonicalObservedCount > querySentinelLimit ||
    repositoryObservedCount > querySentinelLimit ||
    !truncationIsConsistent
  ) {
    return null;
  }

  return {
    canonicalObservedCount,
    limit,
    observedCount,
    observedCountIsLowerBound,
    ordering,
    querySentinelLimit,
    repositoryObservedCount,
    returnedCount,
    truncated,
  };
}

function normalizeDeploymentSource(record: Record<string, unknown>): DeploymentTraceSource {
  const name = nonEmpty(
    stringField(record, "repo_name"),
    stringField(record, "path"),
    "deployment source",
  );
  return {
    detail:
      [stringField(record, "path"), stringField(record, "reason")]
        .filter((value) => value.length > 0)
        .join(" · ") || undefined,
    id: optionalTrim(stringField(record, "repo_id")),
    kind: "repository",
    name,
    relationshipType: deploymentSourceRelationshipType(stringField(record, "relationship_type")),
    sourceId: optionalTrim(stringField(record, "source_id")),
    targetId: optionalTrim(stringField(record, "target_id")),
  };
}

function deploymentSourceRelationshipType(
  value: string,
): DeploymentTraceSource["relationshipType"] {
  return value === "DEPLOYMENT_SOURCE" || value === "DEPLOYS_FROM" ? value : undefined;
}

function normalizeDeploymentFact(record: Record<string, unknown>): DeploymentTraceFact {
  return {
    confidence: numberField(record, "confidence"),
    kind: optionalTrim(stringField(record, "kind")),
    reason: optionalTrim(stringField(record, "reason")),
    target: stringField(record, "target"),
    targetId: optionalTrim(stringField(record, "target_id")),
    type: stringField(record, "type"),
  };
}

function normalizeDeploymentInstance(record: Record<string, unknown>): DeploymentTraceInstance {
  const platforms = Array.isArray(record.platforms)
    ? record.platforms.filter(isRecord).map(normalizeDeploymentPlatform)
    : [];
  return {
    environment: optionalTrim(stringField(record, "environment")),
    id: stringField(record, "instance_id"),
    platforms,
  };
}

function normalizeDeploymentPlatform(record: Record<string, unknown>): DeploymentTracePlatform {
  const topology = normalizeDeploymentTopologyEdges(record.topology_edges);
  return {
    confidence: numberField(record, "platform_confidence"),
    id: optionalTrim(stringField(record, "platform_id")),
    invalidTopologyEdgeCount: topology.invalidCount,
    kind: optionalTrim(stringField(record, "platform_kind")),
    name: nonEmpty(stringField(record, "platform_name"), "runtime platform"),
    reason: optionalTrim(stringField(record, "platform_reason")),
    topologyBasis: deploymentTopologyBasis(stringField(record, "topology_basis")),
    topologyEdges: topology.edges,
  };
}

function normalizeDeploymentTopologyEdges(value: unknown): {
  readonly edges: readonly DeploymentTraceTopologyEdge[];
  readonly invalidCount: number;
} {
  if (!Array.isArray(value)) return { edges: [], invalidCount: 0 };
  const edges: DeploymentTraceTopologyEdge[] = [];
  let invalidCount = 0;
  for (const item of value) {
    if (!isRecord(item)) {
      invalidCount += 1;
      continue;
    }
    const edge = normalizeDeploymentTopologyEdge(item);
    if (edge === null) {
      invalidCount += 1;
    } else {
      edges.push(edge);
    }
  }
  return { edges, invalidCount };
}

function normalizeDeploymentTopologyEdge(
  record: Record<string, unknown>,
): DeploymentTraceTopologyEdge | null {
  const relationshipType = stringField(record, "relationship_type");
  if (
    relationshipType !== "DEFINES" &&
    relationshipType !== "INSTANCE_OF" &&
    relationshipType !== "RUNS_ON" &&
    relationshipType !== "PROVISIONS_DEPENDENCY_FOR" &&
    relationshipType !== "PROVISIONS_PLATFORM"
  ) {
    return null;
  }
  return {
    confidence: numberField(record, "confidence"),
    evidenceSource: optionalTrim(stringField(record, "evidence_source")),
    reason: optionalTrim(stringField(record, "reason")),
    relationshipType,
    sourceId: optionalTrim(stringField(record, "source_id")),
    sourceName: optionalTrim(stringField(record, "source_name")),
    sourceTool: optionalTrim(stringField(record, "source_tool")),
    targetId: optionalTrim(stringField(record, "target_id")),
    targetName: optionalTrim(stringField(record, "target_name")),
  };
}

function deploymentTopologyBasis(value: string): DeploymentTraceTopologyBasis | undefined {
  return value === "direct_runtime" || value === "provisioning_fallback" ? value : undefined;
}

function normalizeTraceEntity(
  record: Record<string, unknown>,
  fields: readonly string[],
): DeploymentTraceEntity {
  const name =
    fields.map((field) => stringField(record, field)).find((value) => value.length > 0) ?? "entity";
  return {
    detail:
      fields
        .map((field) => stringField(record, field))
        .filter((value) => value.length > 0 && value !== name)
        .join(" · ") || undefined,
    id: optionalTrim(
      stringField(record, "id") ||
        stringField(record, "entity_id") ||
        stringField(record, "resource_id"),
    ),
    kind: optionalTrim(stringField(record, "kind") || stringField(record, "resource_type")),
    name,
  };
}

function optionalTrim(value: string | undefined): string | undefined {
  const trimmed = value?.trim() ?? "";
  return trimmed.length > 0 ? trimmed : undefined;
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  return values.find((value) => value !== undefined && value.trim().length > 0)?.trim() ?? "";
}

function stringField(record: Record<string, unknown>, field: string): string {
  const value = record[field];
  return typeof value === "string" ? value : "";
}

function numberField(record: Record<string, unknown>, field: string): number | undefined {
  const value = record[field];
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function booleanField(record: Record<string, unknown>, field: string): boolean | undefined {
  const value = record[field];
  return typeof value === "boolean" ? value : undefined;
}

function nonNegativeIntegerField(
  record: Record<string, unknown>,
  field: string,
): number | undefined {
  const value = numberField(record, field);
  return value !== undefined && Number.isInteger(value) && value >= 0 ? value : undefined;
}

function positiveIntegerField(record: Record<string, unknown>, field: string): number | undefined {
  const value = nonNegativeIntegerField(record, field);
  return value !== undefined && value > 0 ? value : undefined;
}

function stringArrayField(
  record: Record<string, unknown>,
  field: string,
): readonly string[] | null {
  const value = record[field];
  if (!Array.isArray(value)) return null;
  const normalized = value.map((item) => (typeof item === "string" ? item.trim() : ""));
  return normalized.every((item) => item.length > 0) ? normalized : null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
