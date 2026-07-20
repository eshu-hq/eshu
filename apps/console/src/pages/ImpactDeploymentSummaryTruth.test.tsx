import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { DeploymentTraceSummary } from "./ImpactDeploymentSummary";
import type { DeploymentTraceResult } from "../api/impactReviewTypes";

describe("DeploymentTraceSummary topology truth", () => {
  it("labels environment values as instance attributes and renders exact edge provenance", () => {
    const trace: DeploymentTraceResult = {
      cloudResourceLimits: null,
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
      k8sResourceLimits: null,
      k8sResources: [],
      provisionedPlatforms: [],
      repoId: "repository:r_catalog",
      repoName: "catalog-api",
      runtimeTopologyLimits: null,
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
      uncorrelatedCloudResourcesTruncated: false,
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

describe("DeploymentTraceSummary deployment-source relationships", () => {
  it("distinguishes runtime-instance deployment sources with human endpoints", () => {
    const trace = baseTrace({
      deploymentSources: [
        {
          id: "repository:r_config",
          name: "deployment-config",
          relationshipType: "DEPLOYMENT_SOURCE",
          sourceId: "instance:catalog:prod",
          targetId: "repository:r_config",
        },
        {
          id: "repository:r_config",
          name: "deployment-config",
          relationshipType: "DEPLOYMENT_SOURCE",
          sourceId: "instance:catalog:stage",
          targetId: "repository:r_config",
        },
        {
          id: "repository:r_config",
          name: "deployment-config",
          relationshipType: "DEPLOYS_FROM",
          sourceId: "repository:r_config",
          targetId: "repository:r_catalog",
        },
      ],
      instances: [
        { environment: "prod", id: "instance:catalog:prod", platforms: [] },
        { environment: "stage", id: "instance:catalog:stage", platforms: [] },
      ],
    });

    render(
      <MemoryRouter>
        <DeploymentTraceSummary
          canInspectEntity={() => true}
          onInspectEntity={() => undefined}
          trace={trace}
        />
      </MemoryRouter>,
    );

    const rows = screen.getAllByRole("listitem");
    expect(rows).toHaveLength(3);
    expect(within(rows[0]).getByText("DEPLOYMENT_SOURCE")).toBeInTheDocument();
    expect(
      within(rows[0]).getByText(
        "deployment source: catalog-api (prod runtime instance) → deployment-config",
      ),
    ).toBeInTheDocument();
    expect(
      within(rows[0]).getByText("instance:catalog:prod → repository:r_config"),
    ).toBeInTheDocument();
    expect(within(rows[1]).getByText("DEPLOYMENT_SOURCE")).toBeInTheDocument();
    expect(
      within(rows[1]).getByText(
        "deployment source: catalog-api (stage runtime instance) → deployment-config",
      ),
    ).toBeInTheDocument();
    expect(
      within(rows[1]).getByText("instance:catalog:stage → repository:r_config"),
    ).toBeInTheDocument();
    expect(within(rows[2]).getByText("DEPLOYS_FROM")).toBeInTheDocument();
    expect(
      within(rows[2]).getByText("deploys from: deployment-config → catalog-api"),
    ).toBeInTheDocument();
  });
});

function baseTrace(overrides: Partial<DeploymentTraceResult>): DeploymentTraceResult {
  return {
    cloudResourceLimits: null,
    cloudResources: [],
    deploymentFacts: [],
    deploymentOverview: {},
    deploymentSourceLimits: null,
    deploymentSources: [],
    imageRefs: [],
    instances: [],
    k8sResourceLimits: null,
    k8sResources: [],
    provisionedPlatforms: [],
    repoId: "repository:r_catalog",
    repoName: "catalog-api",
    runtimeTopologyLimits: null,
    serviceName: "catalog-api",
    story: "Exact deployment topology.",
    topologyEdges: [],
    workloadId: "workload:catalog-api",
    ...overrides,
    uncorrelatedCloudResourcesTruncated: overrides.uncorrelatedCloudResourcesTruncated ?? false,
  };
}
