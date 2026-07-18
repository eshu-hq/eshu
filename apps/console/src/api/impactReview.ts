import {
  normalizeChangeSurfaceInvestigation,
  type ChangeSurfaceInvestigation,
  type ChangeSurfaceResponse,
} from "./changeSurface";
import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, type EshuTruth } from "./envelope";
import { selectImpactGraph } from "./impactGraph";
import type {
  BlastAffectedEntity,
  BlastRadiusResult,
  BlastTargetType,
  DeploymentTraceEntity,
  DeploymentTraceFact,
  DeploymentTraceInstance,
  DeploymentTracePlatform,
  DeploymentTraceResult,
  DeploymentTraceSource,
  ImpactReview,
  ImpactReviewInput,
  ImpactSection,
  ImpactTargetKind,
} from "./impactReviewTypes";
import type { GraphEdge, GraphModel, GraphNode } from "../console/types";

interface BlastRadiusResponse {
  readonly affected?: readonly BlastAffectedRecord[];
  readonly affected_count?: number;
  readonly limit?: number;
  readonly target?: string;
  readonly target_type?: BlastTargetType;
  readonly truncated?: boolean;
}

interface BlastAffectedRecord {
  readonly claim?: string;
  readonly hops?: number;
  readonly repo?: string;
  readonly repo_id?: string;
  readonly risk?: string;
  readonly tier?: string;
}

interface DeploymentTraceResponse {
  readonly cloud_resources?: readonly Record<string, unknown>[];
  readonly deployment_overview?: Record<string, unknown>;
  readonly deployment_facts?: readonly Record<string, unknown>[];
  readonly deployment_sources?: readonly Record<string, unknown>[];
  readonly image_refs?: readonly string[];
  readonly k8s_resources?: readonly Record<string, unknown>[];
  readonly instances?: readonly Record<string, unknown>[];
  readonly repo_id?: string;
  readonly repo_name?: string;
  readonly service_name?: string;
  readonly story?: string;
  readonly workload_id?: string;
}

const impactSource = {
  blast: "/api/v0/impact/blast-radius",
  changeSurface: "/api/v0/impact/change-surface/investigate",
  deploymentTrace: "/api/v0/impact/trace-deployment-chain",
} as const;

export async function loadImpactReview(
  client: EshuApiClient,
  rawInput: ImpactReviewInput,
): Promise<ImpactReview> {
  const input = normalizeImpactInput(rawInput);
  const [blast, changeSurface, deploymentTrace] = await Promise.all([
    loadBlastSection(client, input),
    loadChangeSurfaceSection(client, input),
    loadDeploymentTraceSection(client, input),
  ]);
  const compositionStartedAt = performance.now();
  const selectedGraph = selectImpactGraph(
    input.target,
    input.targetKind,
    blast,
    changeSurface,
    deploymentTrace,
  );
  const compositionDurationMs = Math.max(0, performance.now() - compositionStartedAt);
  return {
    blast,
    changeSurface,
    deploymentTrace,
    graph: selectedGraph.graph,
    graphPresentation: { ...selectedGraph.presentation, compositionDurationMs },
    input,
  };
}

function normalizeImpactInput(rawInput: ImpactReviewInput): ImpactReview["input"] {
  return {
    environment: optionalTrim(rawInput.environment),
    limit: clampInt(rawInput.limit, 25, 1, 100),
    maxDepth: clampInt(rawInput.maxDepth, 4, 1, 8),
    repoId: optionalTrim(rawInput.repoId),
    target: rawInput.target.trim(),
    targetKind: rawInput.targetKind,
  };
}

async function loadBlastSection(
  client: EshuApiClient,
  input: ImpactReview["input"],
): Promise<ImpactSection<BlastRadiusResult>> {
  const targetType = blastTargetType(input.targetKind);
  if (targetType === null) {
    return {
      reason:
        "Blast radius requires a repository, Terraform module, Crossplane XRD, or SQL table anchor.",
      source: impactSource.blast,
      status: "skipped",
    };
  }
  return loadEnvelopeSection(
    impactSource.blast,
    () =>
      client.post<BlastRadiusResponse>(impactSource.blast, {
        limit: input.limit,
        target: input.target,
        target_type: targetType,
      }),
    (response) => normalizeBlastRadius(response, input.target, targetType),
  );
}

async function loadChangeSurfaceSection(
  client: EshuApiClient,
  input: ImpactReview["input"],
): Promise<ImpactSection<ChangeSurfaceInvestigation>> {
  return loadEnvelopeSection(
    impactSource.changeSurface,
    () => client.post<ChangeSurfaceResponse>(impactSource.changeSurface, changeSurfaceBody(input)),
    normalizeChangeSurfaceInvestigation,
  );
}

async function loadDeploymentTraceSection(
  client: EshuApiClient,
  input: ImpactReview["input"],
): Promise<ImpactSection<DeploymentTraceResult>> {
  if (input.targetKind !== "service" && input.targetKind !== "workload") {
    return {
      reason: "Trace requires a service or workload name.",
      source: impactSource.deploymentTrace,
      status: "skipped",
    };
  }
  return loadEnvelopeSection(
    impactSource.deploymentTrace,
    () =>
      client.post<DeploymentTraceResponse>(impactSource.deploymentTrace, {
        direct_only: false,
        include_related_module_usage: false,
        max_depth: input.maxDepth,
        service_name: input.target,
      }),
    normalizeDeploymentTrace,
  );
}

