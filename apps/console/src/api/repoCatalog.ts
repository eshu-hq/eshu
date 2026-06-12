// api/repoCatalog.ts
// Repository browser loaders. Repo list comes from GET /api/v0/repositories;
// per-repo detail from GET /api/v0/repositories/{id}/stats and /story. Defensive
// over response shape (see GET /api/v0/openapi.json); never fabricates counts.

import type { EshuApiClient } from "./client";

export interface RepoListItem {
  readonly id: string;
  readonly name: string;
  readonly repoSlug: string;
  readonly remoteUrl: string;
  readonly isDependency: boolean;
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
}
interface RepoListResponse { readonly repositories?: readonly RepoRecord[]; }

function str(v: unknown): string { return typeof v === "string" ? v : ""; }

export async function loadRepositories(client: EshuApiClient): Promise<readonly RepoListItem[]> {
  const env = await client.get<RepoListResponse>("/api/v0/repositories?limit=500&offset=0");
  return (env.data?.repositories ?? []).map((r) => ({
    id: str(r.id) || str(r.name),
    name: str(r.name) || str(r.id),
    repoSlug: str(r.repo_slug),
    remoteUrl: str(r.remote_url),
    isDependency: r.is_dependency === true
  })).filter((r) => r.id !== "");
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
