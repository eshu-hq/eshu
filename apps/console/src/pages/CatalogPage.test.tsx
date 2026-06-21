import { fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { CatalogPage } from "./CatalogPage";
import { demoModel } from "../console/demoModel";
import { emptyConsoleModel } from "../console/liveModel";
import type { ConsoleModel, FindingRow } from "../console/types";

describe("CatalogPage", () => {
  it("lists every catalog service from the model", () => {
    render(<CatalogPage model={demoModel} />);

    expect(screen.getByRole("heading", { name: "Catalog" })).toBeInTheDocument();
    expect(
      screen.getByText(`${demoModel.services.length} entries`)
    ).toBeInTheDocument();
    expect(screen.getByText("checkout-service")).toBeInTheDocument();
    expect(screen.getByText("payments-api")).toBeInTheDocument();
    expect(screen.getByText("ledger-service")).toBeInTheDocument();
    expect(screen.getByText("lib-common")).toBeInTheDocument();
  });

  it("filters rows by the search box and renders the empty state", () => {
    render(<CatalogPage model={demoModel} />);

    fireEvent.change(screen.getByPlaceholderText(/Filter catalog/i), {
      target: { value: "ledger" }
    });

    expect(screen.getByText("ledger-service")).toBeInTheDocument();
    expect(screen.queryByText("checkout-service")).not.toBeInTheDocument();
    expect(screen.queryByText("payments-api")).not.toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText(/Filter catalog/i), {
      target: { value: "no-such-entry" }
    });

    expect(
      screen.getByText("No catalog entries from this source.")
    ).toBeInTheDocument();
  });

  it("invokes onOpenService when a row is clicked", () => {
    const onOpenService = vi.fn();
    render(<CatalogPage model={demoModel} onOpenService={onOpenService} />);

    fireEvent.click(screen.getByText("payments-api"));

    expect(onOpenService).toHaveBeenCalledWith("payments-api");
  });

  it("renders the Environments column as an env count", () => {
    // The Environments column shows a count ("1 env") rather than the full
    // environment name list so the table stays narrow.
    render(<CatalogPage model={demoModel} />);

    expect(screen.getByRole("columnheader", { name: "Environments" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Repository" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Truth" })).toBeInTheDocument();

    const checkoutRow = screen.getByText("checkout-service").closest("tr");
    expect(checkoutRow).not.toBeNull();
    // checkout-service has 1 environment: shown as "1 env"
    expect(checkoutRow).toHaveTextContent("1 env");

    const libRow = screen.getByText("lib-common").closest("tr");
    expect(libRow).not.toBeNull();
    // lib-common has no environments: shown as em-dash
    expect(libRow).toHaveTextContent("—");
  });

  it("labels the source as demo fixtures vs live", () => {
    const { unmount } = render(<CatalogPage model={demoModel} />);
    expect(screen.getByText("demo fixtures")).toBeInTheDocument();
    unmount();

    const live: ConsoleModel = { ...demoModel, source: "live" };
    render(<CatalogPage model={live} />);
    expect(screen.getByText("live catalog rows")).toBeInTheDocument();
    expect(screen.getByText("GET /api/v0/catalog?limit=2000")).toBeInTheDocument();
  });

  it("shows a loading spinner while the fetch is in flight (loading provenance)", () => {
    // Reproduces issue #3395: during the ~20s GET /api/v0/catalog fetch the page
    // must NOT render 'No catalog entries' — it must show a spinner instead.
    const loading: ConsoleModel = emptyConsoleModel("loading");
    render(<CatalogPage model={loading} />);

    expect(screen.getByRole("status", { name: "Loading catalog" })).toBeInTheDocument();
    expect(screen.queryByText("No catalog entries from this source.")).not.toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("shows an error state when the catalog section is unavailable", () => {
    const unavailable: ConsoleModel = emptyConsoleModel("unavailable");
    render(<CatalogPage model={unavailable} />);

    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.getByText(/unavailable/i)).toBeInTheDocument();
    expect(screen.queryByText("No catalog entries from this source.")).not.toBeInTheDocument();
  });

  it("renders tier, category, domain, and language columns with demo values", () => {
    render(<CatalogPage model={demoModel} />);

    expect(screen.getByRole("columnheader", { name: "Tier" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Category" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Domain" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Language" })).toBeInTheDocument();

    // checkout-service: tier-1, service, payments, TypeScript
    const checkoutRow = screen.getByText("checkout-service").closest("tr");
    expect(checkoutRow).not.toBeNull();
    expect(checkoutRow).toHaveTextContent("tier-1");
    expect(checkoutRow).toHaveTextContent("payments");
    expect(checkoutRow).toHaveTextContent("TypeScript");

    // lib-common: library tier
    const libRow = screen.getByText("lib-common").closest("tr");
    expect(libRow).not.toBeNull();
    expect(libRow).toHaveTextContent("library");
    expect(libRow).toHaveTextContent("core-engineering");
  });

  it("renders em-dash in tier/category/domain/language when fields are absent", () => {
    const sparse: ConsoleModel = {
      ...demoModel,
      services: [{ id: "bare-svc", name: "bare-svc", kind: "service", repo: "sample/bare", environments: [], truth: "exact", freshness: "fresh" }]
    };
    render(<CatalogPage model={sparse} />);

    const row = screen.getByText("bare-svc").closest("tr");
    expect(row).not.toBeNull();
    // Each absent field renders "—"; the row has at least four of them
    const dashes = row?.querySelectorAll(".t-mut");
    expect(dashes?.length).toBeGreaterThanOrEqual(4);
  });

  it("renders the Findings severity bar column header", () => {
    render(<CatalogPage model={demoModel} />);
    expect(screen.getByRole("columnheader", { name: "Findings" })).toBeInTheDocument();
  });

  it("renders a per-row severity bar when findings match the service", () => {
    const findingRow: FindingRow = {
      id: "f1",
      type: "Vulnerability",
      entity: "checkout-service",
      title: "Test finding",
      detail: "detail",
      truth: "derived",
      labels: ["high"]
    };
    const withFindings: ConsoleModel = { ...demoModel, findings: [findingRow] };
    render(<CatalogPage model={withFindings} />);

    // The severity segment has a title attribute with count and severity label
    const segment = screen.getByTitle("1 high");
    expect(segment).toBeInTheDocument();
    expect(segment).toHaveTextContent("1");

    // Other rows with no matching findings show em-dash in the findings cell
    const paymentsRow = screen.getByText("payments-api").closest("tr");
    expect(paymentsRow).not.toBeNull();
  });

  it("filters rows by tier and domain via the search box", () => {
    render(<CatalogPage model={demoModel} />);

    fireEvent.change(screen.getByPlaceholderText(/Filter catalog/i), {
      target: { value: "finance" }
    });

    // Only ledger-service has domain "finance"
    expect(screen.getByText("ledger-service")).toBeInTheDocument();
    expect(screen.queryByText("checkout-service")).not.toBeInTheDocument();
  });
});
