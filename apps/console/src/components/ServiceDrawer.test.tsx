import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { ServiceDrawer } from "./ServiceDrawer";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

function renderDrawer(model: ConsoleModel): void {
  render(
    <MemoryRouter>
      <ServiceDrawer name="checkout-service" model={model} onClose={() => {}} />
    </MemoryRouter>
  );
}

describe("ServiceDrawer drill-downs", () => {
  it("derives the findings count from the same vulnerabilities the rows list (no drift)", () => {
    const model: ConsoleModel = {
      ...demoModel,
      vulnerabilities: [
        { id: "CVE-2026-1", package: "left-pad", severity: "high", cvss: 8.1, kev: false, fixedVersion: "2.0.1", services: ["checkout-service"] },
        { id: "CVE-2026-2", package: "other", severity: "low", cvss: 3.1, kev: false, fixedVersion: null, services: ["payments-api"] }
      ]
    };
    renderDrawer(model);
    // Only one CVE affects checkout-service, so the count must read 1.
    const findingsBtn = screen.getByRole("button", { name: /Findings \(1\)/ });
    fireEvent.click(findingsBtn);
    expect(screen.getByText("CVE-2026-1")).toBeInTheDocument();
    expect(screen.queryByText("CVE-2026-2")).not.toBeInTheDocument();
    expect(screen.getByText(/1 high/)).toBeInTheDocument();
  });

  it("exposes blast-radius and callers drill-down controls", () => {
    renderDrawer(demoModel);
    expect(screen.getByRole("button", { name: /Blast radius/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Callers \/ importers/ })).toBeInTheDocument();
  });
});
