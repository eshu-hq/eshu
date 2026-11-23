import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "./App";

describe("App", () => {
  it("renders the redesigned hero, 2D graph, roles, fused rollout, bento surfaces, and footer", () => {
    render(<App />);

    expect(screen.getByRole("banner")).toBeInTheDocument();
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    expect(
      screen.getByRole("heading", {
        level: 1,
        name: /Your living organizational knowledge base/i,
      }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Source to runtime graph")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", {
        name: /Every role asks the same living graph/i,
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", {
        name: /How it works — and how teams roll it out safely/i,
      }),
    ).toBeInTheDocument();
    expect(screen.getByRole("contentinfo")).toBeInTheDocument();
  });

  it("supports interactive roles, 2D hero traces, capability tabs, collector filters, workbench, quickstart, and theme toggle", () => {
    render(<App />);

    fireEvent.click(screen.getByRole("tab", { name: "Software Engineer" }));
    expect(screen.getByText(/who calls ChargeCustomer/i)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: "Exposure" }));
    expect(screen.getByText(/trace_exposure_path/i)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: "Supply Chain" }));
    expect(
      screen.getByRole("heading", {
        name: /CI-ready standalone scanner \(SARIF & VEX\)/i,
      }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: /Observability & Semantic/i }));
    expect(screen.getByText("Grafana Loki")).toBeInTheDocument();
    expect(screen.queryByText("Git & Repository Intake")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /eshu docs verify docs\/runbook\.md/i }));
    expect(screen.getByText(/Checked claims: 14/i)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: /2\. Full stack \(eshu-deploy\)/i }));
    expect(
      screen.getByText(/git clone https:\/\/github\.com\/eshu-hq\/eshu-deploy/i),
    ).toBeInTheDocument();

    const toggle = screen.getByRole("button", {
      name: /Switch to warm light theme/i,
    });
    fireEvent.click(toggle);
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });
});
