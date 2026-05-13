import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";
import { CatalogPage } from "./CatalogPage";

describe("CatalogPage", () => {
  it("shows live repository catalog rows", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({
          count: 2,
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
          ]
        })
      )
    );

    render(
      <MemoryRouter>
        <CatalogPage />
      </MemoryRouter>
    );

    expect(screen.getByRole("heading", { name: "Catalog" })).toBeInTheDocument();
    expect(await screen.findAllByText("mobius-tools")).toHaveLength(2);
    expect(screen.getAllByText("iac-eks-pcg")).toHaveLength(2);
    expect(screen.getAllByText("indexed")).toHaveLength(2);
    expect(
      screen.getByRole("link", { name: /open mobius-tools workspace/i })
    ).toHaveAttribute("href", "/workspace/repositories/repository%3Ar_1");
    expect(screen.getByText("Repository graph")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Search catalog"), {
      target: { value: "pcg" }
    });

    expect(screen.queryByText("mobius-tools")).not.toBeInTheDocument();
    expect(screen.getAllByText("iac-eks-pcg")).toHaveLength(2);
  });
});
