import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { SbomPage } from "./SbomPage";
import type { EshuApiClient } from "../api/client";

// SbomPage surfaces the existing SBOM/attestation read models. The render tests
// cover the live browse -> drilldown path and the unavailable state, so the page
// never shows a fabricated zero when the capability is off.
describe("SbomPage", () => {
  const truth = { profile: "production", level: "exact", capability: "x", freshness: { state: "fresh" } };

  function liveClient(): EshuApiClient {
    return {
      get: async (path: string) => {
        if (path.includes("/attachments/count")) {
          return {
            data: {
              total_attachments: 148,
              by_attachment_status: { attached_verified: 100, attached_parse_only: 48 },
              by_artifact_kind: { sbom: 120, attestation: 28 }
            },
            error: null, truth
          };
        }
        if (path.includes("/attachments/inventory")) {
          return {
            data: {
              group_by: "subject_digest", truncated: false,
              buckets: [{ dimension: "subject_digest", value: "sha256:abcdef0123456789abcdef0123456789", count: 2 }]
            },
            error: null, truth
          };
        }
        if (path.includes("/attachments?subject_digest=")) {
          return {
            data: {
              truncated: false,
              attachments: [{
                attachment_id: "att_1",
                subject_digest: "sha256:abcdef0123456789abcdef0123456789",
                attachment_status: "attached_verified",
                artifact_kind: "sbom",
                component_count: 1,
                component_evidence: [{ component_id: "c1", name: "lodash", version: "4.17.21", purl: "pkg:npm/lodash@4.17.21" }],
                repository_ids: ["repo_42"],
                workload_ids: [],
                service_ids: [],
                missing_evidence: ["image_referrer_evidence"],
                source_freshness: "active"
              }]
            },
            error: null, truth
          };
        }
        return { data: {}, error: null, truth: null };
      }
    } as unknown as EshuApiClient;
  }

  it("renders the count rollup and a subject browse, then drills into provenance", async () => {
    render(<SbomPage client={liveClient()} />);

    expect(screen.getByRole("heading", { name: "SBOM & Attestations" })).toBeInTheDocument();
    expect(screen.getByLabelText("SBOM evidence workbench")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("148")).toBeInTheDocument());
    expect(screen.getByText("100/148")).toBeInTheDocument();

    const subjectCell = await screen.findByText(/sha256:abcdef0123/);
    fireEvent.click(subjectCell);

    // drilldown shows the attachment status, repository provenance, and missing hop
    expect(await screen.findByText("attached_verified")).toBeInTheDocument();
    expect(screen.getByText("repo_42")).toBeInTheDocument();
    expect(screen.getByText("image_referrer_evidence")).toBeInTheDocument();
    expect(screen.getByText(/lodash@4.17.21/)).toBeInTheDocument();
  });

  it("shows an unavailable state when the endpoints fail, never a fabricated zero", async () => {
    const downClient = {
      get: async () => { throw new Error("capability off"); }
    } as unknown as EshuApiClient;
    render(<SbomPage client={downClient} />);

    await waitFor(() => expect(screen.getByText(/requires the sbom-attestation collector/)).toBeInTheDocument());
    // the unavailable state surfaces "API not available" (tile sub + panel sub)
    // and shows em-dashes rather than a fabricated 0.
    expect(screen.getAllByText("API not available").length).toBeGreaterThan(0);
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });
});
