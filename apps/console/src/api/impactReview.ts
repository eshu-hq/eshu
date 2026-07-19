import {
  normalizeChangeSurfaceInvestigation,
  type ChangeSurfaceInvestigation,
  type ChangeSurfaceResponse,
} from "./changeSurface";
import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, type EshuTruth } from "./envelope";
import { selectImpactGraph } from "./impactGraph";
import {
  normalizeDeploymentTrace,
  type DeploymentTraceResponse,
} from "./impactReviewDeploymentNormalization";
import type {
  BlastAffectedEntity,
  BlastRadiusResult,
  BlastTargetType,
  DeploymentTraceResult,
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
