import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, vi } from "vitest";

import { App } from "./App";
import { demoModel } from "./console/demoModel";

describe("App demo mode", () => {
  afterEach(() => {
    window.localStorage.clear();
    vi.unstubAllGlobals();
  });

  it("renders demo impact evidence without a live fetch", async () => {
    renderDemoRoute("/impact");

    expect(await screen.findByRole("heading", { level: 2, name: "Impact" })).toBeInTheDocument();
    expect(screen.getByText("Demo fixtures")).toBeInTheDocument();
    expect((await screen.findAllByText("payments-api")).length).toBeGreaterThan(0);
    expect(
      screen.getByText(
        "Demo fixture traces checkout-service from repository workflow to image, workload, and cloud resources.",
      ),
    ).toBeInTheDocument();
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("renders demo CI/CD correlations without a live fetch", async () => {
    renderDemoRoute("/ci-cd/run-correlations");

    expect(
      await screen.findByRole("heading", { level: 2, name: "CI/CD run correlations" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Demo fixtures")).toBeInTheDocument();
    expect(await screen.findByText("1234")).toBeInTheDocument();
    expect(
      screen.getByText("workflow artifact digest matched deployed checkout image"),
    ).toBeInTheDocument();
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("renders demo drift and supply-chain evidence without a live fetch", async () => {
    renderDemoRoute("/cloud-drift");

    expect(
      await screen.findByRole("heading", { level: 2, name: "Cloud Drift" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByText("module.checkout.aws_iam_role.checkout_task"),
    ).toBeInTheDocument();
    expect(screen.getAllByText("observed_without_declaration").length).toBeGreaterThan(0);
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("renders demo SBOM and dependency inventory without a live fetch", async () => {
    renderDemoRoute("/sbom");

    expect(
      await screen.findByRole("heading", { level: 2, name: "SBOM & Attestations" }),
    ).toBeInTheDocument();
    expect(await screen.findByText("1 subjects")).toBeInTheDocument();
    expect(screen.getByText("demo fixtures")).toBeInTheDocument();
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("keeps the private API input on the live proxy when switching from demo", async () => {
    renderDemoRoute("/cloud");

    expect(await screen.findByText("aws_lb.frontend")).toBeInTheDocument();
    expect(screen.getByText("Prospect demo")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Demo fixtures" }));

    const dialog = screen.getByRole("dialog", { name: "Data source" });
    const input = screen.getByPlaceholderText("/eshu-api/") as HTMLInputElement;
    expect(input.value).toBe("/eshu-api/");
    expect(within(dialog).queryByText("✓ connected")).not.toBeInTheDocument();
  });

  it("renders demo IaC inventory from the fixture model without unsupported API errors", async () => {
    renderDemoRoute("/iac");

    expect(
      await screen.findByRole("heading", { level: 2, name: "IaC Inventory" }),
    ).toBeInTheDocument();
    expect(screen.getByText("bounded page from the graph")).toBeInTheDocument();
    expect(screen.getByText('module."checkout".aws_iam_role.this')).toBeInTheDocument();
    expect(screen.queryByText(/Failed to load IaC resources/)).not.toBeInTheDocument();
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  // Regression coverage for issue #4746: guided questions run LIVE playbooks
  // against the live query-playbooks API, which is a distinct surface from the
  // demo_fixture provenance path exercised by every test above. Demo mode must
  // keep rendering its existing fixture provenance unchanged (no live fetch,
  // "Prospect demo" banner) on every other route, and the new guided-questions
  // route must explain it needs a live connection rather than silently
  // fabricating a fixture catalog or reaching the network.
  it("keeps the fixture demo_fixture provenance path unchanged and never fetches for guided questions", async () => {
    renderDemoRoute("/guided-questions");

    expect(await screen.findByText(/Guided questions need a live connection/i)).toBeInTheDocument();
    expect(screen.getByText("Prospect demo")).toBeInTheDocument();
    expect(globalThis.fetch).not.toHaveBeenCalled();

    // The demo fixture model itself (backing every other demo route above) is
    // untouched by this surface: every section is still explicitly "demo", not
    // silently promoted to a live-looking provenance.
    expect(Object.values(demoModel.provenance).every((sourceKind) => sourceKind === "demo")).toBe(
      true,
    );
  });
});

function renderDemoRoute(path: string): void {
  window.localStorage.setItem(
    "eshu.console.environment",
    JSON.stringify({ mode: "demo", apiBaseUrl: "", recentApiBaseUrls: [] }),
  );
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      throw new Error("demo must not call network fetch");
    }),
  );
  render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}
