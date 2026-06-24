import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";

import { CICDRunCorrelationsPage } from "./CICDRunCorrelationsPage";
import type { EshuApiClient } from "../api/client";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";

describe("CICDRunCorrelationsPage", () => {
  it("loads deep-linked run correlations with truth, rollups, and evidence gaps", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        if (path.includes("/count")) {
          return { data: countPayload(), error: null, truth: truthEnvelope("ci_cd.run_correlations.aggregate") };
        }
        if (path.includes("/inventory")) {
          return { data: inventoryPayload(), error: null, truth: truthEnvelope("ci_cd.run_correlations.aggregate") };
        }
        return { data: listPayload(), error: null, truth: truthEnvelope("ci_cd.run_correlations.list") };
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/ci-cd/run-correlations?repository_id=repo-api&environment=prod"]}>
        <CICDRunCorrelationsPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
      </MemoryRouter>
    );

    expect(await screen.findByRole("heading", { name: "CI/CD run correlations" })).toBeInTheDocument();
    await waitFor(() => {
      expect(calls).toEqual([
        "/api/v0/ci-cd/run-correlations/count?repository_id=repo-api&environment=prod",
        "/api/v0/ci-cd/run-correlations/inventory?group_by=outcome&repository_id=repo-api&environment=prod&limit=25",
        "/api/v0/ci-cd/run-correlations?repository_id=repo-api&environment=prod&limit=25"
      ]);
    });
    expect(screen.getAllByText("42").length).toBeGreaterThan(0);
    expect(screen.getByText("ci_cd.run_correlations.list")).toBeInTheDocument();
    expect(screen.getAllByText("exact").length).toBeGreaterThan(0);
    expect(screen.getByText("workflow artifact digest matched image identity")).toBeInTheDocument();
    expect(screen.getByText("workflow image ref")).toBeInTheDocument();
    expect(screen.getByText("ci run to image artifact evidence missing")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Repository" })).toHaveAttribute(
      "href",
      "/repositories/repo-api/source"
    );
    expect(screen.getByRole("link", { name: "Impact" })).toHaveAttribute(
      "href",
      "/impact?kind=service&target=checkout-api"
    );
  });

  it("submits filters into a deep-linkable bounded run-correlation URL", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        if (path.includes("/count")) {
          return { data: countPayload(), error: null, truth: truthEnvelope("ci_cd.run_correlations.aggregate") };
        }
        if (path.includes("/inventory")) {
          return { data: inventoryPayload(), error: null, truth: truthEnvelope("ci_cd.run_correlations.aggregate") };
        }
        return { data: listPayload(), error: null, truth: truthEnvelope("ci_cd.run_correlations.list") };
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/ci-cd/run-correlations"]}>
        <Routes>
          <Route
            element={(
              <>
                <CICDRunCorrelationsPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
                <LocationProbe />
              </>
            )}
            path="/ci-cd/run-correlations"
          />
        </Routes>
      </MemoryRouter>
    );

    await screen.findByRole("heading", { name: "CI/CD run correlations" });
    fireEvent.change(screen.getByLabelText("Repository id"), { target: { value: "repo-api" } });
    fireEvent.change(screen.getByLabelText("Environment"), { target: { value: "prod" } });
    fireEvent.click(screen.getByRole("button", { name: "Review runs" }));

    await waitFor(() => {
      expect(calls).toContain("/api/v0/ci-cd/run-correlations?repository_id=repo-api&environment=prod&limit=25");
    });
    expect(screen.getByTestId("ci-cd-location")).toHaveTextContent(
      "/ci-cd/run-correlations?repository_id=repo-api&environment=prod"
    );
  });
});

function LocationProbe(): React.JSX.Element {
  const location = useLocation();
  return <output data-testid="ci-cd-location">{location.pathname + location.search}</output>;
}

function truthEnvelope(capability: string) {
  return {
    basis: "semantic_facts",
    capability,
    freshness: { state: "fresh" },
    level: "derived",
    profile: "local_authoritative"
  };
}

function countPayload(): Record<string, unknown> {
  return {
    by_environment: { prod: 30, stage: 12 },
    by_outcome: { derived: 10, exact: 32 },
    by_provider: { github_actions: 42 },
    scope: { environment: "prod", repository_id: "repo-api" },
    total_correlations: 42
  };
}

function inventoryPayload(): Record<string, unknown> {
  return {
    buckets: [
      { count: 32, dimension: "outcome", value: "exact" },
      { count: 10, dimension: "outcome", value: "derived" }
    ],
    count: 2,
    group_by: "outcome",
    limit: 25,
    next_offset: null,
    offset: 0,
    scope: { environment: "prod", repository_id: "repo-api" },
    truncated: false
  };
}

function listPayload(): Record<string, unknown> {
  return {
    correlations: [{
      artifact_digest: "sha256:abc",
      canonical_target: "checkout-api",
      canonical_writes: 1,
      commit_sha: "abc123",
      correlation_id: "correlation-1",
      correlation_kind: "workflow_artifact",
      environment: "prod",
      evidence_fact_ids: ["fact-run", "fact-artifact"],
      image_ref: "registry.example.test/team/api:prod",
      outcome: "exact",
      provider: "github_actions",
      provenance_only: false,
      reason: "workflow artifact digest matched image identity",
      repository_id: "repo-api",
      run_id: "12345"
    }],
    count: 1,
    evidence_summary: {
      live_run_correlations: { count: 1, state: "present", truncated: false },
      missing_evidence: ["ci_run_to_image_artifact_evidence_missing"],
      run_artifact_evidence: {
        ambiguous_count: 0,
        artifact_digest_count: 1,
        count: 1,
        image_ref_count: 1,
        state: "present"
      },
      static_workflow_artifacts: {
        count: 2,
        evidence_class: "workflow_image_ref",
        image_ref_count: 1,
        paths: [".github/workflows/deploy.yml"],
        state: "present",
        truncated: false
      }
    },
    limit: 25,
    truncated: false
  };
}
