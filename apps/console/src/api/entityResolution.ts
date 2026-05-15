import type { EshuApiClient } from "./client";

export interface EntityResolutionCandidate {
  readonly filePath: string;
  readonly id: string;
  readonly labels: readonly string[];
  readonly name: string;
  readonly repoId: string;
  readonly repoName: string;
  readonly type: string;
}

export interface EntityResolutionResult {
  readonly candidates: readonly EntityResolutionCandidate[];
  readonly count: number;
  readonly limit: number;
  readonly truncated: boolean;
}

export interface ResolveEntityOptions {
  readonly client: EshuApiClient;
  readonly limit?: number;
  readonly name: string;
  readonly repoId?: string;
  readonly type?: string;
}

interface ResolveEntityResponse {
  readonly count?: unknown;
  readonly entities?: unknown;
  readonly limit?: unknown;
  readonly matches?: unknown;
  readonly truncated?: unknown;
}

export async function resolveEntity({
  client,
  limit = 10,
  name,
  repoId,
  type
}: ResolveEntityOptions): Promise<EntityResolutionResult> {
  const response = await client.postJson<ResolveEntityResponse>(
    "/api/v0/entities/resolve",
    compactBody({ limit, name, repo_id: repoId, type })
  );
  const rawCandidates = Array.isArray(response.entities)
    ? response.entities
    : Array.isArray(response.matches)
      ? response.matches
      : [];

  return {
    candidates: rawCandidates.map(normalizeCandidate),
    count: numberValue(response.count, rawCandidates.length),
    limit: numberValue(response.limit, limit),
    truncated: response.truncated === true
  };
}

function normalizeCandidate(value: unknown): EntityResolutionCandidate {
  const record = objectValue(value);
  const labels = arrayStrings(record.labels);
  const id = stringValue(record.id) || stringValue(record.entity_id);
  return {
    filePath: stringValue(record.file_path),
    id,
    labels,
    name: stringValue(record.name) || id,
    repoId: stringValue(record.repo_id),
    repoName: stringValue(record.repo_name),
    type: stringValue(record.type) || labels[0] || "Entity"
  };
}

function compactBody(
  body: Record<string, string | number | undefined>
): Record<string, string | number> {
  const compacted: Record<string, string | number> = {};
  for (const [key, value] of Object.entries(body)) {
    if (typeof value === "number" || (typeof value === "string" && value.length > 0)) {
      compacted[key] = value;
    }
  }
  return compacted;
}

function objectValue(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null
    ? value as Record<string, unknown>
    : {};
}

function arrayStrings(value: unknown): readonly string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];
}

function numberValue(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}
