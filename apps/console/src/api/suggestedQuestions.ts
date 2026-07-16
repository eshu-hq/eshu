import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";
import { loadRepositories } from "./repoCatalog";
import type { RepoListItem } from "./repoCatalog";

const GRAPH_SUMMARY_SOURCE = "POST /api/v0/ecosystem/graph-summary";
const GENERATION_SOURCE = "GET /api/v0/freshness/generations";
const CHANGED_SINCE_SOURCE = "GET /api/v0/freshness/changed-since";
const SUPPLY_CHAIN_SOURCE = "GET /api/v0/supply-chain/impact/findings";

export type SuggestedQuestionKind = "code" | "freshness" | "relationship" | "security";

export interface SuggestedQuestion {
  readonly href: string;
  readonly id: string;
  readonly kind: SuggestedQuestionKind;
  readonly question: string;
  readonly reason: string;
  readonly source: string;
}

export interface SuggestedQuestionsResult {
  readonly failures: readonly string[];
  readonly questions: readonly SuggestedQuestion[];
}

export interface SuggestedQuestionsOptions {
  readonly repositories?: readonly RepoListItem[];
  readonly repositoryCatalogUnavailable?: boolean;
}

interface GraphSummaryResponse {
  readonly hot_entities?: readonly HotEntityRecord[];
}

interface HotEntityRecord {
  readonly file_path?: string;
  readonly function_id?: string;
  readonly function_name?: string;
  readonly incoming_calls?: number;
  readonly outgoing_calls?: number;
  readonly total_degree?: number;
}

interface GenerationLifecycleResponse {
  readonly generations?: readonly GenerationRecord[];
}

interface GenerationRecord {
  readonly current_active_generation_id?: string;
  readonly generation_id?: string;
  readonly is_active?: boolean;
  readonly status?: string;
}

interface ChangedSinceResponse {
  readonly categories?: readonly ChangedSinceCategory[];
  readonly unavailable?: boolean;
}

interface ChangedSinceCategory {
  readonly counts?: Partial<Record<ChangedSinceClass, number>>;
  readonly name?: string;
}

type ChangedSinceClass = "added" | "retired" | "superseded" | "unchanged" | "updated";

interface ImpactFindingsResponse {
  readonly findings?: readonly ImpactFindingRecord[];
  readonly results?: readonly ImpactFindingRecord[];
}

interface ImpactFindingRecord {
  readonly advisory_id?: string;
  readonly cve_id?: string;
  readonly cvss?: number;
  readonly cvss_score?: number;
  readonly id?: string;
  readonly package?: string;
  readonly package_name?: string;
  readonly repository_id?: string;
  readonly selected_severity_label?: string;
  readonly service_ids?: readonly string[];
  readonly severity?: string;
  readonly workload_ids?: readonly string[];
}

/**
 * Loads suggested questions from first-class bounded query endpoints. Repository
 * scoped endpoints are skipped until a real repository scope is available.
 */
export async function loadSourceBackedSuggestedQuestions(
  client: EshuApiClient,
): Promise<readonly SuggestedQuestion[]> {
  return (await loadSourceBackedSuggestedQuestionsResult(client)).questions;
}

export async function loadSourceBackedSuggestedQuestionsResult(
  client: EshuApiClient,
  options: SuggestedQuestionsOptions = {},
): Promise<SuggestedQuestionsResult> {
  const questions: SuggestedQuestion[] = [];
  const failures: string[] = [];
  const security = settleQuestion("vulnerability impact", loadSecurityQuestion(client));
  let repository: RepoListItem | undefined;
  try {
    const repositories = options.repositories ?? (await loadRepositories(client));
    repository = repositories.find((row) => row.id.trim().length > 0);
    if (options.repositoryCatalogUnavailable === true) failures.push("repository catalog");
  } catch {
    failures.push("repository catalog");
  }
  const repoQuestions =
    repository === undefined
      ? []
      : await Promise.all([
          settleQuestion("graph summary", loadHotEntityQuestion(client, repository)),
          settleQuestion("changed-since", loadChangedSinceQuestion(client, repository)),
        ]);
  for (const result of [...repoQuestions, await security]) {
    if (result.question) questions.push(result.question);
    if (result.failure) failures.push(result.failure);
  }
  return { failures, questions };
}

async function settleQuestion(
  failure: string,
  promise: Promise<SuggestedQuestion | null>,
): Promise<{ readonly failure: string; readonly question: SuggestedQuestion | null }> {
  try {
    return { failure: "", question: await promise };
  } catch {
    return { failure, question: null };
  }
}

async function loadHotEntityQuestion(
  client: EshuApiClient,
  repository: RepoListItem,
): Promise<SuggestedQuestion | null> {
  const env = await client.post<GraphSummaryResponse>("/api/v0/ecosystem/graph-summary", {
    limit: 3,
    repo_id: repository.id,
  });
  if (env.error) throw new EshuEnvelopeError(env.error);
  const entity = env.data?.hot_entities?.find((row) => clean(row.function_name).length > 0);
  if (entity === undefined) return null;
  const name = clean(entity.function_name);
  const degree = entity.total_degree ?? (entity.incoming_calls ?? 0) + (entity.outgoing_calls ?? 0);
  const filePath = clean(entity.file_path);
  return {
    href: `/explorer?q=${encodeURIComponent(name)}`,
    id: `hot:${clean(entity.function_id) || name}`,
    kind: "relationship",
    question: `Why is ${name} a hot graph entity?`,
    reason: `${degree} call relationship${degree === 1 ? "" : "s"} in ${filePath || repository.name}.`,
    source: GRAPH_SUMMARY_SOURCE,
  };
}

