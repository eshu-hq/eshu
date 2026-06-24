import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { loadSbomSummary, loadSbomInventory, loadSbomSubjectDetail } from "./sbomEvidence";

// The SBOM loaders surface the existing reducer-owned supply-chain read models
// (count + inventory + per-subject list). They must read the real response
// shapes and translate a failed call into an explicit "unavailable" provenance,
// never a fabricated zero.
describe("sbomEvidence", () => {
  const truth = {
    profile: "production",
    level: "exact",
    capability: "supply_chain.sbom_attestation_attachments.aggregate",
    freshness: { state: "fresh" }
  };

  function fakeClient(): EshuApiClient {
    return {
      get: async (path: string) => {
        if (path.includes("/sbom-attestations/attachments/count")) {
          return {
            data: {
              total_attachments: 148,
              by_attachment_status: { attached_verified: 100, attached_parse_only: 48 },
              by_artifact_kind: { sbom: 120, attestation: 28 }
            },
            error: null,
            truth
          };
        }
        if (path.includes("/sbom-attestations/attachments/inventory")) {
          return {
            data: {
              group_by: "subject_digest",
              truncated: true,
              buckets: [
                { dimension: "subject_digest", value: "sha256:aaa", count: 3 },
                { dimension: "subject_digest", value: "sha256:bbb", count: 1 },
                { dimension: "subject_digest", value: "", count: 9 }
              ]
            },
            error: null,
            truth
          };
        }
        if (path.includes("/sbom-attestations/attachments?subject_digest=")) {
          return {
            data: {
              truncated: false,
              attachments: [
                {
                  attachment_id: "att_1",
                  subject_digest: "sha256:aaa",
                  document_id: "doc_1",
                  attachment_status: "attached_verified",
                  artifact_kind: "sbom",
                  format: "cyclonedx",
                  spec_version: "1.5",
                  verification_status: "verified",
                  component_count: 2,
                  component_evidence: [
                    { component_id: "c1", name: "lodash", version: "4.17.21", purl: "pkg:npm/lodash@4.17.21" },
                    { component_id: "c2", name: "alpine", version: "3.19", cpe: "cpe:2.3:o:alpine" }
                  ],
                  repository_ids: ["repo_1"],
                  workload_ids: ["wl_1"],
                  service_ids: ["svc_1"],
                  missing_evidence: ["image_referrer_evidence"],
                  source_freshness: "active",
                  source_confidence: "inferred"
                }
              ]
            },
            error: null,
            truth
          };
        }
        return { data: {}, error: null, truth: null };
      }
    } as unknown as EshuApiClient;
  }

  it("reads the cheap count rollup and captures truth", async () => {
    const summary = await loadSbomSummary(fakeClient());
    expect(summary.total).toBe(148);
    expect(summary.byStatus).toEqual({ attached_verified: 100, attached_parse_only: 48 });
    expect(summary.byArtifactKind).toEqual({ sbom: 120, attestation: 28 });
    expect(summary.provenance).toBe("live");
    expect(summary.truth?.profile).toBe("production");
  });

  it("reports unavailable when the count endpoint fails, never a fabricated zero", async () => {
    const client = {
      get: async () => { throw new Error("capability off"); }
    } as unknown as EshuApiClient;
    const summary = await loadSbomSummary(client);
    expect(summary.provenance).toBe("unavailable");
    expect(summary.total).toBe(0);
    expect(summary.truth).toBeNull();
  });

  it("reports unavailable when the count endpoint returns an Eshu error envelope", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_runtime_profile",
          message: "SBOM summary is unavailable in this profile",
          capability: "supply_chain.sbom_attestation_attachments.aggregate"
        },
        truth: null
      })
    } as unknown as EshuApiClient;
    const summary = await loadSbomSummary(client);
    expect(summary.provenance).toBe("unavailable");
    expect(summary.total).toBe(0);
    expect(summary.truth).toBeNull();
  });

  it("maps inventory buckets, drops empty values, and carries truncated", async () => {
    const inv = await loadSbomInventory(fakeClient());
    expect(inv.groupBy).toBe("subject_digest");
    expect(inv.truncated).toBe(true);
    // the empty-value bucket is dropped so the browse has no blank rows
    expect(inv.buckets).toEqual([
      { dimension: "subject_digest", value: "sha256:aaa", count: 3 },
      { dimension: "subject_digest", value: "sha256:bbb", count: 1 }
    ]);
    expect(inv.provenance).toBe("live");
  });

  it("reports unavailable when inventory returns an Eshu error envelope", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_runtime_profile",
          message: "SBOM inventory is unavailable in this profile",
          capability: "supply_chain.sbom_attestation_attachments.inventory"
        },
        truth: null
      })
    } as unknown as EshuApiClient;
    const inv = await loadSbomInventory(client);
    expect(inv.provenance).toBe("unavailable");
    expect(inv.buckets).toEqual([]);
    expect(inv.truth).toBeNull();
  });

  it("drills into a subject and maps provenance, components, and missing evidence", async () => {
    const detail = await loadSbomSubjectDetail(fakeClient(), "sha256:aaa");
    expect(detail.attachments).toHaveLength(1);
    const att = detail.attachments[0];
    expect(att.attachmentStatus).toBe("attached_verified");
    expect(att.repositoryIds).toEqual(["repo_1"]);
    expect(att.workloadIds).toEqual(["wl_1"]);
    expect(att.serviceIds).toEqual(["svc_1"]);
    expect(att.components.map((c) => c.name)).toEqual(["lodash", "alpine"]);
    expect(att.missingEvidence).toEqual(["image_referrer_evidence"]);
    expect(att.sourceFreshness).toBe("active");
    expect(detail.provenance).toBe("live");
  });

  it("reports unavailable when subject detail returns an Eshu error envelope", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_runtime_profile",
          message: "SBOM detail is unavailable in this profile",
          capability: "supply_chain.sbom_attestation_attachments.list"
        },
        truth: null
      })
    } as unknown as EshuApiClient;
    const detail = await loadSbomSubjectDetail(client, "sha256:aaa");
    expect(detail.provenance).toBe("unavailable");
    expect(detail.attachments).toEqual([]);
    expect(detail.truth).toBeNull();
  });

  it("encodes the subject digest into the drilldown query", async () => {
    const seen: string[] = [];
    const client = {
      get: async (path: string) => { seen.push(path); return { data: { attachments: [] }, error: null, truth: null }; }
    } as unknown as EshuApiClient;
    await loadSbomSubjectDetail(client, "sha256:a/b+c");
    expect(seen[0]).toContain("subject_digest=sha256%3Aa%2Fb%2Bc");
  });
});
