import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { FindingsPage } from "./FindingsPage";
import { demoModel } from "../console/demoModel";
import { emptyConsoleModel } from "../console/liveModel";
import type { ConsoleModel, VulnRow } from "../console/types";

describe("FindingsPage", () => {
  it("summarizes findings from the model", () => {
    renderFindings(demoModel);

    expect(screen.getByRole("heading", { name: "Findings" })).toBeInTheDocument();
    expect(screen.getByText("Open findings")).toBeInTheDocument();

    // Finding titles render once each in the table.
    expect(screen.getByText("CVE-2024-0001 reachable in prod image")).toBeInTheDocument();
    expect(screen.getByText("Unreferenced symbol legacyDiscount")).toBeInTheDocument();

    // Each type shows once as a summary tile label and once as a row badge.
    expect(screen.getAllByText("Dead code").length).toBeGreaterThan(1);
    expect(screen.getAllByText("Vulnerability").length).toBeGreaterThan(1);
    expect(screen.getByLabelText("Findings source status")).toHaveAttribute("data-row-count", "3");
    expect(document.querySelectorAll("[data-finding-row]")).toHaveLength(3);
  });

  it("renders the empty state when the model has no findings", () => {
    const empty: ConsoleModel = { ...demoModel, findings: [], vulnerabilities: [] };
    renderFindings(empty);

    expect(screen.getByText("No findings from this source.")).toBeInTheDocument();
    expect(screen.getByText("No findings from this source.")).toHaveAttribute(
      "data-authoritative-empty",
      "true",
    );
  });

  it("joins vulnerability rows into the actionable findings worklist", () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        {
          id: "dead-1",
          type: "Dead code",
          entity: "svc-catalog",
          title: "Unreferenced symbol unusedRoute",
          detail: "server/routes.ts · unused",
          truth: "derived",
          entityId: "content-entity:e1",
          filePath: "server/routes.ts",
        },
      ],
      vulnerabilities: [
        {
          id: "CVE-2026-1234",
          package: "lodash",
          severity: "high",
          cvss: 8.1,
          kev: true,
          fixedVersion: "4.17.22",
          services: ["svc-catalog"],
        },
      ],
    };

    renderFindings(model);

    expect(screen.getByText("CVE-2026-1234 · lodash")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open CVE" })).toHaveAttribute(
      "href",
      "/vulnerabilities/CVE-2026-1234",
    );
    expect(screen.getByRole("link", { name: "Open graph" })).toHaveAttribute(
      "href",
      "/code-graph?candidate=dead-1",
    );
    expect(screen.getAllByRole("link", { name: "Explore entity" })[0]).toHaveAttribute(
      "href",
      "/explorer?q=svc-catalog",
    );
  });

  it("shows the live source contract for each worklist row", () => {
    const model: ConsoleModel = {
      ...demoModel,
      source: "live",
      findings: [
        {
          id: "dead-1",
          type: "Dead code",
          entity: "svc-catalog",
          title: "Unreferenced symbol unusedRoute",
          detail: "server/routes.ts · unused",
          truth: "derived",
          entityId: "content-entity:e1",
          filePath: "server/routes.ts",
        },
      ],
      vulnerabilities: [
        {
          id: "CVE-2026-1234",
          package: "lodash",
          severity: "high",
          cvss: 8.1,
          kev: false,
          fixedVersion: null,
          services: ["svc-catalog"],
        },
      ],
    };

    renderFindings(model);

    expect(screen.getByRole("columnheader", { name: "Source" })).toBeInTheDocument();
    expect(screen.getByText("live worklist rows")).toBeInTheDocument();
    expect(sourceCells("POST /api/v0/code/dead-code")).toHaveLength(1);
    expect(sourceCells("GET /api/v0/supply-chain/impact/findings")).toHaveLength(1);
  });

  it("renders vulnerability rows when dead-code section is unavailable (partial failure)", () => {
    // Regression for review comment on PR #3409: the old single-provenance
    // coalescing picked "unavailable" from provenance.findings and suppressed
    // the whole table even though vulnerability rows were present from the
    // supply-chain section that succeeded. combinedWorklist() now requires ALL
    // sources to fail before blocking the table.
    const vuln: VulnRow = {
      id: "CVE-2026-9999",
      package: "express",
      severity: "high",
      cvss: 7.5,
      kev: false,
      fixedVersion: "4.19.0",
      services: ["api-service"],
    };
    const model: ConsoleModel = {
      ...emptyConsoleModel(),
      source: "live",
      vulnerabilities: [vuln],
      // dead-code section failed; supply-chain section succeeded
      provenance: { findings: "unavailable", vulnerabilities: "live" },
    };

    renderFindings(model);

    // Table stays usable, but the failed source must remain visible.
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Dead-code findings are unavailable");
    expect(screen.getByText("CVE-2026-9999 · express")).toBeInTheDocument();
  });

  it("does not present a partial failure with zero available rows as a valid empty worklist", () => {
    const model: ConsoleModel = {
      ...emptyConsoleModel(),
      source: "live",
      provenance: { findings: "unavailable", vulnerabilities: "empty" },
    };

    renderFindings(model);

    expect(screen.getByRole("alert")).toHaveTextContent("Dead-code findings are unavailable");
    expect(screen.getByText("No findings from the available source.")).toBeInTheDocument();
    expect(screen.queryByText("No findings from this source.")).not.toBeInTheDocument();
    expect(statValue("Open findings")).toHaveTextContent("—");
    expect(statValue("Dead code")).toHaveTextContent("—");
    expect(statValue("Vulnerability")).toHaveTextContent("0");
    expect(statValue("Types")).toHaveTextContent("—");
  });

  it("shows a loading spinner when both worklist sources are in flight", () => {
    const model: ConsoleModel = emptyConsoleModel("loading");
    renderFindings(model);

    expect(screen.getByRole("status", { name: "Loading findings" })).toBeInTheDocument();
    expect(screen.queryByText("No findings from this source.")).not.toBeInTheDocument();
    expect(statValue("Open findings")).toHaveTextContent("—");
    expect(statValue("Dead code")).toHaveTextContent("—");
    expect(statValue("Vulnerability")).toHaveTextContent("—");
    expect(statValue("Types")).toHaveTextContent("—");
  });

  it("keeps ready dead-code rows visible while vulnerabilities are still loading", () => {
    const model: ConsoleModel = {
      ...emptyConsoleModel(),
      source: "live",
      findings: [
        {
          id: "dead-ready",
          type: "Dead code",
          entity: "ready-service",
          title: "Ready finding",
          detail: "ready.ts · unused",
          truth: "derived",
        },
      ],
      provenance: { findings: "live", vulnerabilities: "loading" },
    };

    renderFindings(model);

    expect(screen.getByText("Ready finding")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent(
      "Reachable vulnerability findings are still loading",
    );
  });

  it("shows an error when both worklist sources are unavailable", () => {
    const model: ConsoleModel = emptyConsoleModel("unavailable");
    renderFindings(model);

    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.getByText(/Findings data unavailable/i)).toBeInTheDocument();
    expect(screen.queryByText("No findings from this source.")).not.toBeInTheDocument();
  });
});

function renderFindings(model: ConsoleModel): ReturnType<typeof render> {
  return render(
    <MemoryRouter>
      <FindingsPage model={model} />
    </MemoryRouter>,
  );
}

function sourceCells(text: string): readonly HTMLElement[] {
  return screen.getAllByText(text).filter((element) => element.tagName === "TD");
}

function statValue(label: string): HTMLElement {
  const tile = screen.getByText(label).closest(".stat-tile");
  if (!tile) throw new Error(`missing stat tile: ${label}`);
  const value = tile.querySelector("strong");
  if (!(value instanceof HTMLElement)) throw new Error(`missing stat value: ${label}`);
  return value;
}
