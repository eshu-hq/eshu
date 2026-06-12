// api/imageInventory.ts
// Container-image (OCI) inventory loaders for GET /api/v0/images. Kept in its own
// module so the console snapshot adapter (eshuConsoleLive.ts) stays under the
// file-size cap. The endpoint surfaces (:ContainerImage) node properties only:
// ContainerImage nodes carry no workload edges in the graph (DEPLOYS_FROM is
// Repository->Repository), so there is deliberately no "deploying workloads"
// field here — the page must not fabricate one.

import type { EshuApiClient } from "./client";
import type { EshuTruth } from "./envelope";
import { EshuEnvelopeError } from "./envelope";
import type { SectionProvenance } from "./eshuConsoleLive";

// ImageRow mirrors one row of GET /api/v0/images. Only fields the endpoint
// actually returns are surfaced; absent values stay empty/null rather than
// invented.
export interface ImageRow {
  readonly id: string;
  readonly digest: string;
  readonly repositoryId: string;
  readonly registry: string;
  readonly repository: string;
  readonly name: string;
  readonly tag: string;
  readonly mediaType: string;
  readonly artifactType: string;
  readonly configDigest: string;
  readonly sizeBytes: number | null;
  readonly sourceSystem: string;
}

interface ImageListResponse {
  readonly images?: readonly Record<string, unknown>[];
  readonly count?: number; readonly limit?: number; readonly offset?: number;
  readonly truncated?: boolean; readonly next_cursor?: { readonly offset?: number };
}

// ImagePage is one bounded page of the container-image inventory plus the cursor
// the Images browse surface uses to fetch the next page. truth/provenance let the
// page render explicit truth chips and an empty/unavailable state.
export interface ImagePage {
  readonly images: readonly ImageRow[];
  readonly nextOffset: number | null;
  readonly truth: EshuTruth | null;
  readonly provenance: SectionProvenance;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

// imageRowFromRecord maps one GET /api/v0/images record into the view-model row.
// size_bytes is null when the endpoint omits it (the field is omitempty server
// side) so the UI can show "—" rather than a misleading 0. The record is read
// defensively so the loose snapshot path and the typed page loader can share it.
export function imageRowFromRecord(r: Record<string, unknown>): ImageRow {
  const size = r.size_bytes;
  return {
    id: str(r.id),
    digest: str(r.digest),
    repositoryId: str(r.repository_id),
    registry: str(r.registry),
    repository: str(r.repository),
    name: str(r.name),
    tag: str(r.tag),
    mediaType: str(r.media_type),
    artifactType: str(r.artifact_type),
    configDigest: str(r.config_digest),
    sizeBytes: typeof size === "number" && Number.isFinite(size) ? size : null,
    sourceSystem: str(r.source_system)
  };
}

// imageRowsFromResponse projects a raw list response into id-bearing view rows.
// It accepts a loose `images` array so both the typed page loader and the console
// snapshot section (which reads a permissive record shape) can share it.
export function imageRowsFromResponse(
  data: { readonly images?: readonly Record<string, unknown>[] } | null | undefined
): ImageRow[] {
  return (data?.images ?? []).map(imageRowFromRecord).filter((row) => row.id !== "");
}

// loadImages fetches one page of GET /api/v0/images. It is the loader the Images
// page calls directly so it can paginate via next_cursor without round-tripping
// through the whole console snapshot. Filters are optional exact-match anchors.
export async function loadImages(
  client: EshuApiClient,
  opts: { limit?: number; offset?: number; digest?: string; repositoryId?: string; tag?: string } = {}
): Promise<ImagePage> {
  const limit = opts.limit ?? 50;
  const offset = opts.offset ?? 0;
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  if (opts.digest) params.set("digest", opts.digest);
  if (opts.repositoryId) params.set("repository_id", opts.repositoryId);
  if (opts.tag) params.set("tag", opts.tag);
  try {
    const env = await client.get<ImageListResponse>(`/api/v0/images?${params.toString()}`);
    if (env.error) throw new EshuEnvelopeError(env.error);
    const rows = imageRowsFromResponse(env.data);
    const nextOffset = env.data?.truncated ? env.data?.next_cursor?.offset ?? null : null;
    return {
      images: rows,
      nextOffset: typeof nextOffset === "number" ? nextOffset : null,
      truth: env.truth ?? null,
      provenance: rows.length > 0 ? "live" : "empty"
    };
  } catch {
    return { images: [], nextOffset: null, truth: null, provenance: "unavailable" };
  }
}
