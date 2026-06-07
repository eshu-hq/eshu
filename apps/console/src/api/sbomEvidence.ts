// api/sbomEvidence.ts
// SBOM / attestation evidence loaders. Surfaces the existing reducer-owned
// supply-chain read models — the console adds no new graph reads:
//   - GET /api/v0/supply-chain/sbom-attestations/attachments/count
//       totals + by-status + by-artifact-kind, no scope required
//   - GET /api/v0/supply-chain/sbom-attestations/attachments/inventory
//       browsable buckets (group_by=subject_digest by default), bounded by limit
//   - GET /api/v0/supply-chain/sbom-attestations/attachments?subject_digest=...
//       per-subject provenance drilldown (repository/workload/service evidence,
//       components, status, missing evidence, freshness)
// Every loader is defensive over response shape (see GET /api/v0/openapi.json)
// and never fabricates counts: an unavailable endpoint yields an explicit
// provenance state, not a zero.

import type { EshuApiClient } from "./client";
import type { EshuTruth } from "./envelope";

// SbomEvidenceProvenance mirrors the section-level provenance the console uses
// elsewhere: "live" when the endpoint answered, "empty" when it answered with
// no rows, "unavailable" when the call failed or the capability is off.
export type SbomEvidenceProvenance = "live" | "empty" | "unavailable";

// SbomSummary is the cheap count rollup the page shows first, before any
// payload-heavy drilldown. Counts are keyed by the closed enums the API
// advertises (attachment_status, artifact_kind).
export interface SbomSummary {
  readonly total: number;
  readonly byStatus: Readonly<Record<string, number>>;
  readonly byArtifactKind: Readonly<Record<string, number>>;
  readonly truth: EshuTruth | null;
  readonly provenance: SbomEvidenceProvenance;
}

// SbomSubjectRow is one inventory bucket: a subject digest (or other grouped
// dimension) with the number of attachments that reference it.
export interface SbomSubjectRow {
  readonly dimension: string;
  readonly value: string;
  readonly count: number;
}

// SbomInventory is a bounded page of inventory buckets plus continuation
// metadata so the page can page without unbounded scans.
export interface SbomInventory {
  readonly buckets: readonly SbomSubjectRow[];
  readonly groupBy: string;
  readonly truncated: boolean;
  readonly truth: EshuTruth | null;
  readonly provenance: SbomEvidenceProvenance;
}

// SbomComponent is one parsed SBOM component with its strongest identity hint.
export interface SbomComponent {
  readonly id: string;
  readonly name: string;
  readonly version: string;
  readonly purl: string;
  readonly cpe: string;
}

// SbomAttachment is one SBOM/attestation attachment with the provenance that
// answers "which repo / workload / service does this attestation evidence?".
export interface SbomAttachment {
  readonly attachmentId: string;
  readonly subjectDigest: string;
  readonly documentId: string;
  readonly attachmentStatus: string;
  readonly artifactKind: string;
  readonly format: string;
  readonly specVersion: string;
  readonly verificationStatus: string;
  readonly attachmentScope: string;
  readonly reason: string;
  readonly componentCount: number;
  readonly components: readonly SbomComponent[];
  readonly repositoryIds: readonly string[];
  readonly workloadIds: readonly string[];
  readonly serviceIds: readonly string[];
  readonly warningSummaries: readonly string[];
  readonly missingEvidence: readonly string[];
  readonly sourceFreshness: string;
  readonly sourceConfidence: string;
}

// SbomSubjectDetail is the drilldown for one subject digest: the attachments
// that evidence it, plus the section truth/provenance for the call.
export interface SbomSubjectDetail {
  readonly subjectDigest: string;
  readonly attachments: readonly SbomAttachment[];
  readonly truncated: boolean;
  readonly truth: EshuTruth | null;
  readonly provenance: SbomEvidenceProvenance;
}

// ---- raw response shapes (partial; see GET /api/v0/openapi.json) ----
interface CountResponse {
  readonly total_attachments?: number;
  readonly by_attachment_status?: Readonly<Record<string, number>>;
  readonly by_artifact_kind?: Readonly<Record<string, number>>;
}
interface InventoryResponse {
  readonly buckets?: readonly { dimension?: string; value?: string; count?: number }[];
  readonly group_by?: string;
  readonly truncated?: boolean;
}
interface AttachmentRecord {
  readonly attachment_id?: string;
  readonly subject_digest?: string;
  readonly document_id?: string;
  readonly attachment_status?: string;
  readonly artifact_kind?: string;
  readonly format?: string;
  readonly spec_version?: string;
  readonly verification_status?: string;
  readonly attachment_scope?: string;
  readonly reason?: string;
  readonly component_count?: number;
  readonly component_evidence?: readonly ComponentRecord[];
  readonly repository_ids?: readonly string[];
  readonly workload_ids?: readonly string[];
  readonly service_ids?: readonly string[];
  readonly warning_summaries?: readonly string[];
  readonly missing_evidence?: readonly string[];
  readonly source_freshness?: string;
  readonly source_confidence?: string;
}
interface ComponentRecord {
  readonly component_id?: string;
  readonly name?: string;
  readonly version?: string;
  readonly purl?: string;
  readonly cpe?: string;
}
interface AttachmentListResponse {
  readonly attachments?: readonly AttachmentRecord[];
  readonly truncated?: boolean;
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function strList(value: unknown): readonly string[] {
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === "string") : [];
}

