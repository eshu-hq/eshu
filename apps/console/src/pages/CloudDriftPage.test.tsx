import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { EshuApiClient } from "../api/client";
import { CloudDriftPage } from "./CloudDriftPage";

function envelope(data: unknown, capability = "cloud_runtime_drift.readback.list") {
  return {
    data,
    error: null,
    truth: {
      capability,
      freshness: { state: "fresh" },
      level: "exact",
      profile: "production"
    }
  };
}

function fakeDriftClient(calls: Array<{ path: string; body: unknown }>): EshuApiClient {
  return {
    get: vi.fn(async (path: string) => {
      calls.push({ path, body: null });
      if (path.startsWith("/api/v0/investigations/drift/packet")) {
        return envelope({
          answer: { summary: "Runtime drift packet is supported.", supported: true, truth_class: "exact" },
          bounds: { max_source_facts: 200, truncated: false },
          graph_answers: [{ id: "graph:drift:1", present: true, relation: "MANAGED_BY_TERRAFORM" }],
          identity: { family: "drift", scope: { account_id: "123456789012", provider: "aws" } },
          missing_evidence: [{ hop: "terraform_state_resource", reason: "state evidence missing" }],
          packet_id: "investigation-evidence-packet:drift-demo",
          reducer_decisions: [{ id: "decision:drift:1", state: "rejected" }],
          redaction: { profile: "share_safe_v2" },
          reproduce: [{ kind: "http", route: "/api/v0/investigations/drift/packet" }],
          schema: "investigation_evidence_packet.v2",
          source_facts: [{ evidence_family: "cloud_runtime_drift", fact_id: "fact:drift:1" }],
          validation: { valid: true }
        }, "cloud_runtime_drift.packet");
      }
      throw new Error(`unexpected request ${path}`);
    }),
    post: vi.fn(async (path: string, body: unknown) => {
      calls.push({ path, body });
      if (path === "/api/v0/cloud/runtime-drift/findings") {
        return envelope({
          analysis_status: "materialized_multi_cloud_runtime_drift",
          drift_findings: [{
            fact_id: "fact:multi:1",
            provider: "aws",
            scope_id: "aws:123456789012:us-east-1:s3",
            generation_id: "gen-1",
            cloud_resource_uid: "cloud-resource:s3:payments-prod",
            finding_kind: "orphaned_cloud_resource",
            management_status: "cloud_only",
            confidence: 0.92,
            source_state: "derived",
            missing_evidence: ["terraform_state_resource"],
            recommended_action: "triage_owner_and_import_or_retire",
            safety_gate: { outcome: "read_only_allowed", review_required: false, refused_actions: [] }
          }],
          findings_count: 1,
          limit: 50,
          next_offset: 50,
          offset: 0,
          source_state_groups: [{ source_state: "derived", count: 1 }],
          story: "1 active multi-cloud runtime drift findings matched aws resources.",
          total_findings_count: 51,
          truncated: true
        });
      }
      if (path === "/api/v0/aws/runtime-drift/findings") {
        return envelope({
          drift_findings: [{
            id: "finding:aws:1",
            arn: "arn:aws:s3:::payments-prod",
            provider: "aws",
            account_id: "123456789012",
            region: "us-east-1",
            finding_kind: "orphaned_cloud_resource",
            management_status: "cloud_only",
            outcome: "derived",
            promotion_outcome: "not_promoted",
            promotion_reason: "read_model_only_no_ownership_promotion",
            confidence: 0.92,
            missing_evidence: ["terraform_state_resource"],
            safety_gate: { outcome: "read_only_allowed", review_required: false, refused_actions: [] }
          }],
          findings_count: 1,
          limit: 50,
          next_offset: null,
          offset: 0,
          outcome_groups: [{ outcome: "derived", count: 1 }],
          story: "1 active AWS runtime drift findings matched 123456789012.",
          total_findings_count: 1,
          truncated: false
        }, "aws_runtime_drift.findings");
      }
      if (path === "/api/v0/iac/unmanaged-resources") {
        return envelope({
          findings: [{
            id: "finding:aws:1",
            provider: "aws",
            account_id: "123456789012",
            region: "us-east-1",
            resource_type: "s3",
            resource_id: "payments-prod",
            arn: "arn:aws:s3:::payments-prod",
            finding_kind: "orphaned_cloud_resource",
            management_status: "cloud_only",
            confidence: 0.92,
            recommended_action: "triage_owner_and_import_or_retire",
            missing_evidence: ["terraform_state_resource"],
            warning_flags: ["raw_tags_provenance_only"],
            safety_gate: { outcome: "read_only_allowed", review_required: false, refused_actions: [] }
          }],
          findings_count: 1,
          finding_groups: [{ management_status: "cloud_only", finding_kind: "orphaned_cloud_resource", count: 1 }],
          limit: 50,
          next_offset: null,
          offset: 0,
          story: "1 active IaC management findings matched 123456789012.",
          total_findings_count: 1,
          truncated: false
        }, "iac.management.findings");
      }
      if (path === "/api/v0/iac/terraform-import-plan/candidates") {
        return envelope({
          candidates: [{
            id: "terraform-import:finding:aws:1",
            finding_id: "finding:aws:1",
            status: "ready",
            provider: "aws",
            account_id: "123456789012",
            region: "us-east-1",
            arn: "arn:aws:s3:::payments-prod",
            cloud_resource_type: "s3",
            terraform_resource_type: "aws_s3_bucket",
            suggested_resource_address: "aws_s3_bucket.payments_prod",
            import_id: "payments-prod",
            destination_hint: "create_import_block",
            configuration_shape: "operator_authored",
            safety_gate: { outcome: "read_only_allowed", review_required: false }
          }],
          candidates_count: 1,
          limit: 50,
          next_offset: null,
          offset: 0,
          ready_count: 1,
          refused_count: 0,
          story: "1 Terraform import-plan candidate is ready.",
          total_findings_count: 1,
          truncated: false
        }, "iac.terraform_import_plan");
      }
      if (path === "/api/v0/iac/management-status/explain") {
        return envelope({
          arn: "arn:aws:s3:::payments-prod",
          evidence_groups: [{
            count: 1,
            evidence: [{ id: "ev-1", evidence_type: "aws_cloud_resource", key: "arn", value: "arn:aws:s3:::payments-prod" }],
            layer: "cloud"
          }],
          finding: {
            arn: "arn:aws:s3:::payments-prod",
            finding_kind: "orphaned_cloud_resource",
            id: "finding:aws:1",
            management_status: "cloud_only"
          },
          safety_gate: { outcome: "read_only_allowed", review_required: false },
          story: "arn:aws:s3:::payments-prod is classified as cloud_only from orphaned evidence."
        }, "iac.management.explain");
      }
      throw new Error(`unexpected request ${path}`);
    })
  } as unknown as EshuApiClient;
}

