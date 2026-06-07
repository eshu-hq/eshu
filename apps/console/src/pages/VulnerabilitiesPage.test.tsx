import { render, screen, fireEvent } from "@testing-library/react";
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
  it("separates reachable findings from the known-intelligence catalog", () => {
    renderPage(demoModel);

    expect(screen.getByRole("heading", { name: "Vulnerabilities" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Reachable in services" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Known intelligence (catalog)" })).toBeInTheDocument();

    // Reachable tab is default: the impact-finding advisory renders.
    expect(screen.getByRole("link", { name: "CVE-2024-0001" })).toBeInTheDocument();
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

  it("shows catalog rows linking to the existing CVE detail page", () => {
    renderPage(demoModel);

    fireEvent.click(screen.getByRole("tab", { name: "Known intelligence (catalog)" }));

    const link = screen.getByRole("link", { name: /CVE-2021-44228/ });
    expect(link).toHaveAttribute("href", "/vulnerabilities/CVE-2021-44228");
    // Catalog provenance line distinguishes it from reachable impact.
    expect(screen.getByText(/GET \/api\/v0\/supply-chain\/advisories/)).toBeInTheDocument();
  });

  it("renders the catalog empty state when no advisories and no client", () => {
    const empty: ConsoleModel = {
      ...demoModel,
      advisories: [],
      provenance: { ...demoModel.provenance, advisories: "empty" }
    };
    renderPage(empty);

    fireEvent.click(screen.getByRole("tab", { name: "Known intelligence (catalog)" }));
    expect(
      screen.getByText("No catalog advisories yet — requires the vulnerability-intelligence collector.")
    ).toBeInTheDocument();
  });

  it("renders the catalog unavailable state on provenance failure", () => {
    const failed: ConsoleModel = {
      ...demoModel,
      advisories: [],
      provenance: { ...demoModel.provenance, advisories: "unavailable" }
    };
    renderPage(failed);

    fireEvent.click(screen.getByRole("tab", { name: "Known intelligence (catalog)" }));
    expect(
      screen.getByText(/The vulnerability-intelligence catalog is unavailable/)
    ).toBeInTheDocument();
  });
});
