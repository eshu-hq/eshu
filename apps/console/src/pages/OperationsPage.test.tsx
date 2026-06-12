import { render, screen } from "@testing-library/react";
import { OperationsPage } from "./OperationsPage";
import { demoModel } from "../console/demoModel";

describe("OperationsPage", () => {
  it("labels repository language inventory with the aggregate endpoint", () => {
    render(<OperationsPage model={demoModel} />);

    expect(screen.getByText("GET /api/v0/repositories/language-inventory")).toBeInTheDocument();
    expect(screen.queryByText("GET /api/v0/repositories/by-language")).not.toBeInTheDocument();
  });
});
