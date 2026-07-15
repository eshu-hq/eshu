import type { EshuApiClient } from "./client";
import {
  loadDeploymentConfigInfluence,
  type DeploymentConfigInfluence,
} from "./deploymentConfigInfluence";
import { deploymentGraphFromStory } from "./deploymentGraph";
import type { EshuTruth } from "./envelope";
import { getDemoWorkspaceStory } from "./mockData";
import type { EntityKind, EvidenceRow, OverviewStat, WorkspaceStory } from "./mockData";
import {
  deploymentArtifactDrilldown,
  drilldownForStorySection,
} from "./repositoryEvidenceDrilldown";
import { deploymentEvidenceSummary, isPresent, joinHuman, nonEmpty } from "./repositoryText";
import type {
  ContextConsumer,
  ContextResponse,
  DeploymentEvidenceArtifact,
  LoadWorkspaceStoryOptions,
  StoryResponse,
} from "./repositoryTypes";
export type {
  ContextConsumer,
  ContextResponse,
  DeliveryPath,
  DeploymentEvidenceArtifact,
  LoadWorkspaceStoryOptions,
  StoryResponse,
  StorySection,
} from "./repositoryTypes";
import { serviceSpotlightFromContext } from "./serviceSpotlight";
import type { ServiceContextResponse } from "./serviceSpotlight";
import {
  serviceContextFromStoryDossier,
  type ServiceStoryDossierResponse,
} from "./serviceStoryDossier";

type WorkspaceContextResponse = ContextResponse & ServiceContextResponse;

export async function loadWorkspaceStory({
  client,
  entityId,
  entityKind,
  mode,
}: LoadWorkspaceStoryOptions): Promise<WorkspaceStory | null> {
  if (mode === "demo") {
    return getDemoWorkspaceStory(entityKind, entityId);
  }
  if (client === undefined) {
    throw new Error("Eshu API client is required outside demo mode");
  }

  const data = await client.getJson<StoryResponse>(storyPath(entityKind, entityId));
  const context = await loadContext(client, data, entityKind);
  const serviceContext = await loadServiceContext(client, data, entityKind, entityId);
  const configInfluence = await loadDeploymentConfigInfluenceForStory(
    client,
    data,
    entityId,
    serviceContext,
  );
  const deploymentContext = serviceContext ?? context;
  const title = titleFromStory(data, entityId);
  return {
    deploymentGraph: deploymentGraphFromStory(data, deploymentContext),
    deploymentPath: deploymentPathFromStory(data),
    evidence: evidenceFromStory(data, deploymentContext),
    findings: [],
    id: entityId,
    kind: entityKind,
    limitations: data.limitations ?? [],
    overviewStats: overviewStatsFromStory(data, deploymentContext),
    serviceSpotlight:
      serviceContext === undefined
        ? undefined
        : serviceSpotlightFromContext(
            serviceContext,
            data.deployment_overview?.workloads?.[0] ?? title,
            configInfluence,
          ),
    story: humanStory(data, deploymentContext, title, entityKind),
    title,
    truth: liveRepositoryTruth,
  };
}

async function loadDeploymentConfigInfluenceForStory(
  client: EshuApiClient,
  story: StoryResponse,
  entityId: string,
  serviceContext: WorkspaceContextResponse | undefined,
): Promise<DeploymentConfigInfluence | undefined> {
  if (serviceContext === undefined) {
    return undefined;
  }
  const serviceName = nonEmpty(serviceContext.name, serviceNameFromStory(story, entityId));
  if (serviceName.length === 0) {
    return undefined;
  }
  try {
    return await loadDeploymentConfigInfluence(client, { serviceName });
  } catch {
    return undefined;
  }
}

async function loadServiceContext(
  client: EshuApiClient,
  story: StoryResponse,
  entityKind: EntityKind,
  entityId: string,
): Promise<WorkspaceContextResponse | undefined> {
  if (entityKind === "services" || entityKind === "workloads") {
    return serviceContextFromStoryDossier(
      story as ServiceStoryDossierResponse,
      serviceNameFromStory(story, entityId),
    );
  }
  return loadRepositoryWorkloadContext(client, story, entityKind);
}

async function loadRepositoryWorkloadContext(
  client: EshuApiClient,
  story: StoryResponse,
  entityKind: EntityKind,
): Promise<WorkspaceContextResponse | undefined> {
  if (entityKind !== "repositories") {
    return undefined;
  }
  const workloadName = repositoryServiceSelector(story);
  if (workloadName.length === 0) {
    return undefined;
  }
  try {
    const dossier = await client.getJson<ServiceStoryDossierResponse>(
      `/api/v0/services/${encodeURIComponent(workloadName)}/story`,
    );
    return serviceContextFromStoryDossier(dossier, workloadName);
  } catch {
    try {
      return await client.getJson<WorkspaceContextResponse>(
        `/api/v0/services/${encodeURIComponent(workloadName)}/context`,
      );
    } catch {
      return undefined;
    }
  }
}

function repositoryServiceSelector(story: StoryResponse): string {
  const workloads = story.deployment_overview?.workloads ?? [];
  return (
    workloads
      .map((workload) => nonEmpty(workload))
      .find(
        (workload) =>
          workload.length > 0 &&
          !(workload.startsWith("reducer_") && workload.includes("_workload_identity_workload_")),
      ) ?? ""
  );
}

