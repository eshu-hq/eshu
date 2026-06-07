import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { VulnerabilitiesPage } from "./VulnerabilitiesPage";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

function renderPage(model: ConsoleModel): void {
  render(
    <MemoryRouter>
      <VulnerabilitiesPage model={model} />
    </MemoryRouter>
  );
}

describe("VulnerabilitiesPage", () => {
  it("renders the advisory register with resolved service names", () => {
    renderPage(demoModel);

    expect(screen.getByRole("heading", { name: "Vulnerabilities" })).toBeInTheDocument();
    expect(screen.getByText("CVE-2024-0001")).toBeInTheDocument();
    // Services column shows the human service name from the model.
    expect(screen.getByText("checkout-service")).toBeInTheDocument();
  });

  it("shows the human service name carried by the model, not a raw repo id", () => {
    // The adapter resolves repository ids to catalog names; the page renders
    // those names verbatim. Guard against a raw graph id leaking into the
    // Services column.
    const model: ConsoleModel = {
      ...demoModel,
      source: "live",
      vulnerabilities: [
        {
          id: "GHSA-zzzz",
          package: "axios",
          severity: "high",
          cvss: 7.5,
          kev: false,
          fixedVersion: null,
          services: ["api-node-boats"]
        }
      ]
    };

    renderPage(model);

    expect(screen.getByText("api-node-boats")).toBeInTheDocument();
    expect(screen.queryByText(/^repository[:_]/)).not.toBeInTheDocument();
  });
});
