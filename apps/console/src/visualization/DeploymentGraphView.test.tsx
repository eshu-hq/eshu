import { fireEvent, render, screen, within } from "@testing-library/react";
import { DeploymentGraphView } from "./DeploymentGraphView";
import type { DeploymentGraph } from "../api/mockData";

describe("DeploymentGraphView", () => {
  it("renders full deployment names and lets users drill into nodes", () => {
    const graph: DeploymentGraph = {
      links: [
        {
          label: "deploys from",
          source: "source:iac-eks-argocd",
          target: "evidence:argocd:iac-eks-argocd"
        },
        {
          label: "configures",
          source: "evidence:argocd:iac-eks-argocd",
          target: "target:service"
        }
      ],
      nodes: [
        {
          column: 0,
          id: "source:iac-eks-argocd",
          kind: "repository",
          label: "iac-eks-argocd",
          lane: "argocd:iac-eks-argocd"
        },
        {
          column: 1,
          detail: "applicationsets/devops/core-mcps/boats-search-mcp.yaml",
          id: "evidence:argocd:iac-eks-argocd",
          kind: "evidence",
          label: "ArgoCD ApplicationSet",
          lane: "argocd:iac-eks-argocd"
        },
        {
          column: 3,
          id: "target:service",
          kind: "service",
          label: "boats-chatgpt-app service",
          lane: "service"
        }
      ]
    };

    render(<DeploymentGraphView graph={graph} />);

    const graphImage = screen.getByRole("img", { name: "Deployment evidence graph" });
    expect(within(graphImage).getByText("ArgoCD")).toBeInTheDocument();
    expect(within(graphImage).getByText("ApplicationSet")).toBeInTheDocument();
    expect(screen.queryByText(/\.\.\./)).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /ArgoCD ApplicationSet evidence/i }));

    expect(
      screen.getByText(/applicationsets\/devops\/core-mcps\/boats-search-mcp\.yaml/i)
    ).toBeInTheDocument();
  });
});