async function loadContext(
  client: EshuApiClient,
  story: StoryResponse,
  entityKind: EntityKind,
): Promise<WorkspaceContextResponse | undefined> {
  if (entityKind !== "repositories") {
    return undefined;
  }
  const contextPath = story.drilldowns?.context_path;
  if (contextPath === undefined || contextPath.trim().length === 0) {
    return undefined;
  }
  try {
    return await client.getJson<WorkspaceContextResponse>(contextPath);
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
  reason: "loaded from the local Eshu HTTP API",
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
function titleFromStory(story: StoryResponse, fallback: string): string {
  return nonEmpty(
    story.service_identity?.service_name,
    story.service_name,
    titleFromSubject(story.subject, story.repository?.name ?? fallback),
  );
}

function serviceNameFromStory(story: StoryResponse, fallback: string): string {
  return nonEmpty(story.service_identity?.service_name, story.service_name, fallback);
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
  context: ContextResponse | undefined,
): readonly EvidenceRow[] {
  const storyRows = (story.story_sections ?? []).map((section) => ({
    basis: "repository_story",
    category: section.title ?? "story",
    drilldown: drilldownForStorySection(section, context),
    source: section.title ?? "story",
    summary: section.summary ?? "",
    title: evidenceTitle(section.title),
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
  const artifactRows = Array.from(grouped.values())
    .slice(0, 5)
    .map((group) => {
      const sample = group[0];
      const sourceRepo = nonEmpty(sample.source_repo_name, sample.source_location?.repo_name);
      const path = nonEmpty(sample.source_location?.path, sample.path, sample.name);
      const family = sample.artifact_family ?? "deployment";
      return {
        basis: nonEmpty(sample.relationship_type, sample.evidence_kind, "deployment_evidence"),
        category: "deployment",
        detailPath: path,
        drilldown: deploymentArtifactDrilldown(family, group),
        source: sourceRepo,
        summary: deploymentEvidenceSummary(family, sourceRepo, group.length, path),
        title: family === "argocd" ? "Deployed by ArgoCD" : "Deployed from Helm",
      };
    });
  if (artifactRows.length > 0) {
    return artifactRows;
  }
  return consumerEvidenceRows(context);
}

function consumerEvidenceRows(context: ContextResponse | undefined): readonly EvidenceRow[] {
  return deploymentConsumers(context)
    .slice(0, 5)
    .map((consumer) => {
      const sourceRepo = consumerName(consumer);
      const family = consumerFamily(consumer);
      const path = consumer.sample_paths?.[0] ?? "";
      return {
        basis: consumer.evidence_kinds?.[0] ?? "consumer_repository",
        category: "deployment",
        detailPath: path,
        source: sourceRepo,
        summary: deploymentEvidenceSummary(
          family,
          sourceRepo,
          consumer.sample_paths?.length ?? 1,
          path,
        ),
        title: family === "argocd" ? "Deployed by ArgoCD" : "Deployed from Helm",
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
  context: ContextResponse | undefined,
): readonly OverviewStat[] {
  const files = fileCount(story, context);
  const workloadCount = story.deployment_overview?.workload_count ?? 0;
  const infraCount =
    context?.infrastructure?.length ??
    totalCount(story.infrastructure_overview?.entity_type_counts);
  const deploymentEvidence =
    context?.deployment_evidence?.artifact_count ??
    story.support_overview?.topology_signal_count ??
    0;
  return [
    {
      detail: "Indexed source and configuration files",
      label: "Files",
      value: String(files),
    },
    {
      detail: "Workloads Eshu associated with this repo",
      label: "Workloads",
      value: String(workloadCount),
    },
    {
      detail: "Helm, Kubernetes, Kustomize, Terraform, and ArgoCD objects",
      label: "Infra objects",
      value: String(infraCount),
    },
    {
      detail: "Deployment evidence artifacts from graph/context",
      label: "Deployment evidence",
      value: String(deploymentEvidence),
    },
  ];
}

function humanStory(
  story: StoryResponse,
  context: ContextResponse | undefined,
  fallback: string,
  entityKind: EntityKind,
): string {
  if (entityKind !== "repositories" && story.story !== undefined && story.story.trim().length > 0) {
    return story.story;
  }
  const repoName = titleFromSubject(story.subject, story.repository?.name ?? fallback);
  const files = fileCount(story, context);
  const languages = story.support_overview?.languages ?? [];
  const infraFamilies =
    story.infrastructure_overview?.families ??
    story.deployment_overview?.infrastructure_families ??
    [];
  const consumers = deploymentConsumers(context).map(consumerName).filter(isPresent);
  const parts = [
    `${repoName} is an indexed ${story.subject === undefined ? "entity" : "repository"}.`,
  ];
  if (files > 0) {
    parts.push(
      `${repoName} contains ${files} indexed files${languages.length > 0 ? ` across ${joinHuman(languages)}` : ""}.`,
    );
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
    return (
      name.includes("argocd") ||
      name.includes("helm") ||
      paths.includes("applicationsets/") ||
      paths.includes("charts/")
    );
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
  return match ? Number(match[1]) : 0;
}

function totalCount(counts: Record<string, number> | undefined): number {
  if (counts === undefined) {
    return 0;
  }
  return Object.values(counts).reduce((total, count) => total + count, 0);
}
