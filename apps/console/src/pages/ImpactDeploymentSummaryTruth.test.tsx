import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { DeploymentTraceSummary } from "./ImpactDeploymentSummary";
import type { DeploymentTraceResult } from "../api/impactReviewTypes";

describe("DeploymentTraceSummary topology truth", () => {
  it("labels environment values as instance attributes and renders exact edge provenance", () => {
    const trace: DeploymentTraceResult = {
      cloudResources: [],
      deploymentFacts: [],
      deploymentOverview: {},
      deploymentSourceLimits: null,
      deploymentSources: [],
      imageRefs: [],
      instances: [
        {
          environment: "prod",
          id: "workload-instance:catalog-api:prod",
          platforms: [
            {
              id: "platform:ecs:prod",
              kind: "ecs",
              name: "prod-runtime",
              topologyBasis: "direct_runtime",
              topologyEdges: [
                {
                  confidence: 0.99,
                  evidenceSource: "finalization/workloads",
                  reason: "Workload instance runs on exact platform",
                  relationshipType: "RUNS_ON",
                  sourceId: "workload-instance:catalog-api:prod",
                  sourceTool: "reducer",
                  targetId: "platform:ecs:prod",
                },
              ],
            },
          ],
        },
      ],
      k8sResources: [],
      provisionedPlatforms: [],
      repoId: "repository:r_catalog",
      repoName: "catalog-api",
      serviceName: "catalog-api",
      story: "Exact deployment topology.",
      topologyEdges: [
        {
          evidenceSource: "canonical_graph",
          reason: "Repository defines the selected workload",
          relationshipType: "DEFINES",
          sourceId: "repository:r_catalog",
          targetId: "workload:catalog-api",
        },
        {
          evidenceSource: "canonical_graph",
          reason: "Runtime instance belongs to the selected workload",
          relationshipType: "INSTANCE_OF",
          sourceId: "workload-instance:catalog-api:prod",
          targetId: "workload:catalog-api",
        },
      ],
      workloadId: "workload:catalog-api",
    };

    render(
      <MemoryRouter>
        <DeploymentTraceSummary
          canInspectEntity={(entityId) => entityId !== "environment:prod"}
          onInspectEntity={() => undefined}
          trace={trace}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText("Environment attribute: prod")).toBeInTheDocument();
    expect(screen.queryByText("Outside bounded graph")).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Inspect prod runtime instance" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/RUNS_ON/)).toBeInTheDocument();
    expect(screen.getByText(/finalization\/workloads/)).toBeInTheDocument();
    expect(screen.getByText(/Workload instance runs on exact platform/)).toBeInTheDocument();
    expect(screen.getByText(/reducer/)).toBeInTheDocument();
    expect(screen.getByText("Subject relationship evidence")).toBeInTheDocument();
    expect(screen.getByText(/Repository defines the selected workload/)).toBeInTheDocument();
    expect(
      screen.getByText(/Runtime instance belongs to the selected workload/),
    ).toBeInTheDocument();
  });
});
