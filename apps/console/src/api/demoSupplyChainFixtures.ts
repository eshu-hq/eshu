// api/demoSupplyChainFixtures.ts
// Container image, SBOM attestation, and package-dependency demo fixtures.
// Split out of demoFixtures.ts and dynamically imported from demoClient.ts's
// fetcher (issue #5139) so this surface's payload weight only lands in a
// session that actually queries the supply-chain pages.
import { demoDigest } from "./demoFixtures";

export const imageList = {
  count: 1,
  images: [
    {
      artifact_type: "",
      config_digest: "sha256:cfg9876543210",
      digest: demoDigest,
      id: `oci-image://registry.example/sample/checkout@${demoDigest}`,
      media_type: "application/vnd.oci.image.manifest.v1+json",
      name: "checkout",
      registry: "registry.example",
      repository: "sample/checkout",
      repository_id: "oci-registry://registry.example/sample/checkout",
      size_bytes: 28475610,
      source_system: "oci_registry",
      tag: "1.4.2",
    },
  ],
  limit: 50,
  offset: 0,
  truncated: false,
} as const;

export const sbomCount = {
  by_artifact_kind: { attestation: 1, sbom: 2 },
  by_attachment_status: { attached_unverified: 1, attached_verified: 2 },
  total_attachments: 3,
} as const;

export const sbomInventory = {
  buckets: [{ count: 3, dimension: "subject_digest", value: demoDigest }],
  group_by: "subject_digest",
  truncated: false,
} as const;

export const sbomAttachments = {
  attachments: [
    {
      artifact_kind: "sbom",
      attachment_id: "sbom:checkout:1",
      attachment_scope: "image",
      attachment_status: "attached_verified",
      component_count: 2,
      component_evidence: [
        {
          component_id: "pkg:npm/sample-lib@1.0.0",
          name: "sample-lib",
          purl: "pkg:npm/sample-lib@1.0.0",
          version: "1.0.0",
        },
        {
          component_id: "pkg:npm/left-pad@1.3.0",
          name: "left-pad",
          purl: "pkg:npm/left-pad@1.3.0",
          version: "1.3.0",
        },
      ],
      document_id: "doc:checkout-sbom",
      format: "spdx",
      missing_evidence: [],
      reason: "image digest matched demo checkout deployment evidence",
      repository_ids: ["repository:checkout-service"],
      service_ids: ["checkout-service"],
      source_confidence: "high",
      source_freshness: "active",
      spec_version: "2.3",
      subject_digest: demoDigest,
      verification_status: "verified",
      warning_summaries: [],
      workload_ids: ["workload:checkout"],
    },
  ],
  truncated: false,
} as const;

export const dependencyList = {
  dependencies: [
    {
      anchor_package: "sample-lib",
      anchor_package_id: "pkg:npm/sample-lib",
      declaring_version: "1.0.0",
      dependency_range: "^1.3.0",
      dependency_type: "runtime",
      direction: "forward",
      edge_id: "dep:sample-lib:left-pad",
      optional: false,
      related_ecosystem: "npm",
      related_package: "left-pad",
      related_package_id: "pkg:npm/left-pad",
    },
  ],
  direction: "forward",
  truncated: false,
} as const;
