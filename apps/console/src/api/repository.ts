import type { EshuApiClient } from "./client";
import type { EshuTruth } from "./envelope";
import { getDemoWorkspaceStory } from "./mockData";
import type { EntityKind, EvidenceRow, OverviewStat, WorkspaceStory } from "./mockData";
import { deploymentGraphFromStory } from "./deploymentGraph";
import type { ConsoleMode } from "../config/environment";

export interface StoryResponse {
  readonly deployment_overview?: {
    readonly delivery_paths?: readonly DeliveryPath[];
    readonly direct_story?: readonly string[];
    readonly infrastructure_families?: readonly string[];
    readonly topology_story?: readonly string[];
    readonly workload_count?: number;
    readonly workloads?: readonly string[];
  };
  readonly drilldowns?: {
    readonly context_path?: string;
    readonly coverage_path?: string;
    readonly stats_path?: string;
  };
  readonly infrastructure_overview?: {
    readonly artifact_family_counts?: Record<string, number>;
    readonly entity_type_counts?: Record<string, number>;
    readonly families?: readonly string[];
  };
  readonly limitations?: readonly string[];
  readonly repository?: StoryRepository;
  readonly semantic_overview?: {
    readonly entity_count?: number;
    readonly entity_type_counts?: Record<string, number>;
    readonly language_counts?: Record<string, number>;
  };
  readonly story_sections?: readonly StorySection[];
  readonly story?: string;
  readonly support_overview?: {
    readonly dependency_count?: number;
    readonly language_count?: number;
    readonly languages?: readonly string[];
    readonly topology_signal_count?: number;
  };
  readonly subject?: string | StorySubject;
}

export interface DeliveryPath {
  readonly artifact_family?: string;
  readonly artifact_type?: string;
  readonly delivery_command_families?: readonly string[];
  readonly environments?: readonly string[];
  readonly evidence_kind?: string;
  readonly kind?: string;
  readonly path?: string;
  readonly relative_path?: string;
  readonly signals?: readonly string[];
  readonly trigger_events?: readonly string[];
  readonly workflow_name?: string;
}

interface StoryRepository {
  readonly id?: string;
  readonly local_path?: string;
  readonly name?: string;
}

interface StorySubject {
  readonly id?: string;
  readonly name?: string;
  readonly type?: string;
}

interface StorySection {
  readonly summary?: string;
  readonly title?: string;
}

export interface ContextResponse {
  readonly consumers?: readonly ContextConsumer[];
  readonly consumer_repositories?: readonly ContextConsumer[];
  readonly dependency_count?: number;
  readonly deployment_evidence?: {
    readonly artifact_count?: number;
    readonly artifact_families?: readonly string[];
    readonly artifacts?: readonly DeploymentEvidenceArtifact[];
    readonly relationship_types?: readonly string[];
  };
  readonly file_count?: number;
  readonly infrastructure?: readonly InfrastructureItem[];
  readonly repository?: StoryRepository;
}

export interface ContextConsumer {
  readonly consumer_kinds?: readonly string[];
  readonly evidence_kinds?: readonly string[];
  readonly id?: string;
  readonly name?: string;
  readonly repo_name?: string;
  readonly repository?: string;
  readonly sample_paths?: readonly string[];
}

export interface DeploymentEvidenceArtifact {
  readonly artifact_family?: string;
  readonly confidence?: number;
  readonly direction?: string;
  readonly environment?: string;
  readonly evidence_kind?: string;
  readonly name?: string;
  readonly path?: string;
  readonly relationship_type?: string;
  readonly source_location?: {
    readonly path?: string;
    readonly repo_id?: string;
    readonly repo_name?: string;
  };
  readonly source_repo_name?: string;
  readonly target_repo_name?: string;
}

interface InfrastructureItem {
  readonly file_path?: string;
  readonly kind?: string;
  readonly name?: string;
  readonly type?: string;
}

export interface LoadWorkspaceStoryOptions {
  readonly client?: EshuApiClient;
  readonly entityId: string;
  readonly entityKind: EntityKind;
  readonly mode: ConsoleMode;
}

export async function loadWorkspaceStory({
  client,
  entityId,
  entityKind,
  mode
}: LoadWorkspaceStoryOptions): Promise<WorkspaceStory | null> {
  if (mode === "demo") {
    return getDemoWorkspaceStory(entityKind, entityId);
  }
  if (client === undefined) {
    throw new Error("Eshu API client is required outside demo mode");
  }

  const data = await client.getJson<StoryResponse>(storyPath(entityKind, entityId));
  const context = await loadContext(client, data, entityKind);
  const serviceContext = await loadRepositoryWorkloadContext(client, data, entityKind);
  const deploymentContext = serviceContext ?? context;
  return {
    deploymentGraph: deploymentGraphFromStory(data, deploymentContext),
    deploymentPath: deploymentPathFromStory(data),
    evidence: evidenceFromStory(data, deploymentContext),
    findings: [],
    id: entityId,
    kind: entityKind,
    limitations: data.limitations ?? [],
    overviewStats: overviewStatsFromStory(data, context),
    story: humanStory(data, deploymentContext, entityId),
    title: titleFromSubject(data.subject, entityId),
    truth: liveRepositoryTruth
  };
}

