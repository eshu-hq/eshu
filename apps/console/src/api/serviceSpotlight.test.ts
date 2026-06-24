import { describe, expect, it } from "vitest";

import { serviceSpotlightFromContext, type ServiceContextResponse } from "./serviceSpotlight";

describe("serviceSpotlightFromContext", () => {
  it("normalizes service story support evidence without fabricating links", () => {
    const spotlight = serviceSpotlightFromContext({
      ...serviceContext,
      support_overview: {
        target_support: {
          ambiguous_count: 1,
          evidence: [
            {
              fact_id: "jira-123",
              fact_kind: "work_item.record",
              observed_at: "2026-06-07T11:33:48Z",
              payload: {
                issue_type_name: "Incident",
                provider: "jira_cloud",
                status_name: "In Progress",
                url_redacted: "https://jira.example.test/browse/PAY-123",
                work_item_key: "PAY-123"
              },
              scope_id: "jira:site:example",
              source_system: "jira"
            },
            {
              fact_id: "pd-service",
              fact_kind: "incident_routing.observed_pagerduty_service",
              payload: {
                outcome: "exact",
                provider: "pagerduty",
                service_id: "P-SVC",
                source_class: "observed",
                status: "active"
              },
              scope_id: "pagerduty:prod",
              source_system: "pagerduty"
            }
          ],
          evidence_count: 2,
          incident_routing_count: 1,
          missing_evidence: [],
          work_item_count: 1
        }
      }
    }, "catalog-api");

    expect(spotlight.support).toMatchObject({
      ambiguousCount: 1,
      evidenceCount: 2,
      incidentRoutingCount: 1,
      missingEvidence: [],
      workItemCount: 1
    });
    expect(spotlight.support?.evidence).toEqual([
      expect.objectContaining({
        factId: "jira-123",
        factKind: "work_item.record",
        issueType: "Incident",
        label: "Jira PAY-123",
        provider: "jira_cloud",
        sourceUrlText: "https://jira.example.test/browse/PAY-123",
        status: "In Progress"
      }),
      expect.objectContaining({
        factId: "pd-service",
        factKind: "incident_routing.observed_pagerduty_service",
        label: "PagerDuty routing",
        outcome: "exact",
        provider: "pagerduty",
        status: "active"
      })
    ]);
  });

  it("keeps Terraform config access out of deployment lane sources", () => {
    const spotlight = serviceSpotlightFromContext(serviceContext, "catalog-api");

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
    expect(ecsLane?.sourceRepos).not.toContain("terraform-stack-marketplace");
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
      "terraform-stack-marketplace"
    );

    const deployment = spotlight.relationshipClusters.find((cluster) =>
      cluster.kind === "deployment"
    );
    expect(deployment?.repositories.find((repo) =>
      repo.repository === "helm-charts"
    )?.technology).toBe("helm");
  });

  it("preserves relationship confidence provenance and stale state for console edge inspection", () => {
    const spotlight = serviceSpotlightFromContext(serviceContext, "catalog-api");

    const configAccess = spotlight.relationshipClusters.find((cluster) =>
      cluster.kind === "configuration_access"
    );
    const configRepo = configAccess?.repositories.find((repo) =>
      repo.repository === "terraform-stack-marketplace"
    );

    expect(configRepo).toEqual(expect.objectContaining({
      confidence: 0.64,
      evidenceCount: 2,
      provenanceMethod: "evidence_aggregate",
      rationale: "stale deployment evidence still references config access",
      state: "stale"
    }));
  });

  it("filters explicit service lanes through artifact semantics", () => {
    const spotlight = serviceSpotlightFromContext({
      ...serviceContext,
      deployment_lanes: [
        {
          environments: ["dev", "prod"],
          lane_type: "ecs",
          relationship_types: ["PROVISIONS_DEPENDENCY_FOR", "READS_CONFIG_FROM"],
          resolved_ids: ["ecs-service", "iam-permission"],
          source_repositories: ["terraform-stack-node10", "terraform-stack-marketplace"]
        }
      ]
    }, "catalog-api");

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
        target_repo_name: "catalog-api"
      },
      {
        artifact_family: "helm",
        evidence_kind: "HELM_VALUES_REFERENCE",
        path: "argocd/catalog-api/overlays/qa/values.yaml",
        relationship_type: "DEPLOYS_FROM",
        source_repo_name: "helm-charts",
        target_repo_name: "catalog-api"
      },
      {
        artifact_family: "kustomize",
        evidence_kind: "KUSTOMIZE_RESOURCE_REFERENCE",
        path: "platform-api/files/base.json",
        relationship_type: "DEPLOYS_FROM",
        source_repo_name: "helm-charts",
        target_repo_name: "catalog-api"
      },
      {
        artifact_family: "terraform",
        evidence_kind: "TERRAFORM_ECS_SERVICE",
        path: "environments/dev/ecs.tf",
        relationship_type: "PROVISIONS_DEPENDENCY_FOR",
        source_repo_name: "terraform-stack-node10",
        target_repo_name: "catalog-api"
      },
      {
        artifact_family: "terraform",
        confidence: 0.64,
        confidence_basis: "evidence_aggregate",
        evidence_count: 2,
        evidence_kind: "TERRAFORM_IAM_PERMISSION",
        rationale: "stale deployment evidence still references config access",
        path: "environments/dev/resources.tf",
        relationship_type: "READS_CONFIG_FROM",
        source_freshness: "stale",
        source_repo_name: "terraform-stack-marketplace",
        target_repo_name: "catalog-api"
      }
    ]
  },
  instances: [
    {
      environment: "prod",
      platforms: [
        {
          platform_kind: "kubernetes",
          platform_name: "eks"
        }
      ]
    },
    {
      environment: "dev",
      platforms: [
        {
          platform_kind: "ecs",
          platform_name: "ecs"
        }
      ]
    }
  ],
  name: "catalog-api",
  repo_name: "catalog-api"
};
