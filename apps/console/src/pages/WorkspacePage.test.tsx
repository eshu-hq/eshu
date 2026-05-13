import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { vi } from "vitest";
import { WorkspacePage } from "./WorkspacePage";

describe("WorkspacePage", () => {
  it("renders a live repository story with proof, freshness, and deployment", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({
          deployment_overview: {
            delivery_paths: [
              {
                artifact_type: "github_actions_workflow",
                delivery_command_families: ["helm"],
                environments: ["prod"],
                kind: "workflow_artifact",
                path: ".github/workflows/cd-helm.yml",
                signals: ["workflow_file", "run_commands"],
                trigger_events: ["push"],
                workflow_name: "cd-helm"
              }
            ],
            direct_story: ["Runs through ArgoCD into prod."]
          },
          limitations: ["coverage_not_computed"],
          story: "Repository mobius-tools contains indexed files.",
          story_sections: [
            {
              summary: "41 indexed files across 2 language families",
              title: "codebase"
            }
          ],
          subject: {
            id: "repository:r_1",
            name: "mobius-tools",
            type: "repository"
          }
        })
      )
    );

    render(
      <MemoryRouter initialEntries={["/workspace/repositories/repository:r_1"]}>
        <Routes>
          <Route element={<WorkspacePage />} path="/workspace/:entityKind/:entityId" />
        </Routes>
      </MemoryRouter>
    );

    expect(await screen.findByRole("heading", { name: "mobius-tools" })).toBeInTheDocument();
    expect(screen.getByText(/contains 41 indexed files/i)).toBeInTheDocument();
    expect(screen.getByText("exact")).toBeInTheDocument();
    expect(screen.getByText("fresh")).toBeInTheDocument();
    expect(screen.getByText("Deployment graph")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /cd-helm/i })).toBeInTheDocument();
    expect(screen.getByText("Evidence story")).toBeInTheDocument();
    expect(screen.getByText("41 indexed files across 2 language families")).toBeInTheDocument();
    expect(screen.getAllByText("Drill down")).toHaveLength(2);
  });
});
