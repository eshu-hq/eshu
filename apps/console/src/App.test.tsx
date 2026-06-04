import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";
import { App } from "./App";

describe("App shell", () => {
  it("renders the redesigned console navigation", async () => {
    // The shell auto-connects to the API on boot; make it unreachable so it
    // falls back to the demo model without persisting anything.
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("offline in test");
      })
    );

    render(
      <MemoryRouter initialEntries={["/"]}>
        <App />
      </MemoryRouter>
    );

    expect(
      screen.getByRole("heading", { name: "Eshu Console" })
    ).toBeInTheDocument();

    // findBy lets the boot connect attempt settle before assertions.
    expect(
      await screen.findByRole("link", { name: "Dashboard" })
    ).toHaveAttribute("href", "/dashboard");
    expect(
      screen.getByRole("link", { name: "Graph Explorer" })
    ).toHaveAttribute("href", "/explorer");
    expect(screen.getByRole("link", { name: "Catalog" })).toHaveAttribute(
      "href",
      "/catalog"
    );
    expect(screen.getByRole("link", { name: "Findings" })).toHaveAttribute(
      "href",
      "/findings"
    );
    expect(
      screen.getByRole("link", { name: "Vulnerabilities" })
    ).toHaveAttribute("href", "/vulnerabilities");
    expect(screen.getByRole("link", { name: "Operations" })).toHaveAttribute(
      "href",
      "/operations"
    );
    expect(
      screen.getByRole("link", { name: /context graph/i })
    ).toHaveAttribute("href", "/");

    vi.unstubAllGlobals();
  });
});
