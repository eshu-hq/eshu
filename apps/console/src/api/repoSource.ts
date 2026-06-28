// api/repoSource.ts
// Repository source browsing loaders, wired to the merged content endpoints:
//   GET /api/v0/repositories/{id}/tree?path=     (#1431)
//   GET /api/v0/repositories/{id}/content?path=  (#1432)
//   GET /api/v0/repositories/{id}/branches       (#1433)
// Defensive over response shape; never fabricates files or branch names.

import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";

export interface TreeEntry {
  readonly name: string;
  readonly type: "dir" | "file";
  readonly path: string;
  readonly size: number | null;
  readonly language: string | null;
  readonly childCount: number | null;
}

export interface RepoTree {
  readonly ref: string;
  readonly path: string;
  readonly entries: readonly TreeEntry[];
  readonly truncated: boolean;
}

export interface RepoFile {
  readonly path: string;
  readonly ref: string;
  readonly encoding: "utf-8" | "base64";
  readonly content: string;
  readonly size: number;
  readonly language: string | null;
  readonly truncated: boolean;
  readonly provenance: "live" | "unavailable";
}

export interface RepoBranch {
  readonly name: string;
  readonly headSha: string;
  readonly lastIndexedAt: string | null;
}

export interface RepoBranches {
  readonly defaultBranch: string;
  readonly branches: readonly RepoBranch[];
}

interface TreeResponse {
  readonly ref?: string;
  readonly path?: string;
  readonly truncated?: boolean;
  readonly entries?: ReadonlyArray<{
    name?: string;
    type?: string;
    path?: string;
    size?: number;
    language?: string;
    child_count?: number;
  }>;
}

interface BranchesResponse {
  readonly default_branch?: string;
  readonly branches?: ReadonlyArray<{
    readonly name?: string;
    readonly head_sha?: string;
    readonly last_indexed_at?: string;
  }>;
}

function num(v: unknown): number | null {
  return typeof v === "number" && Number.isFinite(v) ? v : null;
}
function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

export async function loadRepoBranches(client: EshuApiClient, id: string): Promise<RepoBranches> {
  const env = await client.get<BranchesResponse>(
    `/api/v0/repositories/${encodeURIComponent(id)}/branches`,
  );
  if (env.error) throw new EshuEnvelopeError(env.error);
  const data = env.data ?? {};
  const branches: RepoBranch[] = (data.branches ?? [])
    .map(
      (branch): RepoBranch => ({
        name: str(branch.name),
        headSha: str(branch.head_sha),
        lastIndexedAt: branch.last_indexed_at ? str(branch.last_indexed_at) : null,
      }),
    )
    .filter((branch) => branch.name !== "" || branch.headSha !== "");
  return { defaultBranch: str(data.default_branch), branches };
}

export async function loadRepoTree(
  client: EshuApiClient,
  id: string,
  path = "",
  ref = "",
  language = "",
): Promise<RepoTree> {
  const qs = repoSourceQuery({ path, ref, language });
  const env = await client.get<TreeResponse>(
    `/api/v0/repositories/${encodeURIComponent(id)}/tree${qs}`,
  );
  if (env.error) throw new EshuEnvelopeError(env.error);
  const data = env.data ?? {};
  const entries: TreeEntry[] = (data.entries ?? [])
    .map(
      (e): TreeEntry => ({
        name: str(e.name),
        type: e.type === "dir" ? "dir" : "file",
        path: str(e.path),
        size: num(e.size),
        language: e.language ? str(e.language) : null,
        childCount: num(e.child_count),
      }),
    )
    .filter((e) => e.name !== "");
  return { ref: str(data.ref), path: str(data.path), entries, truncated: data.truncated === true };
}

interface ContentResponse {
  readonly path?: string;
  readonly ref?: string;
  readonly encoding?: string;
  readonly content?: string;
  readonly size?: number;
  readonly language?: string;
  readonly truncated?: boolean;
}

export async function loadRepoFile(
  client: EshuApiClient,
  id: string,
  path: string,
  ref = "",
): Promise<RepoFile> {
  try {
    const env = await client.get<ContentResponse>(
      `/api/v0/repositories/${encodeURIComponent(id)}/content${repoSourceQuery({ path, ref })}`,
    );
    const d = env.data ?? {};
    return {
      path: str(d.path) || path,
      ref: str(d.ref),
      encoding: d.encoding === "base64" ? "base64" : "utf-8",
      content: str(d.content),
      size: num(d.size) ?? 0,
      language: d.language ? str(d.language) : null,
      truncated: d.truncated === true,
      provenance: env.data ? "live" : "unavailable",
    };
  } catch {
    return {
      path,
      ref: "",
      encoding: "utf-8",
      content: "",
      size: 0,
      language: null,
      truncated: false,
      provenance: "unavailable",
    };
  }
}

function repoSourceQuery(values: {
  readonly path?: string;
  readonly ref?: string;
  readonly language?: string;
}): string {
  const params = new URLSearchParams();
  const path = values.path?.trim() ?? "";
  const ref = values.ref?.trim() ?? "";
  const language = values.language?.trim() ?? "";
  if (path !== "") params.set("path", path);
  if (ref !== "") params.set("ref", ref);
  if (language !== "") params.set("language", language);
  const query = params.toString();
  return query === "" ? "" : `?${query}`;
}

// decodeRepoFile returns displayable text. base64 (non-UTF-8) content is decoded
// to a binary marker rather than rendered as garbled text.
export function decodeRepoFile(file: RepoFile): { text: string; binary: boolean } {
  if (file.encoding === "base64") return { text: "", binary: true };
  return { text: file.content, binary: false };
}
