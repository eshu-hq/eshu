import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { DeploymentTraceSummary } from "./ImpactDeploymentSummary";
import type { DeploymentSourceLimits, DeploymentTraceResult } from "../api/impactReviewTypes";

describe("DeploymentTraceSummary deployment-source coverage", () => {
  it("discloses a lower-bound count when the server stopped at its sentinel", () => {
    renderSummary({
      canonicalObservedCount: 51,
      limit: 50,
      observedCount: 51,
      observedCountIsLowerBound: true,
      ordering: ["relationship_type_priority", "repo_name", "source_id", "target_id"],
      querySentinelLimit: 51,
      repositoryObservedCount: 4,
      returnedCount: 50,
      truncated: true,
    });

    expect(
      screen.getByText(
        "Deployment sources truncated: showing 50 of at least 51 observed relationships (50-result limit).",
      ),
    ).toBeInTheDocument();
  });

  it("discloses the exact observed count when truncation happened after bounded merging", () => {
    renderSummary({
      canonicalObservedCount: 34,
      limit: 50,
      observedCount: 62,
      observedCountIsLowerBound: false,
      ordering: ["relationship_type_priority", "repo_name", "source_id", "target_id"],
      querySentinelLimit: 51,
      repositoryObservedCount: 28,
      returnedCount: 50,
      truncated: true,
    });

    expect(
      screen.getByText(
        "Deployment sources truncated: showing 50 of 62 observed relationships (50-result limit).",
      ),
    ).toBeInTheDocument();
  });

  it("marks deployment-source completeness unknown when coverage metadata is unavailable", () => {
    renderSummary(null);

    expect(
      screen.getByText(
        "Deployment source coverage unavailable; deployment topology completeness is unverified.",
      ),
    ).toBeInTheDocument();
  });
});

function renderSummary(deploymentSourceLimits: DeploymentSourceLimits | null): void {
  const trace: DeploymentTraceResult = {
    cloudResources: [],
    deploymentFacts: [],
    deploymentOverview: {},
    deploymentSourceLimits,
    deploymentSources: [],
    imageRefs: [],
    instances: [],
    k8sResources: [],
    provisionedPlatforms: [],
    repoId: "repository:r_catalog",
    repoName: "catalog-api",
    runtimeTopologyLimits: null,
    serviceName: "catalog-api",
    story: "Deployment source results are bounded.",
    topologyEdges: [],
    workloadId: "workload:catalog-api",
  };

  render(
    <MemoryRouter>
      <DeploymentTraceSummary
        canInspectEntity={() => false}
        onInspectEntity={() => undefined}
        trace={trace}
      />
    </MemoryRouter>,
  );
}
