import { fireEvent, render, screen } from "@testing-library/react";
import { DeadCodePage } from "./DeadCodePage";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

describe("DeadCodePage", () => {
  it("renders the dedicated dead-code workbench from finding rows", () => {
    render(<DeadCodePage model={demoModel} />);

    expect(screen.getByRole("heading", { name: "Dead code" })).toBeInTheDocument();
    expect(screen.getByLabelText("Dead-code workbench")).toBeInTheDocument();
    expect(screen.getByText("Grouped by repository")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /All kinds/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Any" })).toBeInTheDocument();
    expect(screen.getByText("Unreferenced symbol legacyDiscount")).toBeInTheDocument();
    expect(screen.queryByText("CVE-2024-0001 reachable in prod image")).not.toBeInTheDocument();
  });

  it("filters candidates by analyzer classification", () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        ...demoModel.findings.map((finding) =>
          finding.id === "d2" ? { ...finding, detail: "src/discounts.ts · unused" } : finding
        ),
        {
          id: "d3",
          type: "Dead code",
          entity: "payments-service",
          title: "Unreferenced symbol oldWebhook",
          detail: "src/webhooks.ts · ambiguous",
          truth: "derived"
        }
      ]
    };
    render(<DeadCodePage model={model} />);

    fireEvent.click(screen.getByRole("button", { name: /unused/ }));

    expect(screen.getByText("Unreferenced symbol legacyDiscount")).toBeInTheDocument();
    expect(screen.queryByText("Unreferenced symbol oldWebhook")).not.toBeInTheDocument();
  });

  it("renders an honest empty state when no dead-code candidates exist", () => {
    const empty: ConsoleModel = {
      ...demoModel,
      findings: demoModel.findings.filter((finding) => finding.type !== "Dead code")
    };

    render(<DeadCodePage model={empty} />);

    expect(screen.getByText("No dead-code candidates from this source.")).toBeInTheDocument();
  });
});
