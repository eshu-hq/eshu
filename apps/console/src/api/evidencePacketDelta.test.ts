import { describe, expect, it } from "vitest";
import type { ChangedSincePageData } from "./changedSince";
import { buildEvidencePacketComparison } from "./evidencePacketDelta";

describe("evidencePacketDelta", () => {
  it("maps changed-since classifications into evidence packet comparison states", () => {
    const comparison = buildEvidencePacketComparison(pageData);

    expect(comparison.baselineGenerationId).toBe("gen-prior");
    expect(comparison.currentGenerationId).toBe("gen-current");
    expect(comparison.currentObservedAt).toBe("2026-06-13T18:00:00Z");
    expect(comparison.sinceObservedAt).toBe("2026-06-12T18:00:00Z");
    expect(comparison.sampleLimit).toBe(25);
    expect(comparison.groups[0]).toMatchObject({
      category: "files",
      counts: {
        added: 2,
        changed: 1,
        removed: 1,
        stale: 1,
        unchanged: 5
      }
    });
    expect(comparison.groups[0].hops.map((hop) => hop.verdict)).toEqual([
      "added",
      "changed",
      "removed",
      "stale",
      "unchanged"
    ]);
    expect(comparison.groups[0].hops.find((hop) => hop.verdict === "removed")).toMatchObject({
      stableFactKey: "legacy/config.yaml",
      verdictLabel: "removed/retracted"
    });
    expect(comparison.groups[0].hops.find((hop) => hop.verdict === "stale")).toMatchObject({
      stableFactKey: "old/service-owner",
      truncated: true,
      verdictLabel: "stale/missing"
    });
    expect(comparison.boundedSampleNote).toBe("25 samples per verdict; one bucket is truncated");
  });
});

const pageData: ChangedSincePageData = {
  categories: [{
    category: "files",
    changedCount: 5,
    counts: { added: 2, retired: 1, superseded: 1, unchanged: 5, updated: 1 },
    samples: {
      added: [{ factKind: "file", stableFactKey: "src/main.go" }],
      retired: [{ factKind: "file", stableFactKey: "legacy/config.yaml" }],
      superseded: [{ factKind: "service_owner", stableFactKey: "old/service-owner" }],
      unchanged: [{ factKind: "file", stableFactKey: "README.md" }],
      updated: [{ factKind: "file", stableFactKey: "src/router.go" }]
    },
    truncated: { added: false, retired: false, superseded: true, unchanged: false, updated: false },
    unavailable: false
  }],
  changedCount: 5,
  currentActiveGenerationId: "gen-current",
  currentObservedAt: "2026-06-13T18:00:00Z",
  mode: "repository",
  sampleLimit: 25,
  scopeId: "git-repository-scope:acme/app",
  scopeKind: "repository",
  scopeLabel: "acme/app",
  sinceGenerationId: "gen-prior",
  sinceObservedAt: "2026-06-12T18:00:00Z",
  truth: {
    basis: "semantic_facts",
    capability: "freshness.changed_since",
    freshness: { state: "fresh" },
    level: "exact",
    profile: "production"
  },
  unchangedCount: 5,
  unavailable: false,
  unavailableReason: ""
};