describe("CloudDriftPage", () => {
  it("renders runtime drift, unmanaged resources, import links, truth, and exact status explanations", async () => {
    const calls: Array<{ path: string; body: unknown }> = [];
    render(
      <MemoryRouter initialEntries={["/cloud-drift?account_id=123456789012&provider=aws&region=us-east-1"]}>
        <CloudDriftPage client={fakeDriftClient(calls)} />
      </MemoryRouter>
    );

    expect(screen.getByRole("heading", { name: "Cloud Drift" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("cloud-resource:s3:payments-prod")).toBeInTheDocument());
    expect(screen.getByText("arn:aws:s3:::payments-prod")).toBeInTheDocument();
    expect(screen.getByText("cloud_only")).toBeInTheDocument();
    expect(screen.getByText("aws_s3_bucket.payments_prod")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open import context" })).toHaveAttribute(
      "href",
      "/replatforming?account_id=123456789012&region=us-east-1&arn=arn%3Aaws%3As3%3A%3A%3Apayments-prod"
    );
    expect(screen.getAllByTitle("Truth: exact").length).toBeGreaterThan(0);
    expect(screen.getByText("More multi-cloud drift available at offset 50")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Explain status for arn:aws:s3:::payments-prod" }));
    await waitFor(() => expect(screen.getByText(/classified as cloud_only/)).toBeInTheDocument());
    expect(screen.getByText("aws_cloud_resource")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Load drift evidence packet" }));
    await waitFor(() => expect(screen.getByText("investigation-evidence-packet:drift-demo")).toBeInTheDocument());
    expect(screen.getAllByText("terraform_state_resource").length).toBeGreaterThan(0);
    expect(calls.some((call) =>
      call.path === "/api/v0/investigations/drift/packet?account_id=123456789012&provider=aws&max_source_facts=50"
    )).toBe(true);

    expect(calls.some((call) => call.path === "/api/v0/iac/management-status/explain")).toBe(true);
  });

  it("forwards provider, account, region, and offset filters to bounded POST surfaces", async () => {
    const calls: Array<{ path: string; body: unknown }> = [];
    render(
      <MemoryRouter initialEntries={["/cloud-drift"]}>
        <CloudDriftPage client={fakeDriftClient(calls)} />
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText("Provider filter"), { target: { value: "aws" } });
    fireEvent.change(screen.getByLabelText("Account ID filter"), { target: { value: "123456789012" } });
    fireEvent.change(screen.getByLabelText("Region filter"), { target: { value: "us-east-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Load drift findings" }));

    await waitFor(() => expect(calls.length).toBeGreaterThanOrEqual(4));
    const awsCall = calls.find((call) => call.path === "/api/v0/aws/runtime-drift/findings");
    expect(awsCall?.body).toMatchObject({
      account_id: "123456789012",
      limit: 50,
      offset: 0,
      region: "us-east-1"
    });

    fireEvent.click(screen.getByRole("button", { name: "Next multi-cloud drift page" }));
    await waitFor(() => expect(calls.some((call) =>
      call.path === "/api/v0/cloud/runtime-drift/findings" &&
      (call.body as { readonly offset?: number }).offset === 50
    )).toBe(true));
  });

  it("shows an explicit scope prompt instead of calling scope-required endpoints without a bound query", () => {
    const post = vi.fn();
    const client = { post } as unknown as EshuApiClient;
    render(
      <MemoryRouter initialEntries={["/cloud-drift"]}>
        <CloudDriftPage client={client} />
      </MemoryRouter>
    );

    expect(screen.getByText("Enter a scope or account to load drift evidence.")).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });
});