function num(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

// loadSbomSummary reads the cheap count rollup. A failed call returns an
// explicit "unavailable" provenance so the page can say so rather than show 0.
export async function loadSbomSummary(client: EshuApiClient): Promise<SbomSummary> {
  try {
    const env = await client.get<CountResponse>("/api/v0/supply-chain/sbom-attestations/attachments/count");
    const data = env.data ?? {};
    const total = num(data.total_attachments);
    return {
      total,
      byStatus: data.by_attachment_status ?? {},
      byArtifactKind: data.by_artifact_kind ?? {},
      truth: env.truth ?? null,
      provenance: total > 0 ? "live" : "empty"
    };
  } catch {
    return { total: 0, byStatus: {}, byArtifactKind: {}, truth: null, provenance: "unavailable" };
  }
}

// loadSbomInventory reads a bounded page of inventory buckets, grouped by the
// requested dimension (default subject_digest). limit/offset keep the
// underlying Postgres scan bounded.
export async function loadSbomInventory(
  client: EshuApiClient,
  groupBy = "subject_digest",
  limit = 50,
  offset = 0
): Promise<SbomInventory> {
  try {
    const path =
      `/api/v0/supply-chain/sbom-attestations/attachments/inventory` +
      `?group_by=${encodeURIComponent(groupBy)}&limit=${limit}&offset=${offset}`;
    const env = await client.get<InventoryResponse>(path);
    const data = env.data ?? {};
    const buckets = (data.buckets ?? [])
      .map((b) => ({ dimension: str(b.dimension), value: str(b.value), count: num(b.count) }))
      .filter((b) => b.value !== "");
    return {
      buckets,
      groupBy: data.group_by ?? groupBy,
      truncated: data.truncated === true,
      truth: env.truth ?? null,
      provenance: buckets.length > 0 ? "live" : "empty"
    };
  } catch {
    return { buckets: [], groupBy, truncated: false, truth: null, provenance: "unavailable" };
  }
}

function mapAttachment(record: AttachmentRecord): SbomAttachment {
  return {
    attachmentId: str(record.attachment_id),
    subjectDigest: str(record.subject_digest),
    documentId: str(record.document_id),
    attachmentStatus: str(record.attachment_status),
    artifactKind: str(record.artifact_kind),
    format: str(record.format),
    specVersion: str(record.spec_version),
    verificationStatus: str(record.verification_status),
    attachmentScope: str(record.attachment_scope),
    reason: str(record.reason),
    componentCount: num(record.component_count),
    components: (record.component_evidence ?? []).map((c) => ({
      id: str(c.component_id),
      name: str(c.name),
      version: str(c.version),
      purl: str(c.purl),
      cpe: str(c.cpe)
    })),
    repositoryIds: strList(record.repository_ids),
    workloadIds: strList(record.workload_ids),
    serviceIds: strList(record.service_ids),
    warningSummaries: strList(record.warning_summaries),
    missingEvidence: strList(record.missing_evidence),
    sourceFreshness: str(record.source_freshness),
    sourceConfidence: str(record.source_confidence)
  };
}

// loadSbomSubjectDetail drills into one subject digest and returns the
// attachments that evidence it. The subject_digest scope keeps the list call
// bounded, matching the API's required-scope contract.
export async function loadSbomSubjectDetail(
  client: EshuApiClient,
  subjectDigest: string,
  limit = 25
): Promise<SbomSubjectDetail> {
  try {
    const path =
      `/api/v0/supply-chain/sbom-attestations/attachments` +
      `?subject_digest=${encodeURIComponent(subjectDigest)}&limit=${limit}`;
    const env = await client.get<AttachmentListResponse>(path);
    const data = env.data ?? {};
    const attachments = (data.attachments ?? []).map(mapAttachment);
    return {
      subjectDigest,
      attachments,
      truncated: data.truncated === true,
      truth: env.truth ?? null,
      provenance: attachments.length > 0 ? "live" : "empty"
    };
  } catch {
    return { subjectDigest, attachments: [], truncated: false, truth: null, provenance: "unavailable" };
  }
}
