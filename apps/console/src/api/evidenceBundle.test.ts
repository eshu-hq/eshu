// api/evidenceBundle.test.ts
// Verifies loadEvidenceBundle (issue #4045):
//   - the happy path hits GET /api/v0/evidence/bundle and returns provenance
//     "live" with the parsed bundle
//   - a 403 (the route's real rejection for a scoped-bearer or non-owner
//     browser-session caller) maps to provenance "forbidden" — a scope
//     boundary, not a failure, mirroring adminConsole.ts's audit loaders
//   - any other error (500/503/network) maps to provenance "unavailable"
//     with bundle: null — never a fabricated bundle
import { describe, it, expect, vi } from "vitest";

import { EshuApiHttpError } from "./client";
import type { EshuApiClient } from "./client";
import { loadEvidenceBundle } from "./evidenceBundle";
import type { EvidenceBundleWire } from "./evidenceBundle";

function fixtureBundle(): EvidenceBundleWire {
  return {
    schema_version: "evidence_bundle.v1",
    bundle_id: "sha256:deadbeef",
    identity: { scope_id: "stack", profile: "live", created_at: "2026-08-11T00:00:00Z" },
    redaction: { profile: "default", rules: ["screened_private_endpoints"] },
    contents: {
      pipeline_state: {
        repository_count: 42,
        health_state: "healthy",
        queue: {
          total: 10,
          outstanding: 2,
          overdue_claims: 0,
          oldest_outstanding_age_seconds: 5,
          pending: 1,
          in_flight: 1,
          retrying: 0,
          succeeded: 8,
          failed: 0,
          dead_letter: 0,
        },
        queue_blocked_count: 0,
      },
      semantic_provider_state: {
        state: "available",
        provider_configured: true,
      },
    },
    missing_evidence: [
      { family: "fact_counts", reason: "no status endpoint exposes per-kind fact counts" },
    ],
    validation: { status: "passed", checks: ["schema", "redaction"] },
  };
}

describe("loadEvidenceBundle", () => {
  it("calls GET /api/v0/evidence/bundle and returns provenance 'live'", async () => {
    const getJson = vi.fn(async () => fixtureBundle());
    const client = { getJson } as unknown as EshuApiClient;

    const result = await loadEvidenceBundle(client);

    expect(getJson).toHaveBeenCalledWith("/api/v0/evidence/bundle");
    expect(result.provenance).toBe("live");
    expect(result.bundle?.bundle_id).toBe("sha256:deadbeef");
    expect(result.bundle?.contents.pipeline_state?.repository_count).toBe(42);
  });

  it("maps a 403 to provenance 'forbidden' with bundle: null (scope boundary, not a failure)", async () => {
    const client = {
      getJson: vi.fn(async () => {
        throw new EshuApiHttpError(403);
      }),
    } as unknown as EshuApiClient;

    const result = await loadEvidenceBundle(client);

    expect(result.provenance).toBe("forbidden");
    expect(result.bundle).toBeNull();
  });

  it("maps a 503 to provenance 'unavailable' with bundle: null (real error, no fabrication)", async () => {
    const client = {
      getJson: vi.fn(async () => {
        throw new EshuApiHttpError(503);
      }),
    } as unknown as EshuApiClient;

    const result = await loadEvidenceBundle(client);

    expect(result.provenance).toBe("unavailable");
    expect(result.bundle).toBeNull();
  });

  it("maps a non-HTTP error (e.g. network failure) to provenance 'unavailable'", async () => {
    const client = {
      getJson: vi.fn(async () => {
        throw new Error("network down");
      }),
    } as unknown as EshuApiClient;

    const result = await loadEvidenceBundle(client);

    expect(result.provenance).toBe("unavailable");
    expect(result.bundle).toBeNull();
  });
});
