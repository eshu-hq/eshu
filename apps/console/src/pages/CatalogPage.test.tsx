import { fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { CatalogPage } from "./CatalogPage";
import { demoModel } from "../console/demoModel";
import { emptyConsoleModel } from "../console/liveModel";
import type { ConsoleModel } from "../console/types";

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

  it("renders the Environments column from per-service catalog data", () => {
    // GET /api/v0/catalog resolves per-service environments from the graph's
    // TARGETS_ENVIRONMENT and WorkloadInstance evidence, so the column shows
    // real environments and an em-dash only when a service genuinely has none.
    render(<CatalogPage model={demoModel} />);

    expect(screen.getByRole("columnheader", { name: "Environments" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Repository" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Truth" })).toBeInTheDocument();

    const checkoutRow = screen.getByText("checkout-service").closest("tr");
    expect(checkoutRow).not.toBeNull();
    expect(checkoutRow).toHaveTextContent("prod-us-east-1");

    const libRow = screen.getByText("lib-common").closest("tr");
    expect(libRow).not.toBeNull();
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
});
