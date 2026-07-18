import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { VulnerabilitiesPage } from "./VulnerabilitiesPage";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

function renderPage(model: ConsoleModel): void {
  render(
    <MemoryRouter>
      <VulnerabilitiesPage model={model} />
    </MemoryRouter>,
  );
}

describe("VulnerabilitiesPage status summary", () => {
  it("surfaces populated catalog intelligence on the zero-impact landing state", () => {
    const retainedShape: ConsoleModel = {
      ...demoModel,
      source: "live",
      vulnerabilities: [],
      advisories: Array.from({ length: 5 }, (_, index) => ({
        ...demoModel.advisories[0],
        id: `CVE-2026-000${index + 1}`,
      })),
      advisoryCatalogSummary: { count: 5, limit: 50, truncated: false },
      provenance: {
        ...demoModel.provenance,
        advisories: "live",
        vulnerabilities: "empty",
      },
    };

    renderPage(retainedShape);

    const summary = screen.getByRole("region", { name: "Vulnerability view status" });
    expect(summary).toHaveTextContent("Reachable impact");
    expect(summary).toHaveTextContent("0 affected services proven");
    expect(summary).toHaveTextContent("No affected service is proven by impact findings.");
    expect(summary).toHaveTextContent("Known intelligence");
    expect(summary).toHaveTextContent("5 known advisories");
    expect(summary).toHaveTextContent("Catalog intelligence only; reachability is not implied.");
    expect(screen.getByRole("tab", { name: "Reachable in services" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("does not claim service impact for an affected finding without service evidence", () => {
    const repositoryOnly: ConsoleModel = {
      ...demoModel,
      source: "live",
      vulnerabilities: [
        {
          ...demoModel.vulnerabilities[0],
          affectedServices: [],
          findingId: "finding:repository-only",
          serviceIds: [],
          services: [],
        },
      ],
      provenance: { ...demoModel.provenance, vulnerabilities: "live" },
    };

    renderPage(repositoryOnly);

    const summary = screen.getByRole("region", { name: "Vulnerability view status" });
    expect(summary).toHaveTextContent("0 affected services proven");
    expect(summary).toHaveTextContent("1 impact finding has no admitted service evidence.");
    expect(summary).not.toHaveTextContent("admitted service-impact evidence");
  });

  it("reports service-backed findings and preserves mixed no-service truth", () => {
    const mixed: ConsoleModel = {
      ...demoModel,
      source: "live",
      vulnerabilities: [
        {
          ...demoModel.vulnerabilities[0],
          findingId: "finding:service-backed",
          services: ["checkout-service", "checkout-service"],
        },
        {
          ...demoModel.vulnerabilities[0],
          findingId: "finding:repository-only",
          services: [],
        },
      ],
      provenance: { ...demoModel.provenance, vulnerabilities: "live" },
    };

    renderPage(mixed);

    const summary = screen.getByRole("region", { name: "Vulnerability view status" });
    expect(summary).toHaveTextContent("1 service-backed finding");
    expect(summary).toHaveTextContent("2 impact findings; 1 has no admitted service evidence.");
  });

  it("does not treat a truncated first page as the full catalog count", () => {
    const truncated: ConsoleModel = {
      ...demoModel,
      source: "live",
      advisoryCatalogSummary: { count: 50, limit: 50, truncated: true },
      advisoryCatalogNextCursor: {
        after_advisory_key: "CVE-2021-44228",
        after_cvss: 10,
      },
      provenance: { ...demoModel.provenance, advisories: "live" },
    };

    renderPage(truncated);

    const summary = screen.getByRole("region", { name: "Vulnerability view status" });
    expect(summary).toHaveTextContent("50+ known advisories");
    expect(summary).toHaveTextContent("Bounded first page; more catalog entries are available.");
    expect(summary).toHaveTextContent("Catalog intelligence only; reachability is not implied.");
    expect(summary).not.toHaveTextContent("50 total");
  });

  it.each([
    { provenance: "loading", label: "Loading catalog intelligence" },
    { provenance: "unavailable", label: "Catalog intelligence unavailable" },
    { provenance: "empty", label: "0 known advisories" },
  ] as const)(
    "renders catalog $provenance independently from reachable impact",
    ({ provenance, label }) => {
      const model: ConsoleModel = {
        ...demoModel,
        source: "live",
        advisories: [],
        vulnerabilities: [],
        advisoryCatalogSummary:
          provenance === "empty" ? { count: 0, limit: 50, truncated: false } : null,
        provenance: {
          ...demoModel.provenance,
          advisories: provenance,
          vulnerabilities: "empty",
        },
      };

      renderPage(model);

      const summary = screen.getByRole("region", { name: "Vulnerability view status" });
      expect(summary).toHaveTextContent(label);
      expect(summary).toHaveTextContent("0 affected services proven");
    },
  );
});
