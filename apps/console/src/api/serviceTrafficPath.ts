import type { ServiceDeploymentLane } from "./serviceSpotlight";

export interface ServiceTrafficPath {
  readonly edge: string;
  readonly environment: string;
  readonly evidenceKind: string;
  readonly hostname: string;
  readonly origin: string;
  readonly reason: string;
  readonly runtime: string;
  readonly sourceRepo: string;
  readonly visibility: string;
  readonly workload: string;
}

export interface ServiceTrafficPathContext {
  readonly edge_runtime_evidence?: {
    readonly api_gateway_domains?: readonly APIGatewayDomainRecord[];
    readonly apigateway_domains?: readonly APIGatewayDomainRecord[];
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

interface APIGatewayDomainRecord {
  readonly api_kind?: string;
  readonly api_mappings?: readonly {
    readonly api_id?: string;
    readonly stage?: string;
  }[];
  readonly certificate_arns?: readonly string[];
  readonly distribution_domain_name?: string;
  readonly domain_name?: string;
  readonly regional_domain_name?: string;
}

interface NetworkPathRecord {
  readonly environment?: string;
  readonly from?: string;
  readonly from_type?: string;
  readonly path_type?: string;
  readonly platform_kind?: string;
  readonly reason?: string;
  readonly to?: string;
  readonly to_type?: string;
  readonly visibility?: string;
  readonly workload?: string;
}

export function buildServiceTrafficPaths(
  context: ServiceTrafficPathContext,
  serviceName: string,
  lanes: readonly ServiceDeploymentLane[]
): readonly ServiceTrafficPath[] {
  if ((context.network_paths?.length ?? 0) > 0) {
    return (context.network_paths ?? []).map((path) => ({
      edge: networkEdgeLabel(path),
      environment: nonEmpty(path.environment, firstEnvironment(lanes)),
      evidenceKind: nonEmpty(path.path_type, "network_path"),
      hostname: nonEmpty(path.from, firstHostname(context), serviceName),
      origin: networkOriginLabel(path),
      reason: nonEmpty(path.reason, "network path evidence"),
      runtime: nonEmpty(path.to, firstRuntime(lanes)),
      sourceRepo: firstSourceRepo(lanes),
      visibility: nonEmpty(path.visibility, "visibility pending"),
      workload: nonEmpty(path.workload, serviceName)
    }));
  }

  const apiGatewayDomains = [
    ...(context.edge_runtime_evidence?.api_gateway_domains ?? []),
    ...(context.edge_runtime_evidence?.apigateway_domains ?? [])
  ];
  if (apiGatewayDomains.length > 0) {
    return apiGatewayDomains.map((domain) => {
      const mapping = domain.api_mappings?.[0];
      return {
        edge: `API Gateway ${nonEmpty(domain.api_kind, "domain")}`,
        environment: nonEmpty(mapping?.stage, firstEnvironment(lanes)),
        evidenceKind: "aws_apigateway_domain_name",
        hostname: nonEmpty(domain.domain_name, firstHostname(context), serviceName),
        origin: nonEmpty(
          domain.regional_domain_name,
          domain.distribution_domain_name,
          "origin pending"
        ),
        reason: mapping?.api_id !== undefined && mapping.api_id.trim().length > 0
          ? `custom domain maps to API ${mapping.api_id}`
          : "API Gateway custom domain evidence",
        runtime: firstRuntime(lanes),
        sourceRepo: firstSourceRepo(lanes),
        visibility: "public",
        workload: serviceName
      };
    });
  }

  const distributions = [
    ...(context.edge_runtime_evidence?.cloudfront_distributions ?? []),
    ...(context.edge_runtime_evidence?.distributions ?? [])
  ];
  return distributions.map((distribution) => ({
    edge: nonEmpty(distribution.id, distribution.domain_name, "CloudFront distribution"),
    environment: firstEnvironment(lanes),
    evidenceKind: "aws_cloudfront_distribution",
    hostname: nonEmpty(distribution.aliases?.[0], firstHostname(context), distribution.domain_name),
    origin: nonEmpty(
      distribution.origins?.[0]?.id,
      distribution.origins?.[0]?.domain_name,
      "origin pending"
    ),
    reason: `CloudFront distribution ${nonEmpty(distribution.id, distribution.domain_name)}`,
    runtime: firstRuntime(lanes),
    sourceRepo: firstSourceRepo(lanes),
    visibility: "public",
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

function firstEnvironment(lanes: readonly ServiceDeploymentLane[]): string {
  return nonEmpty(lanes[0]?.environments[0], "environment pending");
}

function firstSourceRepo(lanes: readonly ServiceDeploymentLane[]): string {
  return nonEmpty(lanes.flatMap((lane) => lane.sourceRepos)[0], "source repo pending");
}

function networkEdgeLabel(path: NetworkPathRecord): string {
  const visibility = nonEmpty(path.visibility);
  const fromType = nonEmpty(path.from_type, "entrypoint").replace(/_/g, " ");
  return `${visibility.length > 0 ? `${capitalize(visibility)} ` : ""}${fromType}`;
}

function networkOriginLabel(path: NetworkPathRecord): string {
  return nonEmpty(path.to_type, "runtime target").replace(/_/g, " ");
}

function capitalize(value: string): string {
  return value.length === 0 ? value : value[0].toUpperCase() + value.slice(1);
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}
