// api/evidenceBundle.ts
// Loader for GET /api/v0/evidence/bundle (issue #4045): the console reads the
// same live evidence_bundle.v1 artifact `eshu evidence bundle export --live`
// composes, from the same status providers, so the console panel and the CLI
// command generate the identical bundle rather than two independently
// maintained readings. See docs/public/reference/evidence-bundle.md.
//
// The route is stack-wide (go/internal/query/evidence_bundle_live.go): it
// carries no scoped-token support, and AuthMiddleware rejects a scoped-bearer
// or non-tenant-bound browser-session caller with HTTP 403 before the handler
// ever runs (go/internal/query/evidence_bundle_live_test.go
// TestAuthMiddlewareWithScopedTokensRejectsEvidenceBundleRoute). In a hosted
// multi-tenant deployment every browser session is rejected this way, even an
// admin one — that is the documented posture, not a bug. loadEvidenceBundle
// therefore treats a 403 as the distinct "forbidden" provenance (a scope
// boundary), separate from "unavailable" (a real error), the same
// isForbidden/403-only convention adminConsoleAudit.ts already established for
// the global-operator-only audit routes (#3717).
//
// Field names match the wire shape verbatim
// (go/internal/evidencebundle/types.go, go/internal/query/openapi_paths_evidence_bundle.go).
// Only the fields the panel renders are modeled; the response carries more
// (answer/investigation packets, catalog snapshots, reproduce calls, bounds)
// that this loader does not need to type.
import type { AdminAuditProvenance } from "./adminConsoleTypes";
import type { EshuApiClient } from "./client";
import { EshuApiHttpError } from "./client";

export interface EvidenceBundleIdentity {
  readonly scope_id: string;
  readonly profile: string;
  readonly created_at: string;
}

export interface EvidenceBundlePipelineQueue {
  readonly total: number;
  readonly outstanding: number;
  readonly overdue_claims: number;
  readonly oldest_outstanding_age_seconds: number;
  readonly pending: number;
  readonly in_flight: number;
  readonly retrying: number;
  readonly succeeded: number;
  readonly failed: number;
  readonly dead_letter: number;
}

export interface EvidenceBundleCollectorReadiness {
  readonly collector_kind: string;
  readonly status_category: string;
  readonly health: string;
}

export interface EvidenceBundlePipelineState {
  readonly repository_count: number;
  readonly health_state: string;
  readonly health_reasons?: readonly string[];
  readonly queue: EvidenceBundlePipelineQueue;
  readonly queue_blocked_count: number;
  readonly collectors?: readonly EvidenceBundleCollectorReadiness[];
}

export interface EvidenceBundleSemanticProviderProfile {
  readonly profile_id: string;
  readonly provider_kind: string;
  readonly state: string;
  readonly reason?: string;
}

export interface EvidenceBundleSemanticProviderState {
  readonly state: string;
  readonly reason?: string;
  readonly provider_configured: boolean;
  readonly provider_profiles?: readonly EvidenceBundleSemanticProviderProfile[];
}

export interface EvidenceBundleMissingEvidenceRow {
  readonly family: string;
  readonly reason: string;
}

export interface EvidenceBundleValidation {
  readonly status: string;
  readonly checks: readonly string[];
}

export interface EvidenceBundleRedaction {
  readonly profile: string;
  readonly rules: readonly string[];
}

export interface EvidenceBundleWire {
  readonly schema_version: string;
  readonly bundle_id: string;
  readonly identity: EvidenceBundleIdentity;
  readonly redaction: EvidenceBundleRedaction;
  readonly contents: {
    readonly pipeline_state?: EvidenceBundlePipelineState;
    readonly semantic_provider_state?: EvidenceBundleSemanticProviderState;
  };
  readonly missing_evidence: readonly EvidenceBundleMissingEvidenceRow[];
  readonly validation: EvidenceBundleValidation;
}

// EvidenceBundleProvenance reuses the same three-state contract
// adminConsoleAudit.ts already established for the global-operator-only audit
// routes: "live" data, "unavailable" (a real error), or "forbidden" (a scope
// boundary the current session cannot cross — not a failure).
export type EvidenceBundleProvenance = AdminAuditProvenance;

export interface EvidenceBundleResult {
  readonly bundle: EvidenceBundleWire | null;
  readonly provenance: EvidenceBundleProvenance;
}

// isForbidden reports whether an error is an HTTP 403 — the signal that the
// route rejected this caller's auth mode (scoped bearer token, or a browser
// session that is not the tenant-bound all-scopes owner session) before the
// handler ever ran. It is a scope boundary, not a failure, and must be
// surfaced as "forbidden" so the panel can explain the posture instead of
// showing a generic error.
function isForbidden(err: unknown): boolean {
  return err instanceof EshuApiHttpError && err.status === 403;
}

export async function loadEvidenceBundle(client: EshuApiClient): Promise<EvidenceBundleResult> {
  try {
    const bundle = await client.getJson<EvidenceBundleWire>("/api/v0/evidence/bundle");
    return { bundle, provenance: "live" };
  } catch (err) {
    if (isForbidden(err)) {
      return { bundle: null, provenance: "forbidden" };
    }
    console.error("[evidenceBundle] loadEvidenceBundle failed", err);
    return { bundle: null, provenance: "unavailable" };
  }
}
