import { fireEvent, render, screen, within } from "@testing-library/react";

import { ServiceRelationshipWorkbench } from "./ServiceRelationshipWorkbench";
import type { ServiceSpotlight } from "../api/serviceSpotlight";

describe("ServiceRelationshipWorkbench", () => {
  it("renders an interactive relationship graph with semantic drilldowns", () => {
    render(<ServiceRelationshipWorkbench spotlight={spotlight} />);

    expect(screen.getByRole("img", { name: "catalog-api relationship map" }))
      .toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Deployment flow" }))
      .toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Config dependencies" }))
      .toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reset view" }))
      .toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Zoom in" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Zoom out" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Expand graph widget" }))
      .toHaveAttribute("aria-pressed", "false");

    const deploymentGraph = screen.getByRole("img", { name: "catalog-api relationship map" });
    const workbench = screen.getByRole("region", { name: "Interactive relationship story" });
    const mapStage = screen.getByTestId("relationship-map-stage");
    const viewport = within(deploymentGraph).getByTestId("relationship-map-viewport");
    expect(within(mapStage).getByRole("img", { name: "catalog-api relationship map" }))
      .toBeInTheDocument();
    expect(within(mapStage).getByRole("complementary", { name: "Relationship inspector" }))
      .toBeInTheDocument();
    expect(viewport).toHaveAttribute("transform", "translate(0 0) scale(0.8)");

    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    expect(viewport).toHaveAttribute("transform", "translate(0 0) scale(1.05)");

    fireEvent.click(screen.getByRole("button", { name: "Reset view" }));
    expect(viewport).toHaveAttribute("transform", "translate(0 0) scale(0.8)");

    expect(workbench).not.toHaveClass("relationship-workbench-expanded");
    fireEvent.click(screen.getByRole("button", { name: "Expand graph widget" }));
    expect(workbench).toHaveClass("relationship-workbench-expanded");
    expect(screen.getByRole("button", { name: "Collapse graph widget" }))
      .toHaveAttribute("aria-pressed", "true");

    expect(within(deploymentGraph).getByText("catalog-api")).toBeInTheDocument();
    expect(within(deploymentGraph).getByText("terraform-stack-node10")).toBeInTheDocument();
    expect(within(deploymentGraph).getByText("iac-eks-argocd")).toBeInTheDocument();
    expect(within(deploymentGraph).queryByText("terraform-stack-marketplace")).not.toBeInTheDocument();

    const terraformNode = within(deploymentGraph).getByRole("button", {
      name: /Inspect terraform-stack-node10 Terraform resource/i
    });
    expect(terraformNode).toHaveAttribute("data-draggable", "true");
    fireEvent.click(terraformNode);

    const terraformInspector = screen.getByRole("complementary", { name: "Relationship inspector" });
    expect(within(terraformInspector).getByRole("tab", { name: "Summary" }))
      .toHaveAttribute("aria-selected", "true");
    expect(within(terraformInspector).getByText(/provisions runtime dependencies for catalog-api/i))
      .toBeInTheDocument();
    expect(within(terraformInspector).queryByText(/PROVISIONS_DEPENDENCY_FOR/))
      .not.toBeInTheDocument();

    fireEvent.click(within(terraformInspector).getByRole("tab", { name: "Facts" }));
    expect(within(terraformInspector).getByText(/PROVISIONS_DEPENDENCY_FOR/)).toBeInTheDocument();
    expect(within(terraformInspector).getByText(/READS_CONFIG_FROM/)).toBeInTheDocument();
    expect(within(terraformInspector).getByText(/TERRAFORM_ECS_SERVICE/)).toBeInTheDocument();

    fireEvent.click(within(terraformInspector).getByRole("tab", { name: "Evidence paths" }));
    expect(within(terraformInspector).getByText("environments/dev/ecs.tf")).toBeInTheDocument();

    fireEvent.click(
      within(deploymentGraph).getByRole("button", {
        name: /Inspect DEPLOYS_FROM relationship from iac-eks-argocd/i
      })
    );

    const inspector = screen.getByRole("complementary", { name: "Relationship inspector" });
    const selectionSummary = screen.getByRole("status", { name: "Selected relationship" });
    expect(within(selectionSummary).getByText("DEPLOYS_FROM")).toBeInTheDocument();
    expect(within(selectionSummary).getByText(/iac-eks-argocd -> catalog-api/i))
      .toBeInTheDocument();
    expect(within(inspector).getByText("Selected relationship")).toBeInTheDocument();
    expect(within(inspector).getByRole("tab", { name: "Summary" }))
      .toHaveAttribute("aria-selected", "true");
    expect(within(inspector).getByText("DEPLOYS_FROM")).toBeInTheDocument();
    expect(within(inspector).queryByText("ARGOCD_APPLICATIONSET_DEPLOY_SOURCE"))
      .not.toBeInTheDocument();

    fireEvent.click(within(inspector).getByRole("tab", { name: "Facts" }));
    expect(within(inspector).getByText("ARGOCD_APPLICATIONSET_DEPLOY_SOURCE"))
      .toBeInTheDocument();
    expect(within(inspector).getByText("94%")).toBeInTheDocument();
    expect(within(inspector).getByText("evidence_aggregate")).toBeInTheDocument();
    expect(within(inspector).getByText("2")).toBeInTheDocument();
    expect(within(inspector).getByText("derived")).toBeInTheDocument();

    fireEvent.click(within(inspector).getByRole("tab", { name: "Evidence paths" }));
    expect(within(inspector).getByText("applicationsets/api-node/kustomization.yaml"))
      .toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Config dependencies" }));

    const configGraph = screen.getByRole("img", { name: "catalog-api relationship map" });
    expect(within(configGraph).getByText("terraform-stack-marketplace")).toBeInTheDocument();
    expect(within(configGraph).getAllByText("READS_CONFIG_FROM").length).toBeGreaterThan(0);
    expect(within(configGraph).queryByText("iac-eks-argocd")).not.toBeInTheDocument();

    fireEvent.click(
      within(configGraph).getByRole("button", {
        name: /Inspect READS_CONFIG_FROM relationship from terraform-stack-marketplace/i
      })
    );
    const staleInspector = screen.getByRole("complementary", { name: "Relationship inspector" });
    expect(within(staleInspector).getByText(/stale relationship evidence/i)).toBeInTheDocument();
    fireEvent.click(within(staleInspector).getByRole("tab", { name: "Facts" }));
    expect(within(staleInspector).getByText("stale")).toBeInTheDocument();
    expect(within(staleInspector).getByText("54%")).toBeInTheDocument();
    expect(within(staleInspector).getByText("source evidence is stale")).toBeInTheDocument();
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
      environments: ["dev", "prod", "qa"],
      evidenceCount: 1,
      label: "ECS Terraform",
      relationshipTypes: ["PROVISIONS_DEPENDENCY_FOR"],
      resolvedCount: 1,
      sourceRepos: ["terraform-stack-node10"]
    },
    {
      environments: ["prod"],
      evidenceCount: 2,
      label: "Kubernetes GitOps",
      relationshipTypes: ["DEPLOYS_FROM"],
      resolvedCount: 2,
      sourceRepos: ["iac-eks-argocd", "helm-charts"]
    }
  ],
  name: "catalog-api",
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
          confidence: 0.94,
          evidenceCount: 2,
          evidenceKinds: ["ARGOCD_APPLICATIONSET_DEPLOY_SOURCE"],
          paths: ["applicationsets/api-node/kustomization.yaml"],
          provenanceMethod: "evidence_aggregate",
          rationale: "derived deployment correlation",
          relationshipTypes: ["DEPLOYS_FROM"],
          repository: "iac-eks-argocd",
          state: "derived",
          technology: "argocd"
        },
        {
          evidenceKinds: ["HELM_VALUES_REFERENCE"],
          paths: ["argocd/catalog-api/values.yaml"],
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
          paths: ["environments/dev/ecs.tf"],
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
          paths: ["environments/dev/resources.tf"],
          relationshipTypes: ["READS_CONFIG_FROM"],
          repository: "terraform-stack-node10",
          technology: "terraform"
        },
        {
          confidence: 0.54,
          evidenceKinds: ["TERRAFORM_IAM_PERMISSION"],
          paths: ["environments/dev/resources.tf"],
          provenanceMethod: "evidence_aggregate",
          rationale: "source evidence is stale",
          relationshipTypes: ["READS_CONFIG_FROM"],
          repository: "terraform-stack-marketplace",
          state: "stale",
          technology: "terraform"
        }
      ],
      technology: "terraform"
    }
  ],
  repoName: "catalog-api",
  summary: "catalog-api service story.",
  trafficPaths: [],
  trust: {
    basis: "hybrid",
    freshness: "fresh",
    level: "derived",
    profile: "production"
  }
};
