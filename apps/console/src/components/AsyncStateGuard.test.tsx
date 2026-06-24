import { render, screen } from "@testing-library/react";

import { AsyncStateGuard } from "./AsyncStateGuard";

describe("AsyncStateGuard", () => {
  it("shows a spinner and loading label while in flight", () => {
    render(
      <AsyncStateGuard provenance="loading" label="catalog">
        <p>Data</p>
      </AsyncStateGuard>
    );
    expect(screen.getByRole("status", { name: "Loading catalog" })).toBeInTheDocument();
    expect(screen.queryByText("Data")).not.toBeInTheDocument();
  });

  it("shows an error message when the section is unavailable", () => {
    render(
      <AsyncStateGuard provenance="unavailable" label="catalog">
        <p>Data</p>
      </AsyncStateGuard>
    );
    expect(screen.getByText(/unavailable/i)).toBeInTheDocument();
    expect(screen.queryByText("Data")).not.toBeInTheDocument();
  });

  it("renders children for the live provenance", () => {
    render(
      <AsyncStateGuard provenance="live" label="catalog">
        <p>Data</p>
      </AsyncStateGuard>
    );
    expect(screen.getByText("Data")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("renders children for the demo provenance", () => {
    render(
      <AsyncStateGuard provenance="demo" label="catalog">
        <p>Demo data</p>
      </AsyncStateGuard>
    );
    expect(screen.getByText("Demo data")).toBeInTheDocument();
  });

  it("renders children for the empty provenance (200 with zero rows)", () => {
    render(
      <AsyncStateGuard provenance="empty" label="catalog">
        <p>No rows</p>
      </AsyncStateGuard>
    );
    expect(screen.getByText("No rows")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });
});
