import type { ChangeSurfaceInvestigation } from "./changeSurface";
import type { EshuTruth } from "./envelope";
import type { GraphModel } from "../console/types";

export type ImpactTargetKind =
  | "code_topic"
  | "crossplane_xrd"
  | "repository"
  | "resource"
  | "service"
  | "sql_table"
  | "terraform_module"
  | "workload";

export interface ImpactReviewInput {
  readonly environment?: string;
  readonly limit?: number;
  readonly maxDepth?: number;
  readonly repoId?: string;
  readonly target: string;
  readonly targetKind: ImpactTargetKind;
}

export interface ImpactReview {
  readonly blast: ImpactSection<BlastRadiusResult>;
  readonly changeSurface: ImpactSection<ChangeSurfaceInvestigation>;
  readonly deploymentTrace: ImpactSection<DeploymentTraceResult>;
  readonly graph: GraphModel;
  readonly graphPresentation: ImpactGraphPresentation;
  readonly input: Required<Pick<ImpactReviewInput, "target" | "targetKind">> &
    Pick<ImpactReviewInput, "environment" | "repoId"> & {
      readonly limit: number;
      readonly maxDepth: number;
    };
}

export type ImpactGraphMode = "blast_radius" | "change_surface" | "deployment_trace" | "empty";

export interface ImpactGraphPresentation {
  readonly compositionDurationMs: number;
  readonly duplicateEdges: number;
  readonly duplicateNodes: number;
  readonly edgeLimit: number;
  readonly freshness?: string;
  readonly inputEdges: number;
  readonly inputNodes: number;
  readonly limitations: readonly string[];
  readonly mode: ImpactGraphMode;
  readonly nodeLimit: number;
  readonly omittedEdges: number;
  readonly omittedNodes: number;
  readonly renderedEdges: number;
  readonly renderedNodes: number;
  readonly sourceApis: readonly string[];
  readonly title: string;
  readonly truncated: boolean;
  readonly truthBasis?: string;
  readonly truthLevel?: string;
}

export type ImpactSection<TData> =
  | {
      readonly data: TData;
      readonly source: string;
      readonly status: "ready";
      readonly truth: EshuTruth | null;
    }
  | {
      readonly error: string;
      readonly source: string;
      readonly status: "unavailable";
      readonly truth?: EshuTruth | null;
    }
  | {
      readonly reason: string;
      readonly source: string;
      readonly status: "skipped";
    };

export interface BlastRadiusResult {
  readonly affected: readonly BlastAffectedEntity[];
  readonly affectedCount: number;
  readonly graph: GraphModel;
  readonly limit: number;
  readonly target: string;
  readonly targetType: BlastTargetType;
  readonly truncated: boolean;
}

export interface BlastAffectedEntity {
  readonly claim?: string;
  readonly hops: number;
  readonly repo: string;
  readonly repoId?: string;
  readonly risk?: string;
  readonly tier?: string;
}

export interface DeploymentTraceResult {
  readonly cloudResources: readonly DeploymentTraceEntity[];
  readonly deploymentOverview: Record<string, unknown>;
  readonly deploymentFacts: readonly DeploymentTraceFact[];
  readonly deploymentSourceLimits: DeploymentSourceLimits | null;
  readonly deploymentSources: readonly DeploymentTraceSource[];
  readonly imageRefs: readonly string[];
  readonly invalidTopologyEdgeCount?: number;
  readonly k8sResources: readonly DeploymentTraceEntity[];
  readonly instances: readonly DeploymentTraceInstance[];
  readonly provisionedPlatforms: readonly DeploymentTracePlatform[];
  readonly repoId: string;
  readonly repoName: string;
  readonly serviceName: string;
  readonly story: string;
  readonly topologyEdges: readonly DeploymentTraceTopologyEdge[];
  readonly workloadId: string;
}

export interface DeploymentSourceLimits {
  readonly canonicalObservedCount: number;
  readonly limit: number;
  readonly observedCount: number;
  readonly observedCountIsLowerBound: boolean;
  readonly ordering: readonly string[];
  readonly querySentinelLimit: number;
  readonly repositoryObservedCount: number;
  readonly returnedCount: number;
  readonly truncated: boolean;
}

export interface DeploymentTraceFact {
  readonly confidence?: number;
  readonly kind?: string;
  readonly reason?: string;
  readonly target: string;
  readonly targetId?: string;
  readonly type: string;
}

export interface DeploymentTraceInstance {
  readonly environment?: string;
  readonly id: string;
  readonly platforms: readonly DeploymentTracePlatform[];
}

export interface DeploymentTracePlatform {
  readonly confidence?: number;
  readonly id?: string;
  readonly invalidTopologyEdgeCount?: number;
  readonly kind?: string;
  readonly name: string;
  readonly reason?: string;
  readonly topologyBasis?: DeploymentTraceTopologyBasis;
  readonly topologyEdges: readonly DeploymentTraceTopologyEdge[];
}

export type DeploymentTraceTopologyBasis = "direct_runtime" | "provisioning_fallback";

export interface DeploymentTraceTopologyEdge {
  readonly confidence?: number;
  readonly evidenceSource?: string;
  readonly reason?: string;
  readonly relationshipType: DeploymentTraceTopologyRelationship;
  readonly sourceId?: string;
  readonly sourceName?: string;
  readonly sourceTool?: string;
  readonly targetId?: string;
  readonly targetName?: string;
}

export type DeploymentTraceTopologyRelationship =
  | "DEFINES"
  | "INSTANCE_OF"
  | "PROVISIONS_DEPENDENCY_FOR"
  | "PROVISIONS_PLATFORM"
  | "RUNS_ON";

export interface DeploymentTraceEntity {
  readonly detail?: string;
  readonly id?: string;
  readonly kind?: string;
  readonly name: string;
}

export interface DeploymentTraceSource extends DeploymentTraceEntity {
  readonly relationshipType?: "DEPLOYMENT_SOURCE" | "DEPLOYS_FROM";
  readonly sourceId?: string;
  readonly targetId?: string;
}

export type BlastTargetType = "crossplane_xrd" | "repository" | "sql_table" | "terraform_module";