async function loadEnvelopeSection<TWire, TData>(
  source: string,
  load: () => Promise<{
    readonly data: TWire | null;
    readonly error: { readonly code: string; readonly message: string } | null;
    readonly truth: EshuTruth | null;
  }>,
  normalize: (wire: TWire) => TData,
): Promise<ImpactSection<TData>> {
  try {
    const env = await load();
    if (env.error !== null) {
      throw new EshuEnvelopeError(env.error);
    }
    if (env.data === null) {
      throw new Error("Eshu envelope success response is missing data");
    }
    return {
      data: normalize(env.data),
      source,
      status: "ready",
      truth: env.truth,
    };
  } catch (error) {
    return {
      error: error instanceof Error ? error.message : "request failed",
      source,
      status: "unavailable",
    };
  }
}

function changeSurfaceBody(input: ImpactReview["input"]): Record<string, unknown> {
  const base: Record<string, unknown> = {
    limit: input.limit,
    max_depth: input.maxDepth,
  };
  if (input.environment !== undefined) {
    base.environment = input.environment;
  }
  if (input.repoId !== undefined) {
    base.repo_id = input.repoId;
  }
  switch (input.targetKind) {
    case "code_topic":
      return { ...base, topic: input.target };
    case "repository":
      return { ...base, target: input.target, target_type: "repository" };
    case "resource":
      return { ...base, resource_id: input.target };
    case "service":
      return { ...base, service_name: input.target };
    case "terraform_module":
      return { ...base, target: input.target, target_type: "terraform_module" };
    case "workload":
      return { ...base, target: input.target, target_type: "workload" };
    case "crossplane_xrd":
    case "sql_table":
      return { ...base, query: input.target };
  }
}

function normalizeBlastRadius(
  response: BlastRadiusResponse,
  fallbackTarget: string,
  fallbackTargetType: BlastTargetType,
): BlastRadiusResult {
  const target = nonEmpty(response.target, fallbackTarget);
  const targetType = response.target_type ?? fallbackTargetType;
  const affected = (response.affected ?? [])
    .map(normalizeBlastAffected)
    .filter((entity): entity is BlastAffectedEntity => entity !== null);
  return {
    affected,
    affectedCount: response.affected_count ?? affected.length,
    graph: blastRadiusGraph(target, affected),
    limit: response.limit ?? 25,
    target,
    targetType,
    truncated: response.truncated ?? false,
  };
}

function normalizeBlastAffected(record: BlastAffectedRecord): BlastAffectedEntity | null {
  const repo = nonEmpty(record.repo);
  if (repo.length === 0 || /\s/.test(repo)) {
    return null;
  }
  return {
    claim: optionalTrim(record.claim),
    hops: record.hops ?? 1,
    repo,
    repoId: optionalTrim(record.repo_id),
    risk: optionalTrim(record.risk),
    tier: optionalTrim(record.tier),
  };
}

function blastRadiusGraph(target: string, affected: readonly BlastAffectedEntity[]): GraphModel {
  const centerId = target;
  const nodes = new Map<string, GraphNode>();
  nodes.set(centerId, {
    col: 0,
    hero: true,
    id: centerId,
    kind: "repo",
    label: target,
    sub: "impact origin",
    truth: "exact",
  });
  const edges: GraphEdge[] = [];
  for (const entity of affected) {
    const id = entity.repoId ?? entity.repo;
    if (id === centerId) {
      continue;
    }
    nodes.set(id, {
      col: entity.hops,
      id,
      kind: "repo",
      label: entity.repo,
      sub: `hop ${entity.hops}`,
      truth: "exact",
    });
    edges.push({
      layer: "runtime",
      s: id,
      t: centerId,
      verb: "DEPENDS_ON",
    });
  }
  return { edges, nodes: [...nodes.values()] };
}

function normalizeDeploymentTrace(response: DeploymentTraceResponse): DeploymentTraceResult {
  return {
    cloudResources: (response.cloud_resources ?? []).map((record) =>
      normalizeTraceEntity(record, ["name", "id", "resource_type"]),
    ),
    deploymentOverview: response.deployment_overview ?? {},
    deploymentFacts: (response.deployment_facts ?? []).map(normalizeDeploymentFact),
    deploymentSources: (response.deployment_sources ?? []).map(normalizeDeploymentSource),
    imageRefs: response.image_refs ?? [],
    k8sResources: (response.k8s_resources ?? []).map((record) =>
      normalizeTraceEntity(record, ["entity_name", "name", "kind"]),
    ),
    instances: (response.instances ?? []).map(normalizeDeploymentInstance),
    repoId: nonEmpty(response.repo_id),
    repoName: nonEmpty(response.repo_name),
    serviceName: nonEmpty(response.service_name),
    story: nonEmpty(response.story, "Deployment trace returned no story text."),
    workloadId: nonEmpty(response.workload_id),
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
  return {
    confidence: numberField(record, "platform_confidence"),
    id: optionalTrim(stringField(record, "platform_id")),
    kind: optionalTrim(stringField(record, "platform_kind")),
    name: nonEmpty(stringField(record, "platform_name"), "runtime platform"),
    reason: optionalTrim(stringField(record, "platform_reason")),
  };
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

function blastTargetType(kind: ImpactTargetKind): BlastTargetType | null {
  if (
    kind === "crossplane_xrd" ||
    kind === "repository" ||
    kind === "sql_table" ||
    kind === "terraform_module"
  ) {
    return kind;
  }
  return null;
}

function clampInt(value: number | undefined, fallback: number, min: number, max: number): number {
  if (value === undefined || !Number.isFinite(value)) {
    return fallback;
  }
  return Math.max(min, Math.min(max, Math.trunc(value)));
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

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
