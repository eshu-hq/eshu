import type { ServiceDeploymentLane } from "./serviceSpotlight";
import { buildServiceTrafficPaths } from "./serviceTrafficPath";

const lanes: readonly ServiceDeploymentLane[] = [
  {
    environments: ["bg-prod"],
    evidenceCount: 4,
    evidenceKinds: ["terraform_task_definition"],
    label: "ECS lane",
    platform: "ecs",
    relationshipVerbs: ["DEPLOYS_TO"],
    sourceRepos: ["terraform-stack-node10"],
    targetLabel: "api-node-boats"
  }
];

describe("buildServiceTrafficPaths", () => {
  it("uses bounded network path records before inferred edge evidence", () => {
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
            edge: "CloudFront distribution",
            evidence_kind: "network_path",
            hostname: "api-node-boats.prod.bgrp.io",
            origin: "origin-alb-primary",
            runtime: "ECS bg-prod",
            source_repo: "terraform-stack-node10",
            workload: "api-node-boats"
          }
        ]
      },
      "api-node-boats",
      lanes
    );

    expect(paths).toEqual([
      {
        edge: "CloudFront distribution",
        evidenceKind: "network_path",
        hostname: "api-node-boats.prod.bgrp.io",
        origin: "origin-alb-primary",
        runtime: "ECS bg-prod",
        sourceRepo: "terraform-stack-node10",
        workload: "api-node-boats"
      }
    ]);
  });

  it("turns CloudFront edge runtime evidence into a readable traffic path", () => {
    const paths = buildServiceTrafficPaths(
      {
        edge_runtime_evidence: {
          cloudfront_distributions: [
            {
              aliases: ["api-node-boats.prod.bgrp.io"],
              domain_name: "d123.cloudfront.net",
              id: "E123",
              origins: [{ domain_name: "origin-alb-primary", id: "origin-alb-primary" }]
            }
          ]
        }
      },
      "api-node-boats",
      lanes
    );

    expect(paths).toEqual([
      {
        edge: "E123",
        evidenceKind: "aws_cloudfront_distribution",
        hostname: "api-node-boats.prod.bgrp.io",
        origin: "origin-alb-primary",
        runtime: "bg-prod",
        sourceRepo: "terraform-stack-node10",
        workload: "api-node-boats"
      }
    ]);
  });
});
