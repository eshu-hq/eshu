import { fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { DashboardPage } from "./DashboardPage";
import { demoModel } from "../console/demoModel";

describe("DashboardPage", () => {
  it("renders runtime stat tiles and panels from the model", () => {
    render(<DashboardPage model={demoModel} />);

    expect(screen.getByText("Repositories")).toBeInTheDocument();
    expect(screen.getByText("Index status")).toBeInTheDocument();
    expect(screen.getByText("Queue outstanding")).toBeInTheDocument();
    expect(screen.getByText("Succeeded")).toBeInTheDocument();
    // Index status value from runtime.indexStatus.
    expect(screen.getByText("complete")).toBeInTheDocument();

    expect(
      screen.getByText("Code-to-cloud relationship atlas")
    ).toBeInTheDocument();
    expect(screen.getByText("Relationship coverage")).toBeInTheDocument();
    expect(screen.getByText("Needs attention")).toBeInTheDocument();
  });

  it("opens the entity behind a finding row", () => {
    const onOpenService = vi.fn();
    render(<DashboardPage model={demoModel} onOpenService={onOpenService} />);

    fireEvent.click(screen.getByText("CVE-2024-0001 reachable in prod image"));

    expect(onOpenService).toHaveBeenCalledWith("checkout-service");
  });
});
