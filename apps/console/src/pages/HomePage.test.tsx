import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { vi } from "vitest";
import { HomePage } from "./HomePage";

describe("HomePage", () => {
  it("routes selected live search results into the entity workspace", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({
          count: 1,
          repositories: [
            {
              id: "repository:r_1",
              local_path: "/Users/allen/repos/mobius/mobius-tools",
              name: "mobius-tools"
            }
          ]
        })
      )
    );

    render(
      <MemoryRouter initialEntries={["/"]}>
        <Routes>
          <Route element={<HomePage />} path="/" />
          <Route element={<h1>Workspace opened</h1>} path="/workspace/:entityKind/:entityId" />
        </Routes>
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText("Search Eshu"), {
      target: { value: "mobius" }
    });
    fireEvent.click(await screen.findByRole("button", { name: /mobius-tools/i }));

    expect(screen.getByRole("heading", { name: "Workspace opened" })).toBeInTheDocument();
  });
});
