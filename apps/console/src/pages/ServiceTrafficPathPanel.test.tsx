import { fireEvent, render, screen, within } from "@testing-library/react";
import { vi } from "vitest";

import { ServiceTrafficPathPanel } from "./ServiceTrafficPathPanel";
import type { ServiceTrafficPath } from "../api/serviceTrafficPath";

describe("ServiceTrafficPathPanel", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("resolves clicked graph nodes before offering canonical drilldown", async () => {
    const fetcher = vi.fn(async () =>
      Response.json({
        data: {
          count: 3,
          entities: [
            {
              entity_id: "resource:origin-alb-primary",
              labels: ["K8sResource"],
              name: "origin-alb-primary",
              repo_id: "iac-eks-argocd",
              repo_name: "iac-eks-argocd"
            },
            {
              file_path: "infra/catalog-api.tf",
              id: "entity:origin-alb-primary",
              labels: ["TerraformBlock"],
              name: "origin-alb-primary",
              repo_id: "terraform-stack-node10",
              repo_name: "terraform-stack-node10"
            },
            {
              entity_id: "entity:unscoped-origin",
              labels: ["Variable"],
              name: "origin-alb-primary"
            }
          ],
          limit: 10,
          truncated: true
        },
        error: null,
        truth: {
          basis: "hybrid_graph_and_content",
          capability: "code_search.fuzzy_symbol",
          freshness: { state: "fresh" },
          level: "derived",
          profile: "local_authoritative"
        }
      })
    );
    vi.stubGlobal("fetch", fetcher);

    render(<ServiceTrafficPathPanel paths={[trafficPath]} serviceName="catalog-api" />);

    const graph = screen.getByRole("img", { name: "catalog-api traffic path" });
    fireEvent.click(within(graph).getByLabelText("Resolve Origin origin-alb-primary"));

    expect(await screen.findByRole("heading", { name: "Resolve selected node" }))
      .toBeInTheDocument();
    expect(fetcher).toHaveBeenCalledWith(
      "http://localhost:5174/eshu-api/api/v0/entities/resolve",
      expect.objectContaining({
        body: JSON.stringify({
          limit: 10,
          name: "origin-alb-primary",
          type: "terraform_block"
        }),
        method: "POST"
      })
    );
    const detail = screen.getByLabelText("Selected traffic evidence");
    expect(within(detail).getAllByText("origin-alb-primary").length).toBeGreaterThan(0);
    expect(within(detail).getByText("Showing 3 of 10 candidates")).toBeInTheDocument();
    expect(within(detail).getByText("More matches available")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open iac-eks-argocd" }))
      .toHaveAttribute("href", "/workspace/repositories/iac-eks-argocd");
    expect(screen.getByRole("button", { name: "Select entity:unscoped-origin" }))
      .toBeDisabled();
  });
});

const trafficPath: ServiceTrafficPath = {
  edge: "CloudFront distribution",
  environment: "prod",
  evidenceKind: "aws_cloudfront_distribution",
  hostname: "catalog-api.prod.example.internal",
  origin: "origin-alb-primary",
  reason: "CloudFront distribution E123",
  runtime: "prod",
  sourceRepo: "terraform-stack-node10",
  visibility: "public",
  workload: "catalog-api"
};
