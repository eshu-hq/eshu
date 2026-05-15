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
});
