import { fireEvent, render, screen, within } from "@testing-library/react";
import type { ServiceSpotlight } from "../api/serviceSpotlight";
import { ServiceRelationshipWorkbench } from "./ServiceRelationshipWorkbench";

describe("ServiceRelationshipWorkbench", () => {
  it("renders an interactive relationship graph with semantic drilldowns", () => {
    render(<ServiceRelationshipWorkbench spotlight={spotlight} />);

    expect(screen.getByRole("img", { name: "api-node-boats relationship map" }))
      .toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Deployment flow" }))
      .toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Config dependencies" }))
      .toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reset view" }))
      .toBeInTheDocument();

    const deploymentGraph = screen.getByRole("img", { name: "api-node-boats relationship map" });
    expect(within(deploymentGraph).getByText("api-node-boats")).toBeInTheDocument();
    expect(within(deploymentGraph).getByText("terraform-stack-node10")).toBeInTheDocument();
    expect(within(deploymentGraph).getByText("iac-eks-argocd")).toBeInTheDocument();
    expect(within(deploymentGraph).queryByText("terraform-stack-boattrader")).not.toBeInTheDocument();

    const terraformNode = within(deploymentGraph).getByRole("button", {
      name: /Inspect terraform-stack-node10 Terraform resource/i
    });
    expect(terraformNode).toHaveAttribute("data-draggable", "true");

    fireEvent.click(
      within(deploymentGraph).getByRole("button", {
        name: /Inspect DEPLOYS_FROM relationship from iac-eks-argocd/i
      })
    );

    const inspector = screen.getByRole("complementary", { name: "Relationship inspector" });
    expect(within(inspector).getByText("Selected relationship")).toBeInTheDocument();
    expect(within(inspector).getByText("DEPLOYS_FROM")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Config dependencies" }));

    const configGraph = screen.getByRole("img", { name: "api-node-boats relationship map" });
    expect(within(configGraph).getByText("terraform-stack-boattrader")).toBeInTheDocument();
    expect(within(configGraph).getByText("READS_CONFIG_FROM")).toBeInTheDocument();
    expect(within(configGraph).queryByText("iac-eks-argocd")).not.toBeInTheDocument();
  });
});

const spotlight: ServiceSpotlight = {
  api: {
    endpointCount: 38,
    endpoints: [],
    methodCount: 44,
    sourcePaths: []
  },
  consumers: [],
  dependencies: [],
  deploymentGraph: { links: [], nodes: [] },
  graphDependents: [],
  hostnames: [],
  investigation: {
    coverage: {
      reason: "bounded",
      repositoryCount: 3,
      repositoriesWithEvidence: 3,
      state: "partial",
      truncated: false
    },
    evidenceFamilies: [],
    findings: [],
    nextCalls: [],
    repositories: []
  },
  lanes: [
    {
      environments: ["bg-dev", "bg-prod", "bg-qa"],
      evidenceCount: 1,
      label: "ECS Terraform",
      relationshipTypes: ["PROVISIONS_DEPENDENCY_FOR"],
      resolvedCount: 1,
      sourceRepos: ["terraform-stack-node10"]
    },
    {
      environments: ["bg-prod"],
      evidenceCount: 2,
      label: "Kubernetes GitOps",
      relationshipTypes: ["DEPLOYS_FROM"],
      resolvedCount: 2,
      sourceRepos: ["iac-eks-argocd", "helm-charts"]
    }
  ],
  name: "api-node-boats",
  relationshipCounts: {
    downstream: 4,
    graphDependents: 2,
    references: 2,
    upstream: 1
  },
  relationshipClusters: [
    {
      description: "Repos and artifacts that deploy this service into a runtime.",
      evidenceCount: 2,
      kind: "deployment",
      label: "Deployment sources",
      relationshipTypes: ["DEPLOYS_FROM"],
      repositories: [
        {
          evidenceKinds: ["ARGOCD_APPLICATIONSET_DEPLOY_SOURCE"],
          paths: ["applicationsets/api-node/kustomization.yaml"],
          relationshipTypes: ["DEPLOYS_FROM"],
          repository: "iac-eks-argocd",
          technology: "argocd"
        },
        {
          evidenceKinds: ["HELM_VALUES_REFERENCE"],
          paths: ["argocd/api-node-boats/values.yaml"],
          relationshipTypes: ["DEPLOYS_FROM"],
          repository: "helm-charts",
          technology: "helm"
        }
      ],
      technology: "kubernetes"
    },
    {
      description: "Infrastructure resources that provision runtime dependencies for this service.",
      evidenceCount: 1,
      kind: "runtime_provisioning",
      label: "Runtime provisioning",
      relationshipTypes: ["PROVISIONS_DEPENDENCY_FOR"],
      repositories: [
        {
          evidenceKinds: ["TERRAFORM_ECS_SERVICE"],
          paths: ["environments/bg-dev/ecs.tf"],
          relationshipTypes: ["PROVISIONS_DEPENDENCY_FOR"],
          repository: "terraform-stack-node10",
          technology: "terraform"
        }
      ],
      technology: "terraform"
    },
    {
      description: "Repos that read, grant, or depend on this service's config.",
      evidenceCount: 1,
      kind: "configuration_access",
      label: "Configuration access",
      relationshipTypes: ["READS_CONFIG_FROM"],
      repositories: [
        {
          evidenceKinds: ["TERRAFORM_IAM_PERMISSION"],
          paths: ["environments/bg-dev/resources.tf"],
          relationshipTypes: ["READS_CONFIG_FROM"],
          repository: "terraform-stack-boattrader",
          technology: "terraform"
        }
      ],
      technology: "terraform"
    }
  ],
  repoName: "api-node-boats",
  summary: "api-node-boats service story.",
  trafficPaths: [],
  trust: {
    basis: "hybrid",
    freshness: "fresh",
    level: "derived",
    profile: "production"
  }
};
