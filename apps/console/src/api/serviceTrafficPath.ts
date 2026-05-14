import type { ServiceDeploymentLane } from "./serviceSpotlight";

export interface ServiceTrafficPath {
  readonly edge: string;
  readonly evidenceKind: string;
  readonly hostname: string;
  readonly origin: string;
  readonly runtime: string;
  readonly sourceRepo: string;
  readonly workload: string;
}

export interface ServiceTrafficPathContext {
  readonly edge_runtime_evidence?: {
    readonly cloudfront_distributions?: readonly CloudFrontDistributionRecord[];
    readonly distributions?: readonly CloudFrontDistributionRecord[];
  };
  readonly hostnames?: readonly {
    readonly hostname?: string;
  }[];
  readonly network_paths?: readonly NetworkPathRecord[];
}

interface CloudFrontDistributionRecord {
  readonly aliases?: readonly string[];
  readonly domain_name?: string;
  readonly id?: string;
  readonly origins?: readonly {
    readonly domain_name?: string;
    readonly id?: string;
  }[];
}

interface NetworkPathRecord {
  readonly edge?: string;
  readonly evidence_kind?: string;
  readonly hostname?: string;
  readonly origin?: string;
  readonly runtime?: string;
  readonly source_repo?: string;
  readonly workload?: string;
}

export function buildServiceTrafficPaths(
  context: ServiceTrafficPathContext,
  serviceName: string,
  lanes: readonly ServiceDeploymentLane[]
): readonly ServiceTrafficPath[] {
  if ((context.network_paths?.length ?? 0) > 0) {
    return (context.network_paths ?? []).map((path) => ({
      edge: nonEmpty(path.edge, "edge evidence pending"),
      evidenceKind: nonEmpty(path.evidence_kind, "network_path"),
      hostname: nonEmpty(path.hostname, firstHostname(context), serviceName),
      origin: nonEmpty(path.origin, "origin pending"),
      runtime: nonEmpty(path.runtime, firstRuntime(lanes)),
      sourceRepo: nonEmpty(path.source_repo, firstSourceRepo(lanes)),
      workload: nonEmpty(path.workload, serviceName)
    }));
  }

  const distributions = [
    ...(context.edge_runtime_evidence?.cloudfront_distributions ?? []),
    ...(context.edge_runtime_evidence?.distributions ?? [])
  ];
  return distributions.map((distribution) => ({
    edge: nonEmpty(distribution.id, distribution.domain_name, "CloudFront distribution"),
    evidenceKind: "aws_cloudfront_distribution",
    hostname: nonEmpty(distribution.aliases?.[0], firstHostname(context), distribution.domain_name),
    origin: nonEmpty(
      distribution.origins?.[0]?.id,
      distribution.origins?.[0]?.domain_name,
      "origin pending"
    ),
    runtime: firstRuntime(lanes),
    sourceRepo: firstSourceRepo(lanes),
    workload: serviceName
  }));
}

function firstHostname(context: ServiceTrafficPathContext): string {
  return nonEmpty(context.hostnames?.[0]?.hostname);
}

function firstRuntime(lanes: readonly ServiceDeploymentLane[]): string {
  const lane = lanes[0];
  return nonEmpty(lane?.environments[0], lane?.label, "runtime pending");
}

function firstSourceRepo(lanes: readonly ServiceDeploymentLane[]): string {
  return nonEmpty(lanes.flatMap((lane) => lane.sourceRepos)[0], "source repo pending");
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}
