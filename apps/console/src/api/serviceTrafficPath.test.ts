import type { ServiceDeploymentLane } from "./serviceSpotlight";
import { buildServiceTrafficPaths } from "./serviceTrafficPath";

const lanes: readonly ServiceDeploymentLane[] = [
  {
    environments: ["prod"],
    evidenceCount: 4,
    label: "ECS lane",
    relationshipTypes: ["DEPLOYS_TO"],
    resolvedCount: 1,
    sourceRepos: ["terraform-stack-node10"]
  }
];

describe("buildServiceTrafficPaths", () => {
  it("uses bounded network path records from the service dossier before inferred edge evidence", () => {
    const paths = buildServiceTrafficPaths(
      {
        edge_runtime_evidence: {
          cloudfront_distributions: [
            {
              aliases: ["fallback.example.com"],
              id: "fallback-distribution",
              origins: [{ id: "fallback-origin" }]
            }
          ]
        },
        network_paths: [
          {
            environment: "prod",
            from: "catalog-api.prod.example.internal",
            from_type: "hostname",
            path_type: "hostname_to_runtime",
            platform_kind: "ecs",
            reason: "content_hostname_reference",
            to: "prod-ecs",
            to_type: "runtime_platform",
            visibility: "public"
          }
        ]
      },
      "catalog-api",
      lanes
    );

    expect(paths).toEqual([
      {
        edge: "Public hostname",
        environment: "prod",
        evidenceKind: "hostname_to_runtime",
        hostname: "catalog-api.prod.example.internal",
        origin: "runtime platform",
        reason: "content_hostname_reference",
        runtime: "prod-ecs",
        sourceRepo: "terraform-stack-node10",
        visibility: "public",
        workload: "catalog-api"
      }
    ]);
  });

  it("turns API Gateway custom-domain edge evidence into a traffic path", () => {
    const paths = buildServiceTrafficPaths(
      {
        edge_runtime_evidence: {
          api_gateway_domains: [
            {
              api_kind: "v2",
              api_mappings: [{ api_id: "api-123", stage: "prod" }],
              certificate_arns: ["arn:aws:acm:us-east-1:123:certificate/cert-1"],
              domain_name: "catalog-api.prod.example.internal",
              regional_domain_name: "d-api.execute-api.us-east-1.amazonaws.com"
            }
          ]
        }
      },
      "catalog-api",
      lanes
    );

    expect(paths).toEqual([
      {
        edge: "API Gateway v2",
        environment: "prod",
        evidenceKind: "aws_apigateway_domain_name",
        hostname: "catalog-api.prod.example.internal",
        origin: "d-api.execute-api.us-east-1.amazonaws.com",
        reason: "custom domain maps to API api-123",
        runtime: "prod",
        sourceRepo: "terraform-stack-node10",
        visibility: "public",
        workload: "catalog-api"
      }
    ]);
  });

  it("turns CloudFront edge runtime evidence into a readable traffic path", () => {
    const paths = buildServiceTrafficPaths(
      {
        edge_runtime_evidence: {
          cloudfront_distributions: [
            {
              aliases: ["catalog-api.prod.example.internal"],
              domain_name: "d123.cloudfront.net",
              id: "E123",
              origins: [{ domain_name: "origin-alb-primary", id: "origin-alb-primary" }]
            }
          ]
        }
      },
      "catalog-api",
      lanes
    );

    expect(paths).toEqual([
      {
        edge: "E123",
        environment: "prod",
        evidenceKind: "aws_cloudfront_distribution",
        hostname: "catalog-api.prod.example.internal",
        origin: "origin-alb-primary",
        reason: "CloudFront distribution E123",
        runtime: "prod",
        sourceRepo: "terraform-stack-node10",
        visibility: "public",
        workload: "catalog-api"
      }
    ]);
  });
});
