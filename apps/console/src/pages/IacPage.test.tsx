import { render, screen } from "@testing-library/react";
import { IacPage } from "./IacPage";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

describe("IacPage", () => {
  it("renders the IaC inventory from the model", () => {
    render(<IacPage model={demoModel} />);

    expect(screen.getByRole("heading", { name: "IaC Inventory" })).toBeInTheDocument();
    expect(screen.getByLabelText("IaC evidence workbench")).toBeInTheDocument();
    expect(screen.getByText("Resources (loaded)")).toBeInTheDocument();
    // Resource rows render with their Terraform type.
    expect(screen.getByText("module.\"checkout\".aws_iam_role.this")).toBeInTheDocument();
    expect(screen.getByText("aws_s3_bucket.assets")).toBeInTheDocument();
    // The filter controls are present and labeled for accessibility.
    expect(screen.getByLabelText("Search IaC resources")).toBeInTheDocument();
    expect(screen.getByLabelText("Filter by type")).toBeInTheDocument();
    expect(screen.getByLabelText("Filter by module")).toBeInTheDocument();
  });

  it("renders the empty state when no resources are indexed", () => {
    const empty: ConsoleModel = { ...demoModel, iacResources: [] };
    render(<IacPage model={empty} />);
    expect(screen.getByText("No Terraform/IaC resources have been indexed yet.")).toBeInTheDocument();
  });

  it("renders the unavailable state when the section is unavailable", () => {
    const unavailable: ConsoleModel = {
      ...demoModel,
      iacResources: [],
      provenance: { ...demoModel.provenance, iacResources: "unavailable" }
    };
    render(<IacPage model={unavailable} />);
    expect(
      screen.getByText("IaC inventory is not available from this API (it requires the authoritative graph profile).")
    ).toBeInTheDocument();
  });
});
