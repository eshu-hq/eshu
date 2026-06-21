import type { EshuApiClient } from "./client";
import { loadReplatformingReview } from "./replatforming";

describe("replatforming adapter", () => {
  it("posts bounded scope bodies to rollups, plan, and ownership endpoints", async () => {
    const post = vi.fn(async (path: string) => ({
      data: path.endsWith("/rollups")
        ? rollupsPayload()
        : path.endsWith("/plans")
          ? planPayload()
          : ownershipPayload(),
      error: null,
      truth: truthEnvelope(path.endsWith("/plans") ? "replatforming.plan.readiness" : "replatforming.rollups.readiness")
    }));
    const client = { post } as unknown as EshuApiClient;

    const review = await loadReplatformingReview(client, {
      accountId: "123456789012",
      findingKinds: ["unmanaged_cloud_resource", "orphaned_cloud_resource"],
      limit: 900,
      offset: 25,
      region: "us-east-1",
      scopeKind: "account"
    });

    expect(post).toHaveBeenCalledWith("/api/v0/replatforming/rollups", {
      account_id: "123456789012",
      finding_kinds: ["unmanaged_cloud_resource", "orphaned_cloud_resource"],
      limit: 500,
      offset: 25,
      region: "us-east-1"
    });
    expect(post).toHaveBeenCalledWith("/api/v0/replatforming/plans", {
      account_id: "123456789012",
      finding_kinds: ["unmanaged_cloud_resource", "orphaned_cloud_resource"],
      limit: 500,
      offset: 25,
      region: "us-east-1",
      scope_kind: "account"
    });
    expect(post).toHaveBeenCalledWith("/api/v0/replatforming/ownership-packets", {
      account_id: "123456789012",
      finding_kinds: ["unmanaged_cloud_resource", "orphaned_cloud_resource"],
      limit: 500,
      offset: 25,
      region: "us-east-1"
    });
    expect(review.rollups.status).toBe("ready");
    expect(review.plan.status).toBe("ready");
    expect(review.ownership.status).toBe("ready");
    expect(review.input.limit).toBe(500);
  });

  it("skips every API call until an account or scope anchor is supplied", async () => {
    const client = { post: vi.fn() } as unknown as EshuApiClient;

    const review = await loadReplatformingReview(client, { scopeKind: "account" });

    expect(client.post).not.toHaveBeenCalled();
    expect(review.rollups.status).toBe("skipped");
    expect(review.plan.status).toBe("skipped");
    expect(review.ownership.status).toBe("skipped");
    expect(review.rollups.status === "skipped" ? review.rollups.reason : "").toContain("account_id or scope_id");
  });

  it("posts account-scoped bodies with region narrowing to all three endpoints", async () => {
    const post = vi.fn(async (path: string) => ({
      data: path.endsWith("/rollups")
        ? rollupsPayload()
        : path.endsWith("/plans")
          ? planPayload()
          : ownershipPayload(),
      error: null,
      truth: truthEnvelope("replatforming.rollups.readiness")
    }));
    const client = { post } as unknown as EshuApiClient;

    const review = await loadReplatformingReview(client, {
      accountId: "123456789012",
      region: "us-east-1",
      scopeKind: "account"
    });

    expect(post).toHaveBeenCalledWith("/api/v0/replatforming/rollups", expect.objectContaining({
      account_id: "123456789012",
      region: "us-east-1"
    }));
    expect(post).toHaveBeenCalledWith("/api/v0/replatforming/plans", expect.objectContaining({
      account_id: "123456789012",
      region: "us-east-1",
      scope_kind: "account"
    }));
    expect(post).toHaveBeenCalledWith("/api/v0/replatforming/ownership-packets", expect.objectContaining({
      account_id: "123456789012",
      region: "us-east-1"
    }));
    expect(review.rollups.status).toBe("ready");
    expect(review.plan.status).toBe("ready");
    expect(review.ownership.status).toBe("ready");
  });

  it("skips every API call when region scope kind is supplied without an account or scope anchor", async () => {
    const client = { post: vi.fn() } as unknown as EshuApiClient;

    const review = await loadReplatformingReview(client, {
      region: "us-east-1",
      scopeKind: "region"
    });

    expect(client.post).not.toHaveBeenCalled();
    expect(review.rollups.status).toBe("skipped");
    expect(review.plan.status).toBe("skipped");
    expect(review.ownership.status).toBe("skipped");
    const reason = review.rollups.status === "skipped" ? review.rollups.reason : "";
    expect(reason).toContain("account_id or scope_id");
    expect(reason).toContain("region");
  });

  it("keeps successful sections visible when ownership packets are unavailable", async () => {
    const post = vi.fn(async (path: string) => {
      if (path.endsWith("/ownership-packets")) {
        return {
          data: null,
          error: {
            code: "unsupported_capability",
            message: "ownership packets require local-authoritative profile"
          },
          truth: null
        };
      }
      return {
        data: path.endsWith("/rollups") ? rollupsPayload() : planPayload(),
        error: null,
        truth: truthEnvelope(path.endsWith("/rollups") ? "replatforming.rollups.readiness" : "replatforming.plan.readiness")
      };
    });
    const client = { post } as unknown as EshuApiClient;

    const review = await loadReplatformingReview(client, { accountId: "123456789012", scopeKind: "account" });

    expect(review.rollups.status).toBe("ready");
    expect(review.plan.status).toBe("ready");
    expect(review.ownership.status).toBe("unavailable");
    expect(review.ownership.status === "unavailable" ? review.ownership.error : "").toContain("unsupported_capability");
  });
});

