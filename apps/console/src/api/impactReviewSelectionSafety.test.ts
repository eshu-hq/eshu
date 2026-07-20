import {
  deploymentTracePayload,
  loadDeploymentReview,
  nonEmptyChangeSurface,
} from "./impactReviewDeploymentGraph.testSupport";

describe("impact review selection safety", () => {
  it("does not select deployment topology from a different repository scope", async () => {
    const changeSurface = nonEmptyChangeSurface();
    const resolution = changeSurface.target_resolution as Record<string, unknown>;
    const selected = resolution.selected as Record<string, unknown>;
    const scope = changeSurface.scope as Record<string, unknown>;
    const review = await loadDeploymentReview(deploymentTracePayload(), "fresh", "exact", {
      ...changeSurface,
      scope: { ...scope, repo_id: "repository:r_requested" },
      target_resolution: {
        ...resolution,
        selected: { ...selected, repo_id: "repository:r_requested" },
      },
    });

    expect(review.graphPresentation.mode).toBe("change_surface");
    expect(review.graphPresentation.limitations).toContain(
      "deployment topology not selected because trace and change-surface repository identities disagree",
    );
    expect(review.graph.nodes.some((node) => node.id === "repository:r_catalog")).toBe(false);
  });

  it("surfaces truncation from a selected non-empty change surface", async () => {
    const changeSurface = nonEmptyChangeSurface();
    const coverage = changeSurface.coverage as Record<string, unknown>;
    const review = await loadDeploymentReview(deploymentTracePayload(), "fresh", "exact", {
      ...changeSurface,
      coverage: { ...coverage, truncated: true },
      truncated: true,
    });

    expect(review.graphPresentation).toMatchObject({
      completeness: "truncated",
      mode: "change_surface",
      truncated: true,
    });
  });
});
