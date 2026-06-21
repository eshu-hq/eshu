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
  readonly offset?: number;
}

// API max page size for GET /api/v0/repositories (repositoryListMaxLimit in the
// query handler). Larger values are clamped server-side, so request exactly the
// cap to page through a large stack in the fewest round trips.
const REPOSITORY_PAGE_LIMIT = 500;

// Hard ceiling on pages. The server clamps offset at repositoryListMaxOffset
// (10000), so at most 10000/500 = 20 advancing pages exist before the offset
// stalls. 24 gives 4 pages of headroom over that real bound; anything beyond
// is a misbehaving API and will be caught by the offset-stall break first.
const REPOSITORY_MAX_PAGES = 24;

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
// still stop. Three safety rails prevent silent corruption on a misbehaving API:
//   1. REPOSITORY_MAX_PAGES caps total iterations.
//   2. An offset-stall break stops the loop when the server-echoed offset does
//      not advance (the server clamps offset at 10000, so without this guard
//      subsequent pages would overwrite the same window and accumulate duplicates).
//   3. A console.warn fires when the page cap is reached so operators can tell
//      the list is incomplete rather than discovering a silent wrong count.
export async function loadRepositories(client: EshuApiClient): Promise<readonly RepoListItem[]> {
  const items: RepoListItem[] = [];
  let offset = 0;
  let page = 0;
  for (; page < REPOSITORY_MAX_PAGES; page += 1) {
    const env = await client.get<RepoListResponse>(`/api/v0/repositories?limit=${REPOSITORY_PAGE_LIMIT}&offset=${offset}`);
    if (env.error) throw new EshuEnvelopeError(env.error);
    // Offset-stall guard: check BEFORE appending rows. The server echoes the
    // offset it actually applied after server-side clamping
    // (repositoryListMaxOffset = 10000). If the echoed offset does not match
    // what we requested, the server clamped us — appending this page would
    // duplicate rows already collected from the last un-clamped page.
    const echoedOffset = env.data?.offset;
    if (typeof echoedOffset === "number" && echoedOffset !== offset) break;
    const wire = env.data?.repositories ?? [];
    for (const record of wire) {
      const item = repoListItem(record);
      if (item.id !== "") items.push(item);
    }
    // An empty page is always terminal — truncated:true with zero rows is
    // contradictory and must not cause another fetch.
    if (wire.length === 0) break;
    // truncated is the authoritative paging signal. When the API omits it
    // (older/fixture shape), a full page is the only hint that more may exist;
    // a short page is terminal.
    const truncated = env.data?.truncated;
    const morePages = truncated === undefined ? wire.length === REPOSITORY_PAGE_LIMIT : truncated;
    if (!morePages) break;
    offset += REPOSITORY_PAGE_LIMIT;
  }
  if (page === REPOSITORY_MAX_PAGES) {
    // Surface the incomplete list so operators see it rather than silently
    // trusting a wrong count. This mirrors how the console surfaces load issues.
    console.warn(
      `loadRepositories: reached page limit (${REPOSITORY_MAX_PAGES} pages × ${REPOSITORY_PAGE_LIMIT} rows). ` +
      `Repository list may be incomplete.`
    );
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
