import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";
import { consoleStorageKeys } from "../config/environment";
import { CatalogPage } from "./CatalogPage";

describe("CatalogPage", () => {
  it("shows live repository catalog rows", async () => {
    window.localStorage.setItem(
      consoleStorageKeys.environment,
      JSON.stringify({
        apiBaseUrl: "http://localhost:8080",
        apiKey: "",
        mode: "private"
      })
    );
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = new URL(new Request(input).url).pathname;
        if (path === "/api/v0/repositories") {
          return Response.json({
            count: 2,
            limit: 2,
            offset: 0,
            repositories: [
              {
                id: "repository:r_1",
                local_path: "/Users/allen/repos/mobius/mobius-tools",
                name: "mobius-tools"
              },
              {
                id: "repository:r_2",
                local_path: "/Users/allen/repos/mobius/iac-eks-pcg",
                name: "iac-eks-pcg"
              }
            ],
            truncated: true
          });
        }
        if (path === "/api/v0/repositories/repository%3Ar_2/story") {
          return Response.json({
            deployment_overview: { workloads: ["iac-eks-pcg"] }
          });
        }
        return Response.json({
          deployment_overview: { workloads: [] }
        });
      })
    );

    render(
      <MemoryRouter>
        <CatalogPage />
      </MemoryRouter>
    );

    expect(screen.getByRole("heading", { name: "Catalog" })).toBeInTheDocument();
    expect(await screen.findByText("mobius-tools")).toBeInTheDocument();
    expect(screen.getAllByText("iac-eks-pcg").length).toBeGreaterThan(1);
    expect(screen.getByRole("button", { name: /services 1/i })).toBeInTheDocument();
    expect(screen.getAllByText("indexed")).toHaveLength(2);
    expect(screen.getByText("Offset 0")).toBeInTheDocument();
    expect(screen.getByText("Limit 2")).toBeInTheDocument();
    expect(screen.getByText("More available")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /open mobius-tools workspace/i })
    ).toHaveAttribute("href", "/workspace/repositories/repository%3Ar_1");

    fireEvent.click(screen.getByRole("button", { name: /services 1/i }));

    expect(screen.queryByText("mobius-tools")).not.toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /open iac-eks-pcg workspace/i })
    ).toHaveAttribute("href", "/workspace/services/iac-eks-pcg");

    fireEvent.change(screen.getByLabelText("Search catalog"), {
      target: { value: "pcg" }
    });

    expect(screen.queryByText("mobius-tools")).not.toBeInTheDocument();
    expect(screen.getByText("iac-eks-pcg")).toBeInTheDocument();
  });

  it("turns catalog services and workloads into faceted drilldown rows", async () => {
    window.localStorage.setItem(
      consoleStorageKeys.environment,
      JSON.stringify({
        apiBaseUrl: "http://localhost:8080",
        apiKey: "",
        mode: "private"
      })
    );
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = new URL(new Request(input).url).pathname;
        if (path === "/api/v0/catalog") {
          return Response.json({
            count: 3,
            limit: 100,
            offset: 0,
            repositories: [
              {
                id: "repository:r_1",
                local_path: "/repos/api-node-boats",
                name: "api-node-boats"
              }
            ],
            services: [
              {
                environments: ["bg-prod", "bg-qa"],
                id: "workload:api-node-boats",
                instance_count: 2,
                kind: "service",
                materialization_status: "graph",
                name: "api-node-boats",
                repo_name: "api-node-boats"
              }
            ],
            workloads: [
              {
                environments: ["ops-prod"],
                id: "workload:billing-sync",
                instance_count: 1,
                kind: "cronjob",
                materialization_status: "identity_only",
                name: "billing-sync",
                repo_name: "billing-jobs"
              }
            ],
            truncated: false
          });
        }
        return Response.json({ detail: "missing route" }, { status: 404 });
      })
    );

    render(
      <MemoryRouter>
        <CatalogPage />
      </MemoryRouter>
    );

    expect((await screen.findAllByText("api-node-boats")).length).toBeGreaterThan(1);
    expect(screen.getByText("3 catalog entries")).toBeInTheDocument();
    expect(screen.getByText("3 environments")).toBeInTheDocument();
    expect(screen.getByText("1 identity-only")).toBeInTheDocument();
    expect(screen.getByLabelText("Filter catalog by environment")).toHaveValue("all");

    fireEvent.change(screen.getByLabelText("Filter catalog by environment"), {
      target: { value: "ops-prod" }
    });

    expect(screen.queryByText("api-node-boats")).not.toBeInTheDocument();
    expect(screen.getByText("billing-sync")).toBeInTheDocument();
    expect(screen.getAllByText("ops-prod").length).toBeGreaterThan(1);

    fireEvent.click(screen.getByRole("button", { name: /inspect billing-sync/i }));

    expect(screen.getByRole("heading", { name: "Selected catalog entry" })).toBeInTheDocument();
    expect(screen.getByText("billing-jobs")).toBeInTheDocument();
    expect(screen.getByText("identity only")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /open billing-sync workspace/i })
    ).toHaveAttribute("href", "/workspace/workloads/workload%3Abilling-sync");
  });
});
