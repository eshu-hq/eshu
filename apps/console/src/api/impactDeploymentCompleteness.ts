import type { DeploymentTraceResult } from "./impactReviewTypes";

export function requiredTopologyRelationshipLimitations(
  trace: DeploymentTraceResult,
): readonly string[] {
  const limitations = new Set<string>();
  const hasExactDefines = trace.topologyEdges.some(
    (edge) =>
      edge.relationshipType === "DEFINES" &&
      edge.sourceId === trace.repoId &&
      edge.targetId === trace.workloadId,
  );
  if (trace.repoId.length > 0 && trace.workloadId.length > 0 && !hasExactDefines) {
    limitations.add(
      "subject relationship backbone incomplete; exact DEFINES edge was not returned",
    );
  }

  const allInstancesHaveExactWorkload = trace.instances.every((instance) =>
    trace.topologyEdges.some(
      (edge) =>
        edge.relationshipType === "INSTANCE_OF" &&
        edge.sourceId === instance.id &&
        edge.targetId === trace.workloadId,
    ),
  );
  if (trace.instances.length > 0 && !allInstancesHaveExactWorkload) {
    limitations.add(
      "subject relationship backbone incomplete; exact INSTANCE_OF edges were not returned",
    );
  }

  for (const instance of trace.instances) {
    for (const platform of instance.platforms) {
      if (platform.topologyBasis !== "direct_runtime") {
        limitations.add("runtime topology basis unverified; expected direct_runtime");
      }
      const hasExactRunsOn = platform.topologyEdges.some(
        (edge) =>
          edge.relationshipType === "RUNS_ON" &&
          edge.sourceId === instance.id &&
          edge.targetId === platform.id,
      );
      if (!hasExactRunsOn) {
        limitations.add(
          "runtime relationship backbone incomplete; exact RUNS_ON edge was not returned",
        );
      }
    }
  }

  for (const platform of trace.provisionedPlatforms) {
    if (platform.topologyBasis !== "provisioning_fallback") {
      limitations.add("provisioning topology basis unverified; expected provisioning_fallback");
    }
    const hasExactDependency = platform.topologyEdges.some(
      (edge) =>
        edge.relationshipType === "PROVISIONS_DEPENDENCY_FOR" &&
        edge.sourceId?.startsWith("repository:") === true &&
        edge.targetId === trace.repoId,
    );
    if (!hasExactDependency) {
      limitations.add(
        "provisioning relationship backbone incomplete; exact PROVISIONS_DEPENDENCY_FOR edge was not returned",
      );
    }
    const hasExactPlatform = platform.topologyEdges.some(
      (edge) =>
        edge.relationshipType === "PROVISIONS_PLATFORM" &&
        edge.sourceId?.startsWith("repository:") === true &&
        edge.targetId === platform.id,
    );
    if (!hasExactPlatform) {
      limitations.add(
        "provisioning relationship backbone incomplete; exact PROVISIONS_PLATFORM edge was not returned",
      );
    }
  }

  return [...limitations];
}
