import { render, screen } from "@testing-library/react";
import { FindingsPage } from "./FindingsPage";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

describe("FindingsPage", () => {
  it("summarizes findings from the model", () => {
    render(<FindingsPage model={demoModel} />);

    expect(screen.getByRole("heading", { name: "Findings" })).toBeInTheDocument();
    expect(screen.getByText("Open findings")).toBeInTheDocument();

    // Finding titles render once each in the table.
    expect(
      screen.getByText("CVE-2024-0001 reachable in prod image")
    ).toBeInTheDocument();
    expect(
      screen.getByText("Unreferenced symbol legacyDiscount")
    ).toBeInTheDocument();

    // Each type shows once as a summary tile label and once as a row badge.
    expect(screen.getAllByText("Dead code").length).toBeGreaterThan(1);
    expect(screen.getAllByText("Vulnerability").length).toBeGreaterThan(1);
  });

  it("renders the empty state when the model has no findings", () => {
    const empty: ConsoleModel = { ...demoModel, findings: [] };
    render(<FindingsPage model={empty} />);

    expect(
      screen.getByText("No findings from this source.")
    ).toBeInTheDocument();
  });
});
