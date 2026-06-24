import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { RepositoriesPage } from "./RepositoriesPage";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";

describe("RepositoriesPage", () => {
  it("renders source-backed repository groups over the live repository list", async () => {
    const client = {
      get: async (path: string) => {
        if (path.includes("/repositories?")) {
          return {
            data: {
              repositories: [
                {
                  id: "repository:payments-api",
                  name: "payments-api",
                  repo_slug: "platform/payments-api",
                  is_dependency: false,
                  group_key: "Platform",
                  group_source: "repo_slug_namespace",
                  group_truth: "derived",
                  group_kind: "source",
                  group_reason: "derived from repository slug namespace"
                },
                {
                  id: "repository:billing-api",
                  name: "billing-api",
                  repo_slug: "platform/billing-api",
                  is_dependency: false,
                  group_key: "Platform",
                  group_source: "repo_slug_namespace",
                  group_truth: "derived",
                  group_kind: "source",
                  group_reason: "derived from repository slug namespace"
                },
                {
                  id: "repository:shared-lib",
                  name: "shared-lib",
                  repo_slug: "libraries/shared-lib",
                  is_dependency: true,
                  group_key: "Dependencies",
                  group_source: "repository_dependency_flag",
                  group_truth: "derived",
                  group_kind: "dependency",
                  group_reason: "repository is marked as a dependency"
                },
                {
                  id: "repository:unattributed",
                  name: "unattributed",
                  repo_slug: "",
                  is_dependency: false,
                  group_key: "",
                  group_source: "missing_evidence",
                  group_truth: "missing_evidence",
                  group_kind: "unknown",
                  group_reason: "no source-backed repository group evidence"
                }
              ]
            },
            error: null,
            truth: null
          };
        }
        return { data: {}, error: null, truth: null };
      }
    } as unknown as EshuApiClient;

    render(<RepositoriesPage client={client} model={demoModel} />, { wrapper: MemoryRouter });

    expect(await screen.findByText("Repository groups")).toBeInTheDocument();
    expect(screen.getByText(/Groups use source-backed repository grouping evidence/)).toBeInTheDocument();
    expect(screen.queryByText(/repository names and slug metadata/)).not.toBeInTheDocument();
    expect(screen.getByText("source-backed grouping")).toBeInTheDocument();
    const groupWorkbench = screen.getByLabelText("Repository group workbench");
    expect(groupWorkbench).toBeInTheDocument();
    expect(within(groupWorkbench).getByText("Platform")).toBeInTheDocument();
    expect(within(groupWorkbench).getByText("Dependencies")).toBeInTheDocument();
    expect(within(groupWorkbench).getByText("Grouping evidence missing")).toBeInTheDocument();
    expect(within(groupWorkbench).getByText("derived · repo_slug_namespace")).toBeInTheDocument();
    expect(within(groupWorkbench).getByText("missing_evidence · missing_evidence")).toBeInTheDocument();
    expect(screen.getByText("payments-api")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Find a group or repository"), { target: { value: "shared-lib" } });
    expect(within(groupWorkbench).getByText("shared-lib")).toBeInTheDocument();
    expect(within(groupWorkbench).queryByText("Platform")).not.toBeInTheDocument();
    // A repository-name match filters the group's repositories; the surviving group
    // must keep its source-backed evidence metadata (truth · source), not drop it.
    expect(within(groupWorkbench).getByText("derived · repository_dependency_flag")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Find a group or repository"), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Grid" }));
    await waitFor(() => expect(screen.getByText("payments-api")).toBeInTheDocument());
    expect(screen.getAllByText("Platform").length).toBeGreaterThan(0);
  });

  it("counts every repository in the StatTile after paging past the API page limit", async () => {
    // Regression for #3376: a 906-repo stack must not stop at the first 500-row
    // page. The Repositories StatTile reflects all paged rows, matching the
    // sidebar's index-status total instead of the single-page slice.
    const total = 906;
    const wireRepos = Array.from({ length: total }, (_, index) => ({
      id: `repository:r_${index}`,
      name: `repo-${index}`,
      repo_slug: `org/repo-${index}`,
      is_dependency: false,
      group_key: "Platform",
      group_source: "repo_slug_namespace",
      group_truth: "derived",
      group_kind: "source",
      group_reason: "derived from repository slug namespace"
    }));
    const client = {
      get: async (path: string) => {
        if (path.includes("/repositories?")) {
          const url = new URL(path, "http://console.test");
          const limit = Number(url.searchParams.get("limit") ?? "0");
          const offset = Number(url.searchParams.get("offset") ?? "0");
          const page = wireRepos.slice(offset, offset + limit);
          return {
            data: { repositories: page, count: page.length, limit, offset, truncated: offset + limit < total },
            error: null,
            truth: null
          };
        }
        return { data: {}, error: null, truth: null };
      }
    } as unknown as EshuApiClient;

    render(<RepositoriesPage client={client} model={demoModel} />, { wrapper: MemoryRouter });

    const repositoriesTile = await waitFor(() => {
      const labels = screen.getAllByText("Repositories");
      const tile = labels.map((label) => label.closest(".stat-tile")).find((node): node is HTMLElement => node !== null);
      if (!tile) throw new Error("Repositories stat tile not rendered yet");
      return tile;
    });
    await waitFor(() => expect(within(repositoriesTile).getByText(String(total))).toBeInTheDocument());
  });

  it("links the Dependency repos tile to the Dependencies view and counts depended-on repos", async () => {
    const client = {
      get: async (path: string) => {
        if (path.includes("/repositories?")) {
          return {
            data: {
              repositories: [
                {
                  id: "repository:app",
                  name: "app",
                  repo_slug: "platform/app",
                  is_dependency: false,
                  group_key: "Platform",
                  group_source: "repo_slug_namespace",
                  group_truth: "derived",
                  group_kind: "source",
                  group_reason: "derived from repository slug namespace"
                },
                {
                  id: "repository:lib",
                  name: "lib",
                  repo_slug: "libraries/lib",
                  is_dependency: true,
                  group_key: "Libraries",
                  group_source: "repository_dependency_flag",
                  group_truth: "derived",
                  group_kind: "dependency",
                  group_reason: "another repository depends on this one"
                }
              ]
            },
            error: null,
            truth: null
          };
        }
        return { data: {}, error: null, truth: null };
      }
    } as unknown as EshuApiClient;

    render(<RepositoriesPage client={client} model={demoModel} />, { wrapper: MemoryRouter });

    const dependencyLink = await screen.findByRole("link", {
      name: /View dependency chains in the Dependencies view/
    });
    expect(dependencyLink).toHaveAttribute("href", "/dependencies");
    const dependencyTile = dependencyLink.querySelector(".stat-tile");
    expect(dependencyTile).not.toBeNull();
    expect(within(dependencyTile as HTMLElement).getByText("1")).toBeInTheDocument();
  });

  it("links repository group chips directly to the source browser", async () => {
    const client = {
      get: async (path: string) => {
        if (path.includes("/repositories?")) {
          return {
            data: {
              repositories: [
                {
                  id: "repository:payments-api",
                  name: "payments-api",
                  repo_slug: "platform/payments-api",
                  is_dependency: false,
                  group_key: "Platform",
                  group_source: "repo_slug_namespace",
                  group_truth: "derived",
                  group_kind: "source",
                  group_reason: "derived from repository slug namespace"
                }
              ]
            },
            error: null,
            truth: null
          };
        }
        return { data: {}, error: null, truth: null };
      }
    } as unknown as EshuApiClient;

    render(<RepositoriesPage client={client} model={demoModel} />, { wrapper: MemoryRouter });

    const groupWorkbench = await screen.findByLabelText("Repository group workbench");
    expect(
      within(groupWorkbench).getByRole("link", { name: /payments-api/ })
    ).toHaveAttribute("href", "/repositories/repository%3Apayments-api/source");
  });
});
