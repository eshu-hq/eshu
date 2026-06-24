import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { CloudInventoryPanel } from "./CloudInventoryPanel";
import type { EshuApiClient } from "../api/client";

describe("CloudInventoryPanel", () => {
  it("renders canonical source-state rollups from the inventory readback", async () => {
    const get = vi.fn(async (_path: string) => ({
      data: {
        resources: [
          {
            cloud_resource_uid: "aws:111122223333:AWS::S3::Bucket:acme-prod",
            provider: "aws",
            resource_type: "AWS::S3::Bucket",
            management_origin: "declared",
            scope_id: "cloud-scope:aws:111122223333",
            generation_id: "aws-gen-1",
            source_state: "exact",
            evidence: { declared: true, applied: true, observed: false }
          },
          {
            cloud_resource_uid: "aws:111122223333:AWS::IAM::Role:runtime",
            provider: "aws",
            resource_type: "AWS::IAM::Role",
            management_origin: "observed",
            scope_id: "cloud-scope:aws:111122223333",
            generation_id: "aws-gen-1",
            source_state: "derived",
            evidence: { declared: false, applied: false, observed: true }
          }
        ],
        count: 2,
        limit: 50,
        truncated: false
      },
      error: null,
      truth: {
        capability: "cloud_inventory.readback.list",
        freshness: { state: "fresh" },
        level: "exact",
        profile: "production"
      }
    }));
    const client = { get } as unknown as EshuApiClient;

    render(<CloudInventoryPanel accountId="111122223333" client={client} provider="aws" />);

    await waitFor(() => expect(screen.getByText("Canonical inventory")).toBeInTheDocument());
    expect(screen.getByText("2 canonical identities")).toBeInTheDocument();
    expect(screen.getByText("exact · 1")).toBeInTheDocument();
    expect(screen.getByText("derived · 1")).toBeInTheDocument();
    expect(screen.getByText("AWS::S3::Bucket")).toBeInTheDocument();
    expect(screen.getByText("observed")).toBeInTheDocument();
    const path = get.mock.calls[0]?.[0] ?? "";
    expect(path).toContain("/api/v0/cloud/inventory");
    expect(path).toContain("account_id=111122223333");
    expect(path).toContain("provider=aws");
  });

  it("surfaces unsupported inventory readback instead of fabricating rows", async () => {
    const client = {
      get: vi.fn(async () => {
        throw new Error("unsupported capability");
      })
    } as unknown as EshuApiClient;

    render(<CloudInventoryPanel accountId="" client={client} provider="" />);

    await waitFor(() => expect(screen.getByText(/Canonical inventory unavailable/)).toBeInTheDocument());
  });
});