async function loadChangedSinceQuestion(
  client: EshuApiClient,
  repository: RepoListItem,
): Promise<SuggestedQuestion | null> {
  const priorGeneration = await loadPriorGeneration(client, repository.id);
  if (priorGeneration === null) return null;
  const env = await client.get<ChangedSinceResponse>(
    `/api/v0/freshness/changed-since?repository=${encodeURIComponent(repository.id)}&since_generation_id=${encodeURIComponent(priorGeneration)}&sample_limit=5`,
  );
  if (env.error) throw new EshuEnvelopeError(env.error);
  if (env.data?.unavailable === true) throw new Error("changed-since suggestions are unavailable");
  const total = changedCount(env.data?.categories ?? []);
  if (total === 0) return null;
  const routeParams = new URLSearchParams({
    mode: "repository",
    repository: repository.id,
    since_generation_id: priorGeneration,
  });
  return {
    href: `/changed-since?${routeParams.toString()}`,
    id: `changed-since:${repository.id}:${priorGeneration}`,
    kind: "freshness",
    question: `What changed in ${repository.name} since the prior generation?`,
    reason: `${total} added, updated, retired, or superseded evidence key${total === 1 ? "" : "s"}.`,
    source: `${GENERATION_SOURCE} -> ${CHANGED_SINCE_SOURCE}`,
  };
}

async function loadPriorGeneration(
  client: EshuApiClient,
  repositoryID: string,
): Promise<string | null> {
  const env = await client.get<GenerationLifecycleResponse>(
    `/api/v0/freshness/generations?repository=${encodeURIComponent(repositoryID)}&limit=3`,
  );
  if (env.error) throw new EshuEnvelopeError(env.error);
  const generations = env.data?.generations ?? [];
  const activeID = clean(
    generations.find((row) => row.is_active === true)?.generation_id,
    generations.find((row) => clean(row.current_active_generation_id).length > 0)
      ?.current_active_generation_id,
  );
  if (activeID.length === 0) return null;
  const prior = generations.find(
    (row) =>
      clean(row.generation_id).length > 0 &&
      clean(row.generation_id) !== activeID &&
      row.is_active !== true &&
      row.status !== "pending",
  );
  return clean(prior?.generation_id).length === 0 ? null : clean(prior?.generation_id);
}

async function loadSecurityQuestion(client: EshuApiClient): Promise<SuggestedQuestion | null> {
  const critical = await loadImpactFindingBySeverity(client, "critical");
  const finding = critical ?? (await loadImpactFindingBySeverity(client, "high"));
  if (finding === null) return null;
  const advisoryID = clean(finding.advisory_id, finding.cve_id, finding.id);
  if (advisoryID.length === 0) return null;
  const packageName = clean(finding.package_name, finding.package) || "the affected package";
  const severity = clean(finding.severity, finding.selected_severity_label) || "high";
  const cvss = finding.cvss_score ?? finding.cvss ?? 0;
  const affected = [...(finding.service_ids ?? []), ...(finding.workload_ids ?? [])]
    .map((value) => clean(value))
    .filter((value) => value.length > 0);
  const affectedLabel =
    affected.length === 0
      ? clean(finding.repository_id) || "indexed services"
      : affected.slice(0, 2).join(", ");
  return {
    href: `/vulnerabilities/${encodeURIComponent(advisoryID)}`,
    id: `vulnerability:${advisoryID}`,
    kind: "security",
    question: `Which services are exposed to ${advisoryID} through ${packageName}?`,
    reason: `${titleCase(severity)} impact finding${cvss > 0 ? ` with CVSS ${cvss}` : ""} affecting ${affectedLabel}.`,
    source: SUPPLY_CHAIN_SOURCE,
  };
}

async function loadImpactFindingBySeverity(
  client: EshuApiClient,
  severity: "critical" | "high",
): Promise<ImpactFindingRecord | null> {
  const env = await client.get<ImpactFindingsResponse>(
    `/api/v0/supply-chain/impact/findings?limit=5&severity=${severity}`,
  );
  if (env.error) throw new EshuEnvelopeError(env.error);
  return (env.data?.findings ?? env.data?.results ?? [])[0] ?? null;
}

function changedCount(categories: readonly ChangedSinceCategory[]): number {
  return categories.reduce((total, category) => {
    const counts = category.counts ?? {};
    return (
      total +
      (counts.added ?? 0) +
      (counts.updated ?? 0) +
      (counts.retired ?? 0) +
      (counts.superseded ?? 0)
    );
  }, 0);
}

function clean(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value.trim();
    }
  }
  return "";
}

function titleCase(value: string): string {
  const normalized = value.toLowerCase();
  return `${normalized.slice(0, 1).toUpperCase()}${normalized.slice(1)}`;
}
