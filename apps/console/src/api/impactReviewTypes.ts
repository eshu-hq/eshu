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
  readonly input: Required<Pick<ImpactReviewInput, "target" | "targetKind">> &
    Pick<ImpactReviewInput, "environment" | "repoId"> & {
      readonly limit: number;
      readonly maxDepth: number;
    };
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
  readonly deploymentSources: readonly DeploymentTraceEntity[];
  readonly imageRefs: readonly string[];
  readonly k8sResources: readonly DeploymentTraceEntity[];
  readonly serviceName: string;
  readonly story: string;
  readonly workloadId: string;
}

export interface DeploymentTraceEntity {
  readonly detail?: string;
  readonly id?: string;
  readonly kind?: string;
  readonly name: string;
}

export type BlastTargetType =
  | "crossplane_xrd"
  | "repository"
  | "sql_table"
  | "terraform_module";
