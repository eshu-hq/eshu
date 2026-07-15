import type { EshuApiClient } from "../api/client";
import {
  loadAwsRuntimeDriftFindings,
  loadCloudRuntimeDriftFindings,
  loadTerraformImportPlanCandidates,
  loadUnmanagedCloudResources,
  type AwsRuntimeDriftPage,
  type CloudDriftQuery,
  type CloudRuntimeDriftPage,
  type TerraformImportPlanPage,
  type UnmanagedCloudResourcesPage,
} from "../api/cloudDrift";

export interface DriftState {
  readonly aws: AwsRuntimeDriftPage | null;
  readonly importPlan: TerraformImportPlanPage | null;
  readonly multi: CloudRuntimeDriftPage | null;
  readonly unmanaged: UnmanagedCloudResourcesPage | null;
}

export interface DriftSurfaceErrors {
  readonly aws: string;
  readonly importPlan: string;
  readonly multi: string;
  readonly unmanaged: string;
}

export const EMPTY_DRIFT_STATE: DriftState = {
  aws: null,
  importPlan: null,
  multi: null,
  unmanaged: null,
};

export const EMPTY_DRIFT_ERRORS: DriftSurfaceErrors = {
  aws: "",
  importPlan: "",
  multi: "",
  unmanaged: "",
};

export async function loadCloudDriftSurfaces(
  client: EshuApiClient,
  query: CloudDriftQuery,
  awsEnabled: boolean,
): Promise<{ readonly state: DriftState; readonly errors: DriftSurfaceErrors }> {
  const disabled = Promise.resolve<null>(null);
  const [multi, aws, unmanaged, importPlan] = await Promise.allSettled([
    loadCloudRuntimeDriftFindings(client, query),
    awsEnabled ? loadAwsRuntimeDriftFindings(client, query) : disabled,
    awsEnabled ? loadUnmanagedCloudResources(client, query) : disabled,
    awsEnabled ? loadTerraformImportPlanCandidates(client, query) : disabled,
  ]);
  return {
    state: {
      multi: valueOf(multi),
      aws: valueOf(aws),
      unmanaged: valueOf(unmanaged),
      importPlan: valueOf(importPlan),
    },
    errors: {
      multi: errorOf(multi),
      aws: errorOf(aws),
      unmanaged: errorOf(unmanaged),
      importPlan: errorOf(importPlan),
    },
  };
}

function valueOf<T>(result: PromiseSettledResult<T>): T | null {
  return result.status === "fulfilled" ? result.value : null;
}

function errorOf(result: PromiseSettledResult<unknown>): string {
  if (result.status === "fulfilled") return "";
  return result.reason instanceof Error ? result.reason.message : "request failed";
}
