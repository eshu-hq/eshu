import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";
import { IacPage } from "./IacPage";
import { demoModel } from "../console/demoModel";
import type { EshuApiClient } from "../api/client";
import type { ConsoleModel } from "../console/types";

function envelope(resources: readonly Record<string, unknown>[], opts: { readonly truncated?: boolean; readonly afterName?: string; readonly afterId?: string } = {}) {
  return {
    data: {
      count: resources.length,
      kind: "resource",
      limit: 25,
      resources,
      truncated: opts.truncated === true,
      next_cursor: opts.truncated === true ? { after_name: opts.afterName ?? "next", after_id: opts.afterId ?? "id-next" } : undefined
    },
    error: null,
    truth: {
      capability: "iac_inventory.resources.list",
      freshness: { state: "fresh" },
      level: "exact",
      profile: "production"
    }
  };
}

function row(id: string, name: string) {
  return {
    id,
    kind: "resource",
    name,
    resource_name: name.split(".").at(-1),
    type: "aws_s3_bucket",
    provider: "aws",
    resource_service: "s3",
    resource_category: "storage",
    module: "audit",
    repo_id: "repository:r1",
    relative_path: "logging.tf",
    line_number: 12
  };
}

describe("IacPage", () => {
  it("renders the IaC inventory from the model", () => {
    render(<IacPage model={demoModel} />);

    expect(screen.getByRole("heading", { name: "IaC Inventory" })).toBeInTheDocument();
    expect(screen.getByText("GET /api/v0/iac/resources")).toBeInTheDocument();
    expect(screen.getByLabelText("IaC evidence workbench")).toBeInTheDocument();
    expect(screen.getByText("Resources (loaded)")).toBeInTheDocument();
    // Resource rows render with their Terraform type.
    expect(screen.getByText("module.\"checkout\".aws_iam_role.this")).toBeInTheDocument();
    expect(screen.getByText("aws_s3_bucket.assets")).toBeInTheDocument();
    // The filter controls are present and labeled for accessibility.
    expect(screen.getByLabelText("Search IaC resources")).toBeInTheDocument();
    expect(screen.getByLabelText("Filter by type")).toBeInTheDocument();
    expect(screen.getByLabelText("Filter by module")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Search IaC resources"), { target: { value: "s3" } });
    expect(screen.queryByText("module.\"checkout\".aws_iam_role.this")).not.toBeInTheDocument();
    expect(screen.getByText("aws_s3_bucket.assets")).toBeInTheDocument();
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

  it("shows demo fixture rows and does not call the API when sourceLabel is demo fixtures", async () => {
    const get = vi.fn();
    const client = { get } as unknown as EshuApiClient;

    render(<IacPage client={client} sourceLabel="demo fixtures" model={demoModel} />);

    expect(await screen.findByText("module.\"checkout\".aws_iam_role.this")).toBeInTheDocument();
    expect(screen.getByText("aws_s3_bucket.assets")).toBeInTheDocument();
    expect(screen.getByText("bounded page from the graph")).toBeInTheDocument();
    expect(get).not.toHaveBeenCalled();
  });

  it("loads and pages IaC resources directly from the live API", async () => {
    const get = vi
      .fn()
      .mockResolvedValueOnce(envelope([row("r1", "aws_s3_bucket.logs")], { truncated: true, afterName: "aws_s3_bucket.logs", afterId: "r1" }))
      .mockResolvedValueOnce(envelope([row("r2", "aws_s3_bucket.archive")]));
    const client = { get } as unknown as EshuApiClient;

    render(<IacPage client={client} model={{ ...demoModel, iacResources: [] }} />);

    await waitFor(() => expect(screen.getByText("aws_s3_bucket.logs")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Next/ }));

    await waitFor(() => expect(screen.getByText("aws_s3_bucket.archive")).toBeInTheDocument());
    const lastCall = get.mock.calls[get.mock.calls.length - 1][0] as string;
    expect(lastCall).toContain("after_name=aws_s3_bucket.logs");
    expect(lastCall).toContain("after_id=r1");
    expect(lastCall).not.toContain("offset");
  });
});
