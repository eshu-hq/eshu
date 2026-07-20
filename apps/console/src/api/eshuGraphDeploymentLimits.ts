import type { EshuTruth } from "./envelope";
import type { DeploymentGraphDetail } from "./eshuGraphDeploymentWire";

export interface DeploymentGraphBuildOptions {
  readonly contextTruth?: EshuTruth | null;
  readonly detail?: DeploymentGraphDetail;
  readonly traceTruth?: EshuTruth | null;
}

export interface DeploymentFamilyLimits {
  readonly artifacts: number;
  readonly cloud: number;
  readonly entrypoints: number;
  readonly instances: number;
  readonly k8sRelationships: number;
  readonly k8sResources: number;
  readonly networkPaths: number;
  readonly platformPlacements: number;
  readonly provisionedPlatforms: number;
  readonly sources: number;
  readonly topologyEdges: number;
}

const SUMMARY_LIMITS: DeploymentFamilyLimits = {
  artifacts: 3,
  cloud: 1,
  entrypoints: 1,
  instances: 6,
  k8sRelationships: 2,
  k8sResources: 4,
  networkPaths: 1,
  platformPlacements: 6,
  provisionedPlatforms: 2,
  sources: 2,
  topologyEdges: 20,
};

const EXPANDED_LIMITS: DeploymentFamilyLimits = {
  artifacts: 4,
  cloud: 1,
  entrypoints: 1,
  instances: 14,
  k8sRelationships: 4,
  k8sResources: 4,
  networkPaths: 1,
  platformPlacements: 12,
  provisionedPlatforms: 4,
  sources: 3,
  topologyEdges: 50,
};

export function deploymentFamilyLimits(detail: DeploymentGraphDetail): DeploymentFamilyLimits {
  return detail === "expanded" ? EXPANDED_LIMITS : SUMMARY_LIMITS;
}

export function deploymentGraphBounds(detail: DeploymentGraphDetail): {
  readonly maxEdges: number;
  readonly maxNodes: number;
} {
  return detail === "expanded" ? { maxEdges: 90, maxNodes: 60 } : { maxEdges: 48, maxNodes: 48 };
}