async function loadRepositoryWorkloadContext(
  client: EshuApiClient,
  story: StoryResponse,
  entityKind: EntityKind
): Promise<ContextResponse | undefined> {
  if (entityKind !== "repositories") {
    return undefined;
  }
  const workloadName = story.deployment_overview?.workloads?.[0];
  if (workloadName === undefined || workloadName.trim().length === 0) {
    return undefined;
  }
  try {
    return await client.getJson<ContextResponse>(
      `/api/v0/services/${encodeURIComponent(workloadName)}/context`
    );
  } catch {
    return undefined;
  }
}

async function loadContext(
  client: EshuApiClient,
  story: StoryResponse,
  entityKind: EntityKind
): Promise<ContextResponse | undefined> {
  if (entityKind !== "repositories") {
    return undefined;
  }
  const contextPath = story.drilldowns?.context_path;
  if (contextPath === undefined || contextPath.trim().length === 0) {
    return undefined;
  }
  try {
    return await client.getJson<ContextResponse>(contextPath);
  } catch {
    return undefined;
  }
}

function storyPath(entityKind: EntityKind, entityId: string): string {
  const escapedID = encodeURIComponent(entityId);
  switch (entityKind) {
    case "repositories":
      return `/api/v0/repositories/${escapedID}/story`;
    case "services":
    case "workloads":
      return `/api/v0/services/${escapedID}/story`;
  }
}

const liveRepositoryTruth: EshuTruth = {
  basis: "authoritative_graph",
  capability: "platform_impact.context_overview",
  freshness: { state: "fresh" },
  level: "exact",
  profile: "local_authoritative",
  reason: "loaded from the local Eshu HTTP API"
};

function titleFromSubject(subject: StoryResponse["subject"], fallback: string): string {
  if (typeof subject === "string" && subject.trim().length > 0) {
    return subject;
  }
  if (typeof subject === "object" && subject !== null) {
    return subject.name ?? subject.id ?? fallback;
  }
  return fallback;
}

function deploymentPathFromStory(story: StoryResponse): readonly string[] {
  const direct = story.deployment_overview?.direct_story ?? [];
  if (direct.length > 0) {
    return direct;
  }
  return story.deployment_overview?.topology_story ?? [];
}

function evidenceFromStory(
  story: StoryResponse,
  context: ContextResponse | undefined
): readonly EvidenceRow[] {
  const storyRows = (story.story_sections ?? []).map((section) => ({
    basis: "repository_story",
    category: section.title ?? "story",
    source: section.title ?? "story",
    summary: section.summary ?? "",
    title: evidenceTitle(section.title)
  }));
  return [...deploymentEvidenceRows(context), ...storyRows];
}

function deploymentEvidenceRows(context: ContextResponse | undefined): readonly EvidenceRow[] {
  const artifacts = context?.deployment_evidence?.artifacts ?? [];
  const grouped = new Map<string, DeploymentEvidenceArtifact[]>();
  for (const artifact of artifacts) {
    if (artifact.artifact_family !== "argocd" && artifact.artifact_family !== "helm") {
      continue;
    }
    const sourceRepo = nonEmpty(artifact.source_repo_name, artifact.source_location?.repo_name);
    const key = `${artifact.artifact_family}:${sourceRepo}:${artifact.relationship_type}`;
    grouped.set(key, [...(grouped.get(key) ?? []), artifact]);
  }
  const artifactRows = Array.from(grouped.values()).slice(0, 5).map((group) => {
    const sample = group[0];
    const sourceRepo = nonEmpty(sample.source_repo_name, sample.source_location?.repo_name);
    const path = nonEmpty(sample.source_location?.path, sample.path, sample.name);
    const family = sample.artifact_family ?? "deployment";
    return {
      basis: nonEmpty(sample.relationship_type, sample.evidence_kind, "deployment_evidence"),
      category: "deployment",
      detailPath: path,
      source: sourceRepo,
      summary: deploymentEvidenceSummary(family, sourceRepo, group.length, path),
      title: family === "argocd" ? "Deployed by ArgoCD" : "Deployed from Helm"
    };
  });
  if (artifactRows.length > 0) {
    return artifactRows;
  }
  return consumerEvidenceRows(context);
}

function consumerEvidenceRows(context: ContextResponse | undefined): readonly EvidenceRow[] {
  return deploymentConsumers(context).slice(0, 5).map((consumer) => {
    const sourceRepo = consumerName(consumer);
    const family = consumerFamily(consumer);
    const path = consumer.sample_paths?.[0] ?? "";
    return {
      basis: consumer.evidence_kinds?.[0] ?? "consumer_repository",
      category: "deployment",
      detailPath: path,
      source: sourceRepo,
      summary: deploymentEvidenceSummary(family, sourceRepo, consumer.sample_paths?.length ?? 1, path),
      title: family === "argocd" ? "Deployed by ArgoCD" : "Deployed from Helm"
    };
  });
}

