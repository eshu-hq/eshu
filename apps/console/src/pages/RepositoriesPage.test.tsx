import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import { RepositoriesPage } from "./RepositoriesPage";

describe("RepositoriesPage", () => {
  it("renders demo-style repository groups over the live repository list", async () => {
    const client = {
      get: async (path: string) => {
        if (path.includes("/repositories?")) {
          return {
            data: {
              repositories: [
                { id: "repository:lib-api-hapi", name: "lib-api-hapi", repo_slug: "", is_dependency: false },
                { id: "repository:dmm-clients", name: "dmm-clients", repo_slug: "", is_dependency: false },
                { id: "repository:api-node-boats", name: "api-node-boats", repo_slug: "", is_dependency: false },
                { id: "repository:api-node-external-search", name: "api-node-external-search", repo_slug: "", is_dependency: false },
                { id: "repository:api-node-forex", name: "api-node-forex", repo_slug: "", is_dependency: false },
                { id: "repository:iac-eks-argocd", name: "iac-eks-argocd", repo_slug: "", is_dependency: false }
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
    const groupWorkbench = screen.getByLabelText("Repository group workbench");
    expect(groupWorkbench).toBeInTheDocument();
    expect(within(groupWorkbench).getByText("Shared Libraries")).toBeInTheDocument();
    expect(within(groupWorkbench).getByText("Boat-Search")).toBeInTheDocument();
    expect(within(groupWorkbench).getByText("FX")).toBeInTheDocument();
    expect(within(groupWorkbench).queryByText("api")).not.toBeInTheDocument();
    expect(screen.getByText("lib-api-hapi")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Find a group or repository"), { target: { value: "Boat-Search" } });
    expect(within(groupWorkbench).getByText("api-node-boats")).toBeInTheDocument();
    expect(within(groupWorkbench).queryByText("Shared Libraries")).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Find a group or repository"), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Grid" }));
    await waitFor(() => expect(screen.getByText("api-node-boats")).toBeInTheDocument());
    expect(screen.getAllByText("Boat-Search").length).toBeGreaterThan(0);
  });

  it("links repository group chips directly to the source browser", async () => {
    const client = {
      get: async (path: string) => {
        if (path.includes("/repositories?")) {
          return {
            data: {
              repositories: [
                { id: "repository:api-node-boats", name: "api-node-boats", repo_slug: "platform/api-node-boats", is_dependency: false }
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
      within(groupWorkbench).getByRole("link", { name: /api-node-boats/ })
    ).toHaveAttribute("href", "/repositories/repository%3Aapi-node-boats/source");
  });
});
