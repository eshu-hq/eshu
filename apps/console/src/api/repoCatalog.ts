// api/repoCatalog.ts
// Repository browser loaders. Repo list comes from GET /api/v0/repositories;
// per-repo detail from GET /api/v0/repositories/{id}/stats and /story. Defensive
// over response shape (see GET /api/v0/openapi.json); never fabricates counts.

import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";

export interface RepoListItem {
  readonly id: string;
  readonly name: string;
  readonly repoSlug: string;
  readonly remoteUrl: string;
  readonly isDependency: boolean;
  readonly groupKey: string;
  readonly groupSource: string;
  readonly groupTruth: string;
  readonly groupKind: string;
  readonly groupReason: string;
}

export interface RepoStats {
  readonly fileCount: number | null;
  readonly entityCount: number | null;
  readonly languages: readonly string[];
  readonly entityTypes: readonly string[];
  readonly coverageState: string;
}

export interface RepoDetail {
  readonly id: string;
  readonly name: string;
  readonly stats: RepoStats;
  readonly highlights: readonly string[];
  readonly provenance: "live" | "empty" | "unavailable";
}

interface RepoRecord {
  readonly id?: string; readonly name?: string; readonly repo_slug?: string;
  readonly remote_url?: string; readonly is_dependency?: boolean;
  readonly group_key?: string; readonly group_source?: string; readonly group_truth?: string;
  readonly group_kind?: string; readonly group_reason?: string;
}
interface RepoListResponse {
  readonly repositories?: readonly RepoRecord[];
  readonly truncated?: boolean;
}

// API max page size for GET /api/v0/repositories (repositoryListMaxLimit in the
// query handler). Larger values are clamped server-side, so request exactly the
// cap to page through a large stack in the fewest round trips.
const REPOSITORY_PAGE_LIMIT = 500;

// Hard ceiling on pages so a misbehaving API that always reports truncated can
// never spin forever. The route caps offset at 10000, so 64 pages of 500 covers
// every reachable repository (32k) with headroom.
const REPOSITORY_MAX_PAGES = 64;

function str(v: unknown): string { return typeof v === "string" ? v : ""; }

function repoSlugLeaf(slug: string): string {
  const parts = slug.split(/[\\/]/).filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] ?? "" : "";
}

function isOpaqueRepositoryId(value: string): boolean {
  return value.startsWith("repository:");
}

function repoDisplayName(repo: RepoRecord): string {
  const name = str(repo.name);
  if (name !== "" && !isOpaqueRepositoryId(name)) return name;
  return repoSlugLeaf(str(repo.repo_slug)) || name || str(repo.id);
}

function repoListItem(r: RepoRecord): RepoListItem {
  return {
    id: str(r.id) || str(r.name),
    name: repoDisplayName(r),
    repoSlug: str(r.repo_slug),
    remoteUrl: str(r.remote_url),
    isDependency: r.is_dependency === true,
    groupKey: str(r.group_key),
    groupSource: str(r.group_source),
    groupTruth: str(r.group_truth),
    groupKind: str(r.group_kind),
    groupReason: str(r.group_reason)
  };
}

// loadRepositories pages through GET /api/v0/repositories until the API stops
// reporting more repositories. The route caps a page at 500 rows and signals
// more pages with `truncated=true`, so a single fetch silently dropped every
// repository beyond the first page on large stacks (issue #3376). Paging makes
// the returned list the true total, which the Repositories page then counts
// honestly instead of showing a single-page slice. A short page (fewer rows than
// the page limit) is also treated as terminal so callers that omit `truncated`
// still stop, and never fabricates rows the API did not return.
export async function loadRepositories(client: EshuApiClient): Promise<readonly RepoListItem[]> {
  const items: RepoListItem[] = [];
  let offset = 0;
  for (let page = 0; page < REPOSITORY_MAX_PAGES; page += 1) {
    const env = await client.get<RepoListResponse>(`/api/v0/repositories?limit=${REPOSITORY_PAGE_LIMIT}&offset=${offset}`);
    if (env.error) throw new EshuEnvelopeError(env.error);
    const wire = env.data?.repositories ?? [];
    for (const record of wire) {
      const item = repoListItem(record);
      if (item.id !== "") items.push(item);
    }
    // truncated is the authoritative paging signal. When the API omits it
    // (older/fixture shape), a full page is the only hint that more may exist;
    // a short page is terminal.
    const truncated = env.data?.truncated;
    const morePages = truncated === undefined ? wire.length === REPOSITORY_PAGE_LIMIT : truncated;
    if (!morePages || wire.length === 0) break;
    offset += REPOSITORY_PAGE_LIMIT;
  }
  return items;
}

export async function loadRepositoryNameMap(client: EshuApiClient): Promise<ReadonlyMap<string, string>> {
  const repos = await loadRepositories(client);
  return new Map(repos.map((repo) => [repo.id, repo.name]));
}

interface StatsResponse {
  readonly repository?: { id?: string; name?: string };
  readonly file_count?: number | null;
  readonly entity_count?: number | null;
  readonly languages?: readonly string[];
  readonly entity_types?: readonly string[];
  readonly coverage?: { source_backend?: string };
}
interface StoryResponse {
  readonly repository?: { id?: string; name?: string };
  readonly highlights?: readonly unknown[];
  readonly sections?: readonly { title?: string }[];
}

function num(v: unknown): number | null {
  return typeof v === "number" && Number.isFinite(v) ? v : null;
}

export async function loadRepositoryDetail(client: EshuApiClient, id: string): Promise<RepoDetail> {
  try {
    const statsEnv = await client.get<StatsResponse>(`/api/v0/repositories/${encodeURIComponent(id)}/stats`);
    if (statsEnv.error) throw new EshuEnvelopeError(statsEnv.error);
    const s = statsEnv.data ?? {};
    let highlights: string[] = [];
    try {
      const storyEnv = await client.get<StoryResponse>(`/api/v0/repositories/${encodeURIComponent(id)}/story`);
      const raw = storyEnv.data?.highlights ?? storyEnv.data?.sections ?? [];
      highlights = raw.map((h) => typeof h === "string" ? h : str((h as { title?: string })?.title)).filter(Boolean);
    } catch { /* story optional */ }
    return {
      id,
      name: s.repository?.name ?? id,
      stats: {
        fileCount: num(s.file_count),
        entityCount: num(s.entity_count),
        languages: s.languages ?? [],
        entityTypes: s.entity_types ?? [],
        coverageState: s.coverage?.source_backend ?? "unavailable"
      },
      highlights,
      provenance: statsEnv.data ? "live" : "empty"
    };
  } catch {
    return {
      id, name: id,
      stats: { fileCount: null, entityCount: null, languages: [], entityTypes: [], coverageState: "unavailable" },
      highlights: [], provenance: "unavailable"
    };
  }
}
