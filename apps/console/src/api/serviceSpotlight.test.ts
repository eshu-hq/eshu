import { describe, expect, it } from "vitest";
import { serviceSpotlightFromContext, type ServiceContextResponse } from "./serviceSpotlight";

describe("serviceSpotlightFromContext", () => {
  it("keeps Terraform config access out of deployment lane sources", () => {
    const spotlight = serviceSpotlightFromContext(serviceContext, "api-node-boats");

    expect(spotlight.lanes).toEqual([
      expect.objectContaining({
        evidenceCount: 3,
        label: "Kubernetes",
        relationshipTypes: ["DEPLOYS_FROM"],
        sourceRepos: ["iac-eks-argocd", "helm-charts"]
      }),
      expect.objectContaining({
        evidenceCount: 1,
        label: "ECS",
        relationshipTypes: ["PROVISIONS_DEPENDENCY_FOR"],
        sourceRepos: ["terraform-stack-node10"]
      })
    ]);

    const ecsLane = spotlight.lanes.find((lane) => lane.label === "ECS");
    expect(ecsLane?.sourceRepos).not.toContain("terraform-stack-boattrader");
    expect(ecsLane?.relationshipTypes).not.toContain("READS_CONFIG_FROM");

    const configAccess = spotlight.relationshipClusters.find((cluster) =>
      cluster.kind === "configuration_access"
    );
    expect(configAccess).toEqual(expect.objectContaining({
      label: "Configuration access",
      relationshipTypes: ["READS_CONFIG_FROM"],
      technology: "terraform"
    }));
    expect(configAccess?.repositories.map((repo) => repo.repository)).toContain(
      "terraform-stack-boattrader"
    );

    const deployment = spotlight.relationshipClusters.find((cluster) =>
      cluster.kind === "deployment"
    );
    expect(deployment?.repositories.find((repo) =>
      repo.repository === "helm-charts"
    )?.technology).toBe("helm");
  });

  it("filters explicit service lanes through artifact semantics", () => {
    const spotlight = serviceSpotlightFromContext({
      ...serviceContext,
      deployment_lanes: [
        {
          environments: ["bg-dev", "bg-prod"],
          lane_type: "ecs",
          relationship_types: ["PROVISIONS_DEPENDENCY_FOR", "READS_CONFIG_FROM"],
          resolved_ids: ["ecs-service", "iam-permission"],
          source_repositories: ["terraform-stack-node10", "terraform-stack-boattrader"]
        }
      ]
    }, "api-node-boats");

    expect(spotlight.lanes).toEqual([
      expect.objectContaining({
        evidenceCount: 1,
        label: "ECS",
        relationshipTypes: ["PROVISIONS_DEPENDENCY_FOR"],
        sourceRepos: ["terraform-stack-node10"]
      })
    ]);
  });
});

const serviceContext: ServiceContextResponse = {
  deployment_evidence: {
    artifacts: [
      {
        artifact_family: "kustomize",
        evidence_kind: "KUSTOMIZE_RESOURCE_REFERENCE",
        path: "applicationsets/api-node/kustomization.yaml",
        relationship_type: "DEPLOYS_FROM",
        source_repo_name: "iac-eks-argocd",
        target_repo_name: "api-node-boats"
      },
      {
        artifact_family: "helm",
        evidence_kind: "HELM_VALUES_REFERENCE",
        path: "argocd/api-node-boats/overlays/bg-qa/values.yaml",
        relationship_type: "DEPLOYS_FROM",
        source_repo_name: "helm-charts",
        target_repo_name: "api-node-boats"
      },
      {
        artifact_family: "kustomize",
        evidence_kind: "KUSTOMIZE_RESOURCE_REFERENCE",
        path: "api-node-platform/files/base.json",
        relationship_type: "DEPLOYS_FROM",
        source_repo_name: "helm-charts",
        target_repo_name: "api-node-boats"
      },
      {
        artifact_family: "terraform",
        evidence_kind: "TERRAFORM_ECS_SERVICE",
        path: "environments/bg-dev/ecs.tf",
        relationship_type: "PROVISIONS_DEPENDENCY_FOR",
        source_repo_name: "terraform-stack-node10",
        target_repo_name: "api-node-boats"
      },
      {
        artifact_family: "terraform",
        evidence_kind: "TERRAFORM_IAM_PERMISSION",
        path: "environments/bg-dev/resources.tf",
        relationship_type: "READS_CONFIG_FROM",
        source_repo_name: "terraform-stack-boattrader",
        target_repo_name: "api-node-boats"
      }
    ]
  },
  instances: [
    {
      environment: "bg-prod",
      platforms: [
        {
          platform_kind: "kubernetes",
          platform_name: "eks"
        }
      ]
    },
    {
      environment: "bg-dev",
      platforms: [
        {
          platform_kind: "ecs",
          platform_name: "ecs"
        }
      ]
    }
  ],
  name: "api-node-boats",
  repo_name: "api-node-boats"
};
