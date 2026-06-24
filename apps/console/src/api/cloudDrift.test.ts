import { describe, expect, it, vi } from "vitest";

import type { EshuApiClient } from "./client";
import {
  loadAwsRuntimeDriftFindings,
  loadCloudRuntimeDriftFindings,
  loadIaCManagementExplanation,
  loadTerraformImportPlanCandidates,
  loadUnmanagedCloudResources
} from "./cloudDrift";

function envelope(data: unknown) {
  return {
    data,
    error: null,
    truth: {
      capability: "cloud_runtime_drift.readback.list",
      freshness: { state: "fresh" },
      level: "exact",
      profile: "production"
    }
  };
}

describe("cloud drift adapters", () => {
  it("posts the provider-neutral runtime drift request and maps truth, paging, and safety posture", async () => {
    const capture: { path?: string; body?: unknown } = {};
    const client = {
      post: vi.fn(async (path: string, body: unknown) => {
        capture.path = path;
        capture.body = body;
        return envelope({
          analysis_status: "materialized_multi_cloud_runtime_drift",
          drift_findings: [{
            fact_id: "fact:drift:1",
            provider: "aws",
            scope_id: "aws:123456789012:us-east-1:lambda",
            generation_id: "gen-7",
            cloud_resource_uid: "cloud-resource:lambda:payments",
            finding_kind: "unmanaged_cloud_resource",
            management_status: "cloud_only",
            confidence: 0.94,
            source_state: "derived",
            missing_evidence: ["terraform_state_resource"],
            recommended_action: "triage_owner_and_import_or_retire",
            safety_gate: {
              outcome: "read_only_allowed",
              read_only: true,
              review_required: false,
              refused_actions: []
            }
          }],
          findings_count: 1,
          limit: 25,
          next_offset: 25,
          offset: 0,
          provider: "aws",
          story: "1 active multi-cloud runtime drift findings matched aws resources.",
          total_findings_count: 51,
          truncated: true
        });
      })
    } as unknown as EshuApiClient;

    const page = await loadCloudRuntimeDriftFindings(client, {
      accountId: "123456789012",
      limit: 25,
      offset: 0,
      provider: "aws",
      region: "us-east-1",
      scopeId: "aws:123456789012:us-east-1:lambda"
    });

    expect(capture.path).toBe("/api/v0/cloud/runtime-drift/findings");
    expect(capture.body).toEqual({
      account_id: "123456789012",
      finding_kinds: undefined,
      limit: 25,
      offset: 0,
      provider: "aws",
      scope_id: "aws:123456789012:us-east-1:lambda"
    });
    expect(page.findings[0]).toMatchObject({
      id: "fact:drift:1",
      canonicalResourceId: "cloud-resource:lambda:payments",
      managementStatus: "cloud_only",
      safetyOutcome: "read_only_allowed"
    });
    expect(page.nextOffset).toBe(25);
    expect(page.truth.freshness).toBe("fresh");
  });

  it("loads AWS drift, unmanaged resources, import candidates, and exact explanation without fabricating rows", async () => {
    const calls: Array<[string, unknown]> = [];
    const post = vi.fn(async (path: string, body: unknown) => {
      calls.push([path, body]);
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
            confidence: 0.9,
            safety_gate: { outcome: "read_only_allowed", review_required: false }
          }],
          findings_count: 1,
          total_findings_count: 1,
          limit: 50,
          offset: 0,
          truncated: false
        });
      }
      if (path === "/api/v0/iac/unmanaged-resources") {
        return envelope({
          findings: [{
            id: "finding:aws:1",
            arn: "arn:aws:s3:::payments-prod",
            provider: "aws",
            account_id: "123456789012",
            region: "us-east-1",
            resource_type: "s3",
            resource_id: "payments-prod",
            finding_kind: "orphaned_cloud_resource",
            management_status: "cloud_only",
            recommended_action: "triage_owner_and_import_or_retire",
            confidence: 0.9,
            missing_evidence: ["terraform_state_resource"],
            warning_flags: ["raw_tags_provenance_only"],
            safety_gate: { outcome: "read_only_allowed", review_required: false }
          }],
          findings_count: 1,
          total_findings_count: 1,
          limit: 50,
          offset: 0,
          truncated: false
        });
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
          ready_count: 1,
          refused_count: 0,
          total_findings_count: 1,
          limit: 50,
          offset: 0,
          truncated: false
        });
      }
      if (path === "/api/v0/iac/management-status/explain") {
        return envelope({
          arn: "arn:aws:s3:::payments-prod",
          finding: {
            id: "finding:aws:1",
            arn: "arn:aws:s3:::payments-prod",
            management_status: "cloud_only",
            finding_kind: "orphaned_cloud_resource"
          },
          evidence_groups: [{
            layer: "cloud",
            count: 1,
            evidence: [{ id: "ev-1", evidence_type: "aws_cloud_resource", key: "arn", value: "arn:aws:s3:::payments-prod" }]
          }],
          safety_gate: { outcome: "read_only_allowed", review_required: false },
          story: "arn:aws:s3:::payments-prod is classified as cloud_only."
        });
      }
      throw new Error(`unexpected path ${path}`);
    });
    const client = { post } as unknown as EshuApiClient;
    const query = { accountId: "123456789012", limit: 50, offset: 0, region: "us-east-1" };

    const aws = await loadAwsRuntimeDriftFindings(client, query);
    const unmanaged = await loadUnmanagedCloudResources(client, query);
    const candidates = await loadTerraformImportPlanCandidates(client, query);
    const explanation = await loadIaCManagementExplanation(client, {
      accountId: "123456789012",
      arn: "arn:aws:s3:::payments-prod",
      region: "us-east-1"
    });

    expect(aws.findings[0].promotionOutcome).toBe("not_promoted");
    expect(unmanaged.findings[0].managementStatus).toBe("cloud_only");
    expect(candidates.candidates[0]).toMatchObject({
      findingId: "finding:aws:1",
      status: "ready",
      suggestedResourceAddress: "aws_s3_bucket.payments_prod"
    });
    expect(explanation.evidenceGroups[0].layer).toBe("cloud");
    expect(calls.map(([path]) => path)).toEqual([
      "/api/v0/aws/runtime-drift/findings",
      "/api/v0/iac/unmanaged-resources",
      "/api/v0/iac/terraform-import-plan/candidates",
      "/api/v0/iac/management-status/explain"
    ]);
  });

  it("propagates Eshu error envelopes instead of returning empty drift pages", async () => {
    const client = {
      post: vi.fn(async () => ({
        data: null,
        error: {
          capability: "cloud_runtime_drift.readback.list",
          code: "unsupported_capability",
          message: "cloud runtime drift readback requires reducer materialization"
        },
        truth: null
      }))
    } as unknown as EshuApiClient;

    await expect(loadCloudRuntimeDriftFindings(client, {
      accountId: "123456789012",
      limit: 50,
      offset: 0,
      provider: "aws"
    })).rejects.toThrow("unsupported_capability");
  });

  it("rejects malformed success envelopes instead of fabricating empty fresh pages", async () => {
    const client = {
      post: vi.fn(async () => ({
        data: null,
        error: null,
        truth: null
      }))
    } as unknown as EshuApiClient;

    await expect(loadCloudRuntimeDriftFindings(client, {
      accountId: "123456789012",
      limit: 50,
      offset: 0,
      provider: "aws"
    })).rejects.toThrow("missing data or truth");
  });
});
