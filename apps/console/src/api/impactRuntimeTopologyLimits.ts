import {
  boundedCollectionGraphAccounting,
  normalizeBoundedCollectionLimits,
} from "./impactBoundedCollectionLimits";
import type { RuntimeTopologyLimits } from "./impactReviewTypes";

interface RuntimeTopologyCounts {
  readonly instances: number;
  readonly platformEdges: number;
  readonly provisionedPlatforms: number;
}

export interface RuntimeTopologyGraphAccounting {
  readonly limitations: readonly string[];
  readonly omittedEdges: number;
  readonly omittedNodes: number;
  readonly truncated: boolean;
}

export function normalizeRuntimeTopologyLimits(
  value: unknown,
  counts: RuntimeTopologyCounts,
): RuntimeTopologyLimits | null {
  if (!isRecord(value)) return null;
  const instances = normalizeBoundedCollectionLimits(value.instances, counts.instances);
  const platformEdges = normalizeBoundedCollectionLimits(
    value.platform_edges,
    counts.platformEdges,
  );
  const provisionedPlatforms = normalizeBoundedCollectionLimits(
    value.provisioned_platforms,
    counts.provisionedPlatforms,
  );
  if (instances === null || platformEdges === null || provisionedPlatforms === null) return null;
  return { instances, platformEdges, provisionedPlatforms };
}

export function runtimeTopologyGraphAccounting(
  limits: RuntimeTopologyLimits | null,
): RuntimeTopologyGraphAccounting {
  if (limits === null) {
    return {
      limitations: [
        "runtime-topology completeness unverified because collection metadata is unavailable",
      ],
      omittedEdges: 0,
      omittedNodes: 0,
      truncated: false,
    };
  }

  const instances = boundedCollectionGraphAccounting(limits.instances, {
    edgeMultiplier: 1,
    family: "runtime instance",
    item: "instance",
    missingMetadataLimitation: "",
    nodeMultiplier: 1,
  });
  const placements = boundedCollectionGraphAccounting(limits.platformEdges, {
    edgeMultiplier: 1,
    family: "direct placement",
    item: "relationship",
    missingMetadataLimitation: "",
    nodeMultiplier: 0,
  });
  const provisioned = boundedCollectionGraphAccounting(limits.provisionedPlatforms, {
    edgeMultiplier: 2,
    family: "provisioned platform",
    item: "platform record",
    missingMetadataLimitation: "",
    nodeMultiplier: 0,
  });
  return {
    limitations: [...instances.limitations, ...placements.limitations, ...provisioned.limitations],
    omittedEdges: instances.omittedEdges + placements.omittedEdges + provisioned.omittedEdges,
    omittedNodes: instances.omittedNodes + placements.omittedNodes + provisioned.omittedNodes,
    truncated: instances.truncated || placements.truncated || provisioned.truncated,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
