import { fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { DashboardPage } from "./DashboardPage";

describe("DashboardPage", () => {
  it("shows live runtime and indexing status", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({
          queue: { outstanding: 0, succeeded: 201 },
          repository_count: 23,
          status: "healthy"
        })
      )
    );

    render(<DashboardPage />);

    expect(screen.getByRole("heading", { name: "Dashboard" })).toBeInTheDocument();
    expect(await screen.findByText("Index status")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /repositories 23/i })).toBeInTheDocument();
    expect(screen.getByText("23")).toBeInTheDocument();
    expect(screen.getByText("Runtime timeline")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /queue outstanding 0/i }));

    expect(screen.getByText(/Queue is drained/)).toBeInTheDocument();
    expect(screen.getByText(/0 outstanding/)).toBeInTheDocument();
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
