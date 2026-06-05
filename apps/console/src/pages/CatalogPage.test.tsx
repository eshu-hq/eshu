import { fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { CatalogPage } from "./CatalogPage";
import { demoModel } from "../console/demoModel";
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

  it("labels the source as demo fixtures vs live", () => {
    const { unmount } = render(<CatalogPage model={demoModel} />);
    expect(screen.getByText("demo fixtures")).toBeInTheDocument();
    unmount();

    const live: ConsoleModel = { ...demoModel, source: "live" };
    render(<CatalogPage model={live} />);
    expect(screen.getByText("live from /api/v0/catalog")).toBeInTheDocument();
  });
});
