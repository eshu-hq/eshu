import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { FindingsPage } from "./FindingsPage";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

describe("FindingsPage", () => {
  it("summarizes findings from the model", () => {
    renderFindings(demoModel);

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
    const empty: ConsoleModel = { ...demoModel, findings: [], vulnerabilities: [] };
    renderFindings(empty);

    expect(
      screen.getByText("No findings from this source.")
    ).toBeInTheDocument();
  });

  it("joins vulnerability rows into the actionable findings worklist", () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [{
        id: "dead-1",
        type: "Dead code",
        entity: "api-node-boats",
        title: "Unreferenced symbol unusedRoute",
        detail: "server/routes.ts · unused",
        truth: "derived",
        entityId: "content-entity:e1",
        filePath: "server/routes.ts"
      }],
      vulnerabilities: [{
        id: "CVE-2026-1234",
        package: "lodash",
        severity: "high",
        cvss: 8.1,
        kev: true,
        fixedVersion: "4.17.22",
        services: ["api-node-boats"]
      }]
    };

    renderFindings(model);

    expect(screen.getByText("CVE-2026-1234 · lodash")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open CVE" })).toHaveAttribute(
      "href",
      "/vulnerabilities/CVE-2026-1234"
    );
    expect(screen.getByRole("link", { name: "Open graph" })).toHaveAttribute(
      "href",
      "/code-graph?candidate=dead-1"
    );
    expect(screen.getAllByRole("link", { name: "Explore entity" })[0]).toHaveAttribute(
      "href",
      "/explorer?q=api-node-boats"
    );
  });
});

function renderFindings(model: ConsoleModel): ReturnType<typeof render> {
  return render(
    <MemoryRouter>
      <FindingsPage model={model} />
    </MemoryRouter>
  );
}