function evidenceTitle(title: string | undefined): string {
  switch (title) {
    case "codebase":
      return "Codebase inventory";
    case "deployment":
      return "Deployment shape";
    case "relationships":
      return "Service relationships";
    case "semantics":
      return "Semantic inventory";
    case "support":
      return "Support surface";
    default:
      return title ?? "Evidence";
  }
}

function overviewStatsFromStory(
  story: StoryResponse,
  context: ContextResponse | undefined
): readonly OverviewStat[] {
  const files = fileCount(story, context);
  const workloadCount = story.deployment_overview?.workload_count ?? 0;
  const infraCount = context?.infrastructure?.length ??
    totalCount(story.infrastructure_overview?.entity_type_counts);
  const deploymentEvidence = context?.deployment_evidence?.artifact_count ??
    story.support_overview?.topology_signal_count ?? 0;
  return [
    {
      detail: "Indexed source and configuration files",
      label: "Files",
      value: String(files)
    },
    {
      detail: "Workloads Eshu associated with this repo",
      label: "Workloads",
      value: String(workloadCount)
    },
    {
      detail: "Helm, Kubernetes, Kustomize, Terraform, and ArgoCD objects",
      label: "Infra objects",
      value: String(infraCount)
    },
    {
      detail: "Deployment evidence artifacts from graph/context",
      label: "Deployment evidence",
      value: String(deploymentEvidence)
    }
  ];
}

function humanStory(story: StoryResponse, context: ContextResponse | undefined, fallback: string): string {
  if (typeof story.subject === "string" && story.story !== undefined) {
    return story.story;
  }
  const repoName = titleFromSubject(story.subject, story.repository?.name ?? fallback);
  const files = fileCount(story, context);
  const languages = story.support_overview?.languages ?? [];
  const infraFamilies = story.infrastructure_overview?.families ??
    story.deployment_overview?.infrastructure_families ?? [];
  const consumers = deploymentConsumers(context).map(consumerName).filter(isPresent);
  const parts = [`${repoName} is an indexed ${story.subject === undefined ? "entity" : "repository"}.`];
  if (files > 0) {
    parts.push(`${repoName} contains ${files} indexed files${languages.length > 0 ? ` across ${joinHuman(languages)}` : ""}.`);
  }
  if (infraFamilies.length > 0) {
    parts.push(`Eshu found ${joinHuman(infraFamilies)} infrastructure evidence.`);
  }
  if (consumers.length > 0) {
    const verb = consumers.length === 1 ? "references" : "reference";
    parts.push(`${joinHuman(consumers)} ${verb} it through deployment evidence.`);
  }
  return parts.join(" ");
}

function deploymentConsumers(context: ContextResponse | undefined): readonly ContextConsumer[] {
  const candidates = [...(context?.consumers ?? []), ...(context?.consumer_repositories ?? [])];
  return candidates.filter((consumer) => {
    const name = consumerName(consumer).toLowerCase();
    const paths = (consumer.sample_paths ?? []).join(" ").toLowerCase();
    return name.includes("argocd") ||
      name.includes("helm") ||
      paths.includes("applicationsets/") ||
      paths.includes("charts/");
  });
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

function fileCount(story: StoryResponse, context: ContextResponse | undefined): number {
  if (context?.file_count !== undefined) {
    return context.file_count;
  }
  const codebase = story.story_sections?.find((section) => section.title === "codebase")?.summary;
  const match = codebase?.match(/^(\d+)/);
  return match === undefined ? 0 : Number(match[1]);
}

function dedupeEvidence(rows: readonly EvidenceRow[]): readonly EvidenceRow[] {
  const seen = new Set<string>();
  return rows.filter((row) => {
    const key = `${row.title}:${row.source}:${row.detailPath}`;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function totalCount(counts: Record<string, number> | undefined): number {
  if (counts === undefined) {
    return 0;
  }
  return Object.values(counts).reduce((total, count) => total + count, 0);
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}

function deploymentEvidenceSummary(
  family: string,
  sourceRepo: string,
  count: number,
  path: string
): string {
  if (family === "argocd") {
    return `${sourceRepo} has ${count} ArgoCD ApplicationSet evidence item(s), including ${path}.`;
  }
  return `${sourceRepo} has ${count} Helm chart or values evidence item(s), including ${path}.`;
}

function isPresent(value: string | undefined): value is string {
  return value !== undefined && value.trim().length > 0;
}

function joinHuman(values: readonly string[]): string {
  if (values.length <= 2) {
    return values.join(" and ");
  }
  return `${values.slice(0, -1).join(", ")}, and ${values[values.length - 1]}`;
}
