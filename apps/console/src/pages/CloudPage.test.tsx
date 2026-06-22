import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { CloudPage } from "./CloudPage";
import type { EshuApiClient } from "../api/client";

// CloudPage renders live cloud inventory from GET /api/v0/cloud/resources. It must
// show real rows, expose the keyset Next control only when more pages exist, and
// forward the server cursor (not an offset) on Next.
function envelope(resources: unknown[], opts: { truncated: boolean; after?: string }) {
  return {
    data: {
      resources,
      count: resources.length,
      limit: 50,
      truncated: opts.truncated,
      next_cursor: opts.truncated
        ? { after_resource_type: "aws_iam_role", after_id: opts.after ?? "last" }
        : undefined
    },
    error: null,
    truth: {
      profile: "production",
      level: "exact",
      capability: "platform_impact.cloud_resource_list",
      freshness: { state: "fresh" }
    }
  };
}

function inventoryEnvelope(resources: unknown[] = []) {
  return {
    data: {
      resources,
      count: resources.length,
      limit: 50,
      truncated: false
    },
    error: null,
    truth: {
      profile: "production",
      level: "exact",
      capability: "cloud_inventory.readback.list",
      freshness: { state: "fresh" }
    }
  };
}

function row(id: string, name: string) {
  return {
    id,
    resource_type: "aws_iam_role",
    name,
    provider: "aws",
    region: "us-east-1",
    account_id: "123456789012",
    arn: `arn:aws:iam::123456789012:role/${name}`,
    service_name: "iam",
    state: "active"
  };
}

describe("CloudPage", () => {
  it("renders live rows and the truth chip from the envelope", async () => {
    const client = {
      get: vi.fn(async () => envelope([row("r1", "role-a"), row("r2", "role-b")], { truncated: false }))
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/cloud"]}>
        <CloudPage client={client} />
      </MemoryRouter>
    );

    expect(screen.getByRole("heading", { name: "Cloud" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("role-a")).toBeInTheDocument());
    expect(screen.getByText("role-b")).toBeInTheDocument();
    // truth chip text comes from the envelope level (exact).
    expect(screen.getAllByTitle("Truth: exact").length).toBeGreaterThan(0);
    expect(screen.getByText("Resources by family")).toBeInTheDocument();
    expect(screen.getAllByText("Accounts").length).toBeGreaterThan(0);
    expect(screen.getByText("Endpoint")).toBeInTheDocument();
    expect(screen.getAllByText("/api/v0/cloud/resources").length).toBeGreaterThan(0);
    expect(screen.getByText("Network topology")).toBeInTheDocument();
  });

  it("forwards the keyset cursor (not an offset) when paging next", async () => {
    const get = vi.fn(async (path: string) => {
      if (path.includes("/api/v0/cloud/inventory")) return inventoryEnvelope();
      if (path.includes("after_id=r1")) return envelope([row("r2", "role-b")], { truncated: false });
      return envelope([row("r1", "role-a")], { truncated: true, after: "r1" });
    });
    const client = { get } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/cloud"]}>
        <CloudPage client={client} />
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText("role-a")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Next/ }));

    await waitFor(() => expect(screen.getByText("role-b")).toBeInTheDocument());
    const lastCall = get.mock.calls[get.mock.calls.length - 1][0] as string;
    expect(lastCall).toContain("after_id=r1");
    expect(lastCall).not.toContain("offset");
  });

  it("applies a filter on submit and forwards it to the API", async () => {
    const paths: string[] = [];
    const get = vi.fn(async (path: string) => {
      paths.push(path);
      return envelope([row("r1", "role-a")], { truncated: false });
    });
    const client = { get } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/cloud"]}>
        <CloudPage client={client} />
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText("role-a")).toBeInTheDocument());
    fireEvent.change(screen.getByLabelText("resource type filter"), {
      target: { value: "aws_s3_bucket" }
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() => expect(paths.some((path) =>
      path.includes("/api/v0/cloud/resources") && path.includes("resource_type=aws_s3_bucket")
    )).toBe(true));
  });

  it("switches to the demo-style table grouped by resource family", async () => {
    const client = {
      get: vi.fn(async () => envelope([
        { ...row("r1", "role-a"), resource_type: "aws_iam_role" },
        { ...row("r2", "bucket-a"), resource_type: "aws_s3_bucket" }
      ], { truncated: false }))
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/cloud"]}>
        <CloudPage client={client} />
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText("role-a")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Table" }));

    expect(screen.getAllByText("Identity & access").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Storage").length).toBeGreaterThan(0);
    expect(screen.getByText(/Grouped by family/)).toBeInTheDocument();
  });

  it("renders an error state when the endpoint fails", async () => {
    const client = {
      get: vi.fn(async () => {
        throw new Error("HTTP 503");
      })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/cloud"]}>
        <CloudPage client={client} />
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText(/Failed to load: HTTP 503/)).toBeInTheDocument());
  });
});
