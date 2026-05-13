import { fireEvent, render, screen, within } from "@testing-library/react";
import { vi } from "vitest";
import { DashboardPage } from "./DashboardPage";

describe("DashboardPage", () => {
  it("shows live runtime and indexing status", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = new URL(new Request(input).url).pathname;
        if (path.endsWith("/api/v0/repositories")) {
          return Response.json({
            repositories: [
              { id: "repository:r_1", name: "mobius-tools" },
              { id: "repository:r_2", name: "iac-eks-pcg" }
            ]
          });
        }
        if (path.endsWith("/api/v0/repositories/repository%3Ar_1/story")) {
          return Response.json({
            drilldowns: { context_path: "/api/v0/repositories/repository:r_1/context" }
          });
        }
        if (path.endsWith("/api/v0/repositories/repository:r_1/context")) {
          return Response.json({});
        }
        if (path.endsWith("/api/v0/repositories/repository%3Ar_2/story")) {
          return Response.json({
            deployment_overview: { workloads: ["iac-eks-pcg"] },
            drilldowns: { context_path: "/api/v0/repositories/repository:r_2/context" }
          });
        }
        if (
          path.endsWith("/api/v0/repositories/repository:r_2/context") ||
          path.endsWith("/api/v0/repositories/repository%3Ar_2/context")
        ) {
          return Response.json({
            deployment_evidence: {
              artifacts: [
                {
                  artifact_family: "argocd",
                  relationship_type: "DISCOVERS_CONFIG_IN",
                  source_location: {
                    path: "applicationsets/devops/core-mcps/platformcontextgraph.yaml",
                    repo_name: "iac-eks-argocd"
                  },
                  source_repo_name: "iac-eks-argocd",
                  target_repo_name: "iac-eks-pcg"
                }
              ]
            }
          });
        }
        if (path.endsWith("/api/v0/services/iac-eks-pcg/context")) {
          return Response.json({
            deployment_evidence: {
              artifacts: [
                {
                  artifact_family: "helm",
                  relationship_type: "DEPLOYS_FROM",
                  source_location: {
                    path: "charts/platformcontextgraph/values.yaml",
                    repo_name: "helm-charts"
                  },
                  source_repo_name: "helm-charts",
                  target_repo_name: "iac-eks-pcg"
                }
              ]
            }
          });
        }
        return Response.json({
          queue: { outstanding: 0, succeeded: 201 },
          repository_count: 23,
          status: "healthy"
        });
      })
    );

    render(<DashboardPage />);

    expect(screen.getByRole("heading", { name: "Dashboard" })).toBeInTheDocument();
    expect(
      await screen.findByRole("button", { name: /index status healthy/i })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /inspect graph repositories 23/i })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /inspect catalog repositories 2/i })
    ).toBeInTheDocument();
    expect(screen.getAllByText("23").length).toBeGreaterThan(0);
    expect(screen.getByText("Runtime dossier")).toBeInTheDocument();
    expect(screen.getByText(/23 repositories indexed by graph status/i)).toBeInTheDocument();
    expect(screen.getByText("Queue ledger")).toBeInTheDocument();
    expect(await screen.findByText("Deployment relationship graph")).toBeInTheDocument();
    expect(screen.getByText("Canonical verbs")).toBeInTheDocument();
    expect(screen.getByText("Runtime topology")).toBeInTheDocument();
    expect(screen.getAllByText("DISCOVERS_CONFIG_IN").length).toBeGreaterThan(0);
    expect(screen.getAllByText("DEPLOYS_FROM").length).toBeGreaterThan(0);
    expect(screen.getByText("RUNS_ON")).toBeInTheDocument();
    expect(screen.getByText("READS_CONFIG_FROM")).toBeInTheDocument();
    expect(screen.getByText("PROVISIONS_PLATFORM")).toBeInTheDocument();
    expect(screen.getByText("DEPLOYMENT_SOURCE")).toBeInTheDocument();

    const queueLedger = screen.getByLabelText("Queue ledger");
    fireEvent.click(within(queueLedger).getByRole("button", { name: /queue outstanding 0/i }));

    const detail = screen.getByLabelText("Runtime dossier");
    expect(
      within(detail).getByText(/No queued work is waiting on reducers or projectors/)
    ).toBeInTheDocument();
  });

  it("shows degraded projection separately from catalog availability", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = new URL(new Request(input).url).pathname;
        if (path.endsWith("/api/v0/repositories")) {
          return Response.json({
            repositories: [
              { id: "repository:r_1", name: "boats-chatgpt-app" },
              { id: "repository:r_2", name: "iac-eks-pcg" }
            ]
          });
        }
        if (path.includes("/story") || path.includes("/context")) {
          return Response.json({});
        }
        return Response.json({
          queue: { dead_letter: 4, in_flight: 1, outstanding: 1, succeeded: 209 },
          reasons: ["4 work items are dead-lettered"],
          repository_count: 0,
          status: "degraded"
        });
      })
    );

    render(<DashboardPage />);

    expect(
      await screen.findByRole("button", { name: /index status degraded/i })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /inspect graph repositories 0/i })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /inspect catalog repositories 2/i })
    ).toBeInTheDocument();

    const queueLedger = screen.getByLabelText("Queue ledger");
    fireEvent.click(within(queueLedger).getByRole("button", { name: /dead letters 4/i }));

    const detail = screen.getByLabelText("Runtime dossier");
    expect(within(detail).getByText("4 dead-lettered work item(s).")).toBeInTheDocument();
  });

  it("shows the local API failure reason when loading fails", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("blocked by test");
      })
    );

    render(<DashboardPage />);

    expect(
      await screen.findByText("Local Eshu API unavailable: blocked by test.")
    ).toBeInTheDocument();
  });
});