function truthEnvelope(capability: string) {
  return {
    basis: "semantic_facts",
    capability,
    freshness: { state: "fresh" },
    level: "derived",
    profile: "local_authoritative"
  };
}

function rollupsPayload(): Record<string, unknown> {
  return {
    account_id: "123456789012",
    dimensions: {
      account: [{
        key: "123456789012",
        readiness: { import_ready: 1, needs_review: 2, refused: 1 },
        source_state_counts: { derived: 3, rejected: 1 },
        total: 4
      }],
      environment: [],
      service: []
    },
    limit: 500,
    next_offset: null,
    offset: 25,
    readiness_totals: { import_ready: 1, needs_review: 2, refused: 1 },
    rollup_findings_count: 4,
    source_state_totals: { derived: 3, rejected: 1 },
    story: "4 active AWS replatforming findings matched account scope.",
    total_findings_count: 4,
    truncated: false
  };
}

function planPayload(): Record<string, unknown> {
  return {
    blast_radius_summaries: [{ group_id: "low", item_count: 1, severity: "low" }],
    items_count: 2,
    limit: 500,
    next_offset: null,
    offset: 25,
    plan: {
      blast_radius_groups: [],
      contract_version: "v1",
      items: [{
        blast_radius_group: "low",
        confidence: "derived",
        finding_kind: "orphaned_cloud_resource",
        import_candidate: {
          import_block: "import { to = aws_lambda_function.payments id = \"arn:aws:lambda:us-east-1:123456789012:function:payments-api\" }",
          resource_type: "aws_lambda_function",
          status: "ready"
        },
        item_id: "fact:ready-lambda",
        management_status: "cloud_only",
        owner_candidates: [{ confidence: "derived", kind: "service", value: "payments-api" }],
        provider: "aws",
        resource_type: "lambda",
        safety_gate: { outcome: "allowed", review_required: false },
        source_state: "derived",
        stable_id: "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
        wave_id: "wave-1-early-safe"
      }, {
        blast_radius_group: "blocked",
        import_candidate: {
          refusal_reasons: ["security_review_required"],
          status: "refused"
        },
        item_id: "fact:blocked",
        management_status: "cloud_only",
        provider: "aws",
        resource_type: "iam",
        safety_gate: { outcome: "security_review_required", review_required: true },
        source_state: "rejected",
        stable_id: "arn:aws:iam::123456789012:role/app",
        wave_id: "wave-3-blocked"
      }],
      non_goals: [
        "does not run Terraform or any migration",
        "does not import resources or mutate cloud state",
        "does not write user repositories"
      ],
      scope: { account: "123456789012", kind: "account" },
      waves: [{ id: "wave-1-early-safe", item_ids: ["fact:ready-lambda"], order: 1, rationale: "ready candidates first" }]
    },
    ready_import_count: 1,
    refused_import_count: 1,
    scope_kind: "account",
    story: "2 migration packet items composed for account scope.",
    total_findings_count: 2,
    truncated: false,
    wave_summaries: [{ item_count: 1, order: 1, wave_id: "wave-1-early-safe" }]
  };
}

function ownershipPayload(): Record<string, unknown> {
  return {
    ambiguous_count: 1,
    limit: 500,
    next_offset: null,
    offset: 25,
    ownership_packets: [{
      freshness: { state: "fresh" },
      item_id: "fact:ready-lambda",
      missing_evidence: ["terraform_state_address"],
      owner_candidates: [{ confidence: "derived", kind: "service", value: "payments-api" }],
      provider: "aws",
      safety_gate: { outcome: "allowed", review_required: false },
      source_state: "derived",
      stable_id: "arn:aws:lambda:us-east-1:123456789012:function:payments-api"
    }],
    packets_count: 1,
    rejected_count: 0,
    story: "1 ownership packet composed.",
    total_findings_count: 1,
    truncated: false,
    unattributed_count: 0
  };
}
