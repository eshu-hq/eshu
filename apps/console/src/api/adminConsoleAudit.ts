// api/adminConsoleAudit.ts
// Audit loaders for the admin console (issue #3703), split out of
// adminConsole.ts to keep each module well under the 500-line limit. These two
// routes are GLOBAL shared-operator only, so a tenant admin receives HTTP 403.
// That is a scope boundary, NOT a failure: both loaders surface provenance
// "forbidden" distinctly from "unavailable" (a real error) so the audit panel
// can render an operator-scope note instead of an error. See #3717.
//
// Field names match the backend OpenAPI verbatim
// (go/internal/query/openapi_paths_auth_admin_reads.go). Only audit-safe fields
// are modeled — never actor, scope, or policy revision hashes.
import { EshuApiHttpError } from "./client";
import type { EshuApiClient } from "./client";
import type { AdminAuditProvenance } from "./adminConsoleTypes";

// ---------------------------------------------------------------------------
// Audit events — GET /api/v0/auth/admin/audit/events
// ---------------------------------------------------------------------------

export interface AuditEventItem {
  readonly event_type?: string;
  readonly actor_class?: string;
  readonly scope_class?: string;
  readonly decision?: string;
  readonly reason_code?: string;
  readonly occurred_at?: string;
  readonly service_principal_id?: string;
  readonly correlation_id?: string;
}

export interface AuditEventsResult {
  readonly events: readonly AuditEventItem[];
  readonly truncated: boolean;
  readonly provenance: AdminAuditProvenance;
}

interface AuditEventsWire {
  readonly events?: readonly AuditEventItem[];
  readonly truncated?: boolean;
}

// isForbidden reports whether an error is an HTTP 403 — the signal that the
// caller is a tenant admin asking a global-operator-only route. It is a scope
// boundary, not a failure, and must be surfaced as "forbidden" so the audit
// panel can show an operator-scope note rather than an error.
function isForbidden(err: unknown): boolean {
  return err instanceof EshuApiHttpError && err.status === 403;
}

export async function loadAuditEvents(client: EshuApiClient): Promise<AuditEventsResult> {
  try {
    const resp = await client.getJson<AuditEventsWire>("/api/v0/auth/admin/audit/events");
    return { events: resp.events ?? [], truncated: resp.truncated ?? false, provenance: "live" };
  } catch (err) {
    if (isForbidden(err)) {
      return { events: [], truncated: false, provenance: "forbidden" };
    }
    console.error("[adminConsole] loadAuditEvents failed", err);
    return { events: [], truncated: false, provenance: "unavailable" };
  }
}

// ---------------------------------------------------------------------------
// Audit summary — GET /api/v0/auth/admin/audit/summary
// ---------------------------------------------------------------------------

export interface AuditCount {
  readonly name: string;
  readonly count: number;
}

export interface AuditSummaryData {
  readonly total?: number;
  readonly allowed?: number;
  readonly denied?: number;
  readonly unavailable?: number;
  readonly last_occurred_at?: string;
  readonly event_type_counts?: readonly AuditCount[];
  readonly decision_counts?: readonly AuditCount[];
  readonly reason_counts?: readonly AuditCount[];
  readonly actor_class_counts?: readonly AuditCount[];
  readonly scope_class_counts?: readonly AuditCount[];
}

export interface AuditSummaryResult {
  readonly summary: AuditSummaryData | null;
  readonly provenance: AdminAuditProvenance;
}

export async function loadAuditSummary(client: EshuApiClient): Promise<AuditSummaryResult> {
  try {
    const data = await client.getJson<AuditSummaryData>("/api/v0/auth/admin/audit/summary");
    return { summary: data, provenance: "live" };
  } catch (err) {
    if (isForbidden(err)) {
      return { summary: null, provenance: "forbidden" };
    }
    console.error("[adminConsole] loadAuditSummary failed", err);
    return { summary: null, provenance: "unavailable" };
  }
}
