import { deploymentGraphFromStory } from "./deploymentGraph";
import type { ContextResponse, StoryResponse } from "./repository";

describe("deploymentGraphFromStory", () => {
  it("preserves typed relationship verbs instead of flattening every lane into deployment", () => {
    const story: StoryResponse = {
      deployment_overview: {
        workloads: ["api-node-boats"]
      },
      repository: {
        name: "api-node-boats"
      },
      subject: {
        id: "repository:r_api_node_boats",
        name: "api-node-boats",
        type: "repository"
      }
    };
    const context: ContextResponse = {
      deployment_evidence: {
        artifacts: [
          {
            artifact_family: "helm",
            environment: "bg-qa",
            evidence_kind: "HELM_VALUES_REFERENCE",
            path: "argocd/api-node-boats/values.yaml",
            relationship_type: "DEPLOYS_FROM",
            source_repo_name: "helm-charts"
          },
          {
            artifact_family: "terraform",
            environment: "bg-dev",
            evidence_kind: "TERRAFORM_ECS_SERVICE",
            path: "services/api-node-boats/ecs.tf",
            relationship_type: "PROVISIONS_DEPENDENCY_FOR",
            source_repo_name: "terraform-stack-node10"
          },
          {
            artifact_family: "terraform",
            environment: "bg-dev",
            evidence_kind: "TERRAFORM_SSM_PARAMETER",
            path: "parameters/api-node-boats.tf",
            relationship_type: "READS_CONFIG_FROM",
            source_repo_name: "terraform-stack-boattrader"
          }
        ]
      }
    };

    const graph = deploymentGraphFromStory(story, context);
    const labels = graph.links.map((link) => link.label);

    expect(labels).toEqual(expect.arrayContaining([
      "DEPLOYS_FROM",
      "PROVISIONS_DEPENDENCY_FOR",
      "READS_CONFIG_FROM"
    ]));
    expect(graph.links).not.toContainEqual(
      expect.objectContaining({
        label: "deploys from",
        target: "target:service"
      })
    );
  });
});
