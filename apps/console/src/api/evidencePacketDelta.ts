import type { ChangedSincePageData, ChangeClassification } from "./changedSince";

/** UI-facing verdict vocabulary for current-vs-baseline evidence packet diffs. */
export type EvidencePacketDeltaVerdict = "added" | "changed" | "removed" | "stale" | "unchanged";

/** Per-category counts after mapping backend changed-since states to packet terms. */
export interface EvidencePacketDeltaCounts {
  readonly added: number;
  readonly changed: number;
  readonly removed: number;
  readonly stale: number;
  readonly unchanged: number;
}

/** Bounded stable-fact sample rendered as an evidence hop in the comparison UI. */
export interface EvidencePacketDeltaHop {
  readonly category: string;
  readonly factKind: string;
  readonly stableFactKey: string;
  readonly truncated: boolean;
  readonly verdict: EvidencePacketDeltaVerdict;
  readonly verdictLabel: string;
}

/** One grouped bucket of evidence packet changes for a backend diff category. */
export interface EvidencePacketDeltaGroup {
  readonly category: string;
  readonly counts: EvidencePacketDeltaCounts;
  readonly hops: readonly EvidencePacketDeltaHop[];
}

/** Complete console view model for comparing a prior packet to current graph state. */
export interface EvidencePacketComparison {
  readonly baselineGenerationId: string;
  readonly boundedSampleNote: string;
  readonly currentGenerationId: string;
  readonly currentObservedAt: string | null;
  readonly groups: readonly EvidencePacketDeltaGroup[];
  readonly sampleLimit: number;
  readonly sinceObservedAt: string | null;
}

const classificationOrder: readonly ChangeClassification[] = [
  "added",
  "updated",
  "retired",
  "superseded",
  "unchanged"
];

const verdictLabels: Record<EvidencePacketDeltaVerdict, string> = {
  added: "added",
  changed: "changed",
  removed: "removed/retracted",
  stale: "stale/missing",
  unchanged: "unchanged"
};

/**
 * Converts the generated changed-since read model into the console's evidence
 * packet comparison vocabulary without dropping retired or superseded facts.
 */
export function buildEvidencePacketComparison(page: ChangedSincePageData): EvidencePacketComparison {
  const groups = page.categories.map((category) => ({
    category: category.category || "unknown",
    counts: {
      added: category.counts.added,
      changed: category.counts.updated,
      removed: category.counts.retired,
      stale: category.counts.superseded,
      unchanged: category.counts.unchanged
    },
    hops: classificationOrder.flatMap((classification) => {
      const verdict = verdictForClassification(classification);
      return category.samples[classification].map((sample) => ({
        category: category.category || "unknown",
        factKind: sample.factKind || "fact",
        stableFactKey: sample.stableFactKey || "-",
        truncated: category.truncated[classification],
        verdict,
        verdictLabel: verdictLabels[verdict]
      }));
    })
  }));
  return {
    baselineGenerationId: page.sinceGenerationId || page.sinceObservedAt || "baseline",
    boundedSampleNote: boundedSampleNote(page, groups),
    currentGenerationId: page.currentActiveGenerationId || page.currentObservedAt || "current",
    currentObservedAt: page.currentObservedAt,
    groups,
    sampleLimit: page.sampleLimit,
    sinceObservedAt: page.sinceObservedAt
  };
}

function verdictForClassification(classification: ChangeClassification): EvidencePacketDeltaVerdict {
  if (classification === "updated") return "changed";
  if (classification === "retired") return "removed";
  if (classification === "superseded") return "stale";
  return classification;
}

function boundedSampleNote(
  page: ChangedSincePageData,
  groups: readonly EvidencePacketDeltaGroup[]
): string {
  const truncatedCount = groups.reduce(
    (sum, group) => sum + group.hops.filter((hop) => hop.truncated).length,
    0
  );
  const base = `${page.sampleLimit} samples per verdict`;
  if (truncatedCount === 0) return base;
  return `${base}; ${truncatedCount === 1 ? "one bucket is" : `${truncatedCount} buckets are`} truncated`;
}
