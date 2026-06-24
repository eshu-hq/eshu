import { describe, expect, it, vi } from "vitest";

import { EshuApiClient } from "./client";
import {
  deploymentConfigInfluenceFromResponse,
  loadDeploymentConfigInfluence
} from "./deploymentConfigInfluence";

describe("deploymentConfigInfluenceFromResponse", () => {
  it("normalizes deployment configuration influence into an audit trail", () => {
    const influence = deploymentConfigInfluenceFromResponse({
      coverage: {
        limit: 10,
        query_shape: "deployment_config_influence_story",
        truncated: false
      },
      image_tag_sources: [
        {
          evidence_kind: "helm_values_reference",
          matched_alias: "image.tag",
          matched_value: "ghcr.io/acme/catalog-api:1.2.3",
          relative_path: "clusters/prod/catalog-api/values.yaml",
          repo_id: "repository:iac-eks-argocd",
          repo_name: "iac-eks-argocd",
          start_line: 17
        }
      ],
      influencing_repositories: [
        {
          repo_name: "catalog-api",
          roles: ["service_owner"]
        },
        {
          repo_name: "iac-eks-argocd",
          roles: ["deployment_source", "configuration_artifact"]
        }
      ],
      read_first_files: [
        {
          evidence_kind: "helm_values_reference",
          next_call: "get_file_lines",
          relative_path: "clusters/prod/catalog-api/values.yaml",
          repo_id: "repository:iac-eks-argocd",
          repo_name: "iac-eks-argocd",
          start_line: 17
        }
      ],
      rendered_targets: [
        {
          kind: "Deployment",
          name: "catalog-api",
          namespace: "items"
        }
      ],
      resource_limit_sources: [
        {
          evidence_kind: "kubernetes_resource_limit",
          matched_alias: "resources.limits.cpu",
          matched_value: "500m",
          relative_path: "charts/catalog-api/templates/deployment.yaml",
          repo_name: "helm-charts"
        }
      ],
      runtime_setting_sources: [],
      service_name: "catalog-api",
      story: "catalog-api is influenced by 1 values layer.",
      values_layers: [
        {
          evidence_kind: "helm_values_reference",
          relative_path: "clusters/prod/catalog-api/values.yaml",
          repo_name: "iac-eks-argocd"
        }
      ]
    });

    expect(influence.serviceName).toBe("catalog-api");
    expect(influence.summary).toContain("1 values layer");
    expect(influence.coverage).toEqual({
      limit: 10,
      queryShape: "deployment_config_influence_story",
      truncated: false
    });
    expect(influence.repositories).toEqual([
      {
        name: "catalog-api",
        roles: ["service_owner"]
      },
      {
        name: "iac-eks-argocd",
        roles: ["configuration_artifact", "deployment_source"]
      }
    ]);
    expect(influence.sections.map((section) => section.label)).toEqual([
      "Values layers",
      "Image tags",
      "Runtime settings",
      "Resource limits",
      "Rendered targets",
      "Read first"
    ]);
    expect(influence.sections[1].items[0]).toMatchObject({
      label: "image.tag",
      path: "clusters/prod/catalog-api/values.yaml",
      repoName: "iac-eks-argocd",
      value: "ghcr.io/acme/catalog-api:1.2.3"
    });
    expect(influence.sections[5].items[0]).toMatchObject({
      action: "get_file_lines",
      line: 17,
      path: "clusters/prod/catalog-api/values.yaml"
    });
  });
});

describe("loadDeploymentConfigInfluence", () => {
  it("calls the bounded deployment config influence contract", async () => {
    const fetcher = vi.fn(async () =>
      new Response(
        JSON.stringify({
          data: {
            coverage: { limit: 25, truncated: false },
            service_name: "catalog-api",
            story: "config influence story"
          },
          error: null,
          truth: {
            basis: "hybrid",
            capability: "platform_impact.deployment_config_influence",
            freshness: { state: "fresh" },
            level: "derived",
            profile: "local_authoritative"
          }
        }),
        { status: 200 }
      )
    );
    const client = new EshuApiClient({
      apiKey: "secret",
      baseUrl: "http://eshu.test",
      fetcher
    });

    await loadDeploymentConfigInfluence(client, {
      environment: "prod",
      serviceName: "catalog-api"
    });

    expect(fetcher).toHaveBeenCalledWith(
      "http://eshu.test/api/v0/impact/deployment-config-influence",
      expect.objectContaining({
        body: JSON.stringify({
          environment: "prod",
          limit: 25,
          service_name: "catalog-api"
        }),
        method: "POST"
      })
    );
  });
});
