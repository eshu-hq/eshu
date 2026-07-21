import { summaryNode } from "./eshuGraphDeploymentPresentation";
import type {
  DeploymentCollectionLimits,
  DeploymentTraceResponse,
  ServiceDeploymentContextResponse,
} from "./eshuGraphDeploymentWire";
import type { GraphNode } from "../console/types";

interface CollectionStatus {
  readonly incomplete: boolean;
  readonly text: string;
}

export function sourceCompletenessSummary(
  context: ServiceDeploymentContextResponse,
  trace: DeploymentTraceResponse,
): GraphNode | null {
  if (!hasCompletenessMetadata(context, trace)) return null;

  const runtime = trace.runtime_topology_limits ?? context.runtime_topology_limits;
  const statuses = [
    context.result_limits !== undefined || runtime?.instances === undefined
      ? contextLimitsStatus("workload instances", context.result_limits, context.instances?.length)
      : null,
    limitsStatus("runtime instances", runtime?.instances, trace.instances?.length),
    limitsStatus("platform edges", runtime?.platform_edges),
    limitsStatus("provisioned platforms", runtime?.provisioned_platforms),
    limitsStatus(
      "deployment sources",
      trace.deployment_source_limits,
      trace.deployment_sources?.length,
    ),
    limitsStatus("cloud resources", trace.cloud_resource_limits, trace.cloud_resources?.length),
    limitsStatus("Kubernetes resources", trace.k8s_resource_limits, trace.k8s_resources?.length),
  ].filter((status): status is CollectionStatus => status !== null);
  if (statuses.length === 0) return null;

  return summaryNode(
    "source_bounds",
    statuses.some((status) => status.incomplete)
      ? "Source API returned incomplete deployment evidence"
      : "Source API completeness is unverified",
    statuses.map((status) => status.text).join(" · "),
  );
}

function hasCompletenessMetadata(
  context: ServiceDeploymentContextResponse,
  trace: DeploymentTraceResponse,
): boolean {
  return (
    context.result_limits !== undefined ||
    context.runtime_topology_limits !== undefined ||
    trace.runtime_topology_limits !== undefined ||
    trace.deployment_source_limits !== undefined ||
    trace.cloud_resource_limits !== undefined ||
    trace.k8s_resource_limits !== undefined
  );
}

function contextLimitsStatus(
  label: string,
  limits: ServiceDeploymentContextResponse["result_limits"],
  returnedCount: number | undefined,
): CollectionStatus | null {
  if (!limits) return missingLimits(label);
  const observedCount = limits.instance_count ?? limits.observed_count;
  return limitsStatus(
    label,
    {
      ...limits,
      observed_count: observedCount,
      observed_count_is_lower_bound: false,
      returned_count: limits.returned_count ?? returnedCount,
    },
    returnedCount,
  );
}

function limitsStatus(
  label: string,
  limits: DeploymentCollectionLimits | undefined,
  returnedCount?: number,
): CollectionStatus | null {
  if (!limits) return missingLimits(label);
  const returned = limits.returned_count ?? returnedCount;
  const observed = limits.observed_count;
  if (
    returned === undefined ||
    observed === undefined ||
    limits.truncated === undefined ||
    limits.observed_count_is_lower_bound === undefined
  ) {
    return missingLimits(label);
  }
  const expectedTruncation = limits.observed_count_is_lower_bound || returned < observed;
  if (limits.truncated !== expectedTruncation || returned > observed) {
    return { incomplete: false, text: `${label}: completeness metadata is inconsistent` };
  }
  const incomplete =
    limits.truncated === true ||
    limits.observed_count_is_lower_bound === true ||
    returned < observed;
  if (!incomplete) return null;

  const returnedText = `returned ${returned}`;
  const observedText = `observed ${limits.observed_count_is_lower_bound ? "at least " : ""}${observed}`;
  return { incomplete: true, text: `${label}: ${returnedText}, ${observedText}, truncated` };
}

function missingLimits(label: string): CollectionStatus {
  return { incomplete: false, text: `${label}: completeness metadata unavailable` };
}
