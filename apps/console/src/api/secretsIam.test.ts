import { EshuApiClient, type EshuFetcher } from "./client";
import { loadSecretsIamPosture } from "./secretsIam";

describe("secrets IAM adapter", () => {
  it("loads bounded posture sections for one reducer scope", async () => {
    const get = vi.fn(async (path: string) => {
      if (path.includes("/posture-summary")) {
        return { data: summaryPayload(), error: null, truth: truthEnvelope("secrets_iam.posture_summary.read") };
      }
      if (path.includes("/identity-trust-chains")) {
        return { data: trustChainsPayload(), error: null, truth: truthEnvelope("secrets_iam.identity_trust_chains.list") };
      }
      if (path.includes("/privilege-posture-observations")) {
        return { data: privilegePayload(), error: null, truth: truthEnvelope("secrets_iam.privilege_posture_observations.list") };
      }
      if (path.includes("/secret-access-paths")) {
        return { data: accessPathsPayload(), error: null, truth: truthEnvelope("secrets_iam.secret_access_paths.list") };
      }
      return { data: gapsPayload(), error: null, truth: truthEnvelope("secrets_iam.posture_gaps.list") };
    });
    const client = { get } as unknown as EshuApiClient;

    const review = await loadSecretsIamPosture(client, {
      limit: 500,
      scopeId: "scope-prod",
      state: "partial"
    });

    expect(get.mock.calls.map(([path]) => path)).toEqual([
      "/api/v0/secrets-iam/posture-summary?scope_id=scope-prod",
      "/api/v0/secrets-iam/identity-trust-chains?scope_id=scope-prod&limit=200&state=partial",
      "/api/v0/secrets-iam/privilege-posture-observations?scope_id=scope-prod&limit=200&state=partial",
      "/api/v0/secrets-iam/secret-access-paths?scope_id=scope-prod&limit=200&state=partial",
      "/api/v0/secrets-iam/posture-gaps?scope_id=scope-prod&limit=200&state=partial"
    ]);
    expect(review.input.limit).toBe(200);
    expect(review.summary.status).toBe("ready");
    expect(review.trustChains.status).toBe("ready");
    expect(review.trustChains.status === "ready" ? review.trustChains.data.nextCursor?.afterChainId : "").toBe("chain-2");
    expect(review.secretAccessPaths.status === "ready" ? review.secretAccessPaths.data.paths[0]?.capabilities : []).toEqual(["read", "list"]);
  });

  it("skips requests until a scope anchor is supplied", async () => {
    const client = { get: vi.fn() } as unknown as EshuApiClient;

    const review = await loadSecretsIamPosture(client, {});

    expect(client.get).not.toHaveBeenCalled();
    expect(review.summary.status).toBe("skipped");
    expect(review.trustChains.status).toBe("skipped");
    expect(review.summary.status === "skipped" ? review.summary.reason : "").toContain("scope_id");
  });

  it("keeps successful sections visible when one read model is unavailable", async () => {
    const get = vi.fn(async (path: string) => {
      if (path.includes("/identity-trust-chains")) {
        return {
          data: null,
          error: {
            code: "unsupported_capability",
            message: "identity trust chains require local-authoritative profile"
          },
          truth: null
        };
      }
      if (path.includes("/posture-summary")) {
        return { data: summaryPayload(), error: null, truth: truthEnvelope("secrets_iam.posture_summary.read") };
      }
      if (path.includes("/privilege-posture-observations")) {
        return { data: privilegePayload(), error: null, truth: truthEnvelope("secrets_iam.privilege_posture_observations.list") };
      }
      if (path.includes("/secret-access-paths")) {
        return { data: accessPathsPayload(), error: null, truth: truthEnvelope("secrets_iam.secret_access_paths.list") };
      }
      return { data: gapsPayload(), error: null, truth: truthEnvelope("secrets_iam.posture_gaps.list") };
    });
    const client = { get } as unknown as EshuApiClient;

    const review = await loadSecretsIamPosture(client, { scopeId: "scope-prod" });

    expect(review.summary.status).toBe("ready");
    expect(review.trustChains.status).toBe("unavailable");
    expect(review.trustChains.status === "unavailable" ? review.trustChains.error : "").toContain("unsupported_capability");
    expect(review.postureGaps.status).toBe("ready");
  });

  it("preserves structured non-2xx contract errors from the real API client", async () => {
    const fetcher: EshuFetcher = async (input: RequestInfo | URL): Promise<Response> => {
      const path = new URL(new Request(input).url).pathname;
      if (path.includes("/identity-trust-chains")) {
        return Response.json({
          data: null,
          error: {
            code: "unsupported_capability",
            message: "identity trust chains require local-authoritative profile"
          },
          truth: null
        }, { status: 501 });
      }
      if (path.includes("/posture-summary")) {
        return Response.json({ data: summaryPayload(), error: null, truth: truthEnvelope("secrets_iam.posture_summary.read") });
      }
      if (path.includes("/privilege-posture-observations")) {
        return Response.json({ data: privilegePayload(), error: null, truth: truthEnvelope("secrets_iam.privilege_posture_observations.list") });
      }
      if (path.includes("/secret-access-paths")) {
        return Response.json({ data: accessPathsPayload(), error: null, truth: truthEnvelope("secrets_iam.secret_access_paths.list") });
      }
      return Response.json({ data: gapsPayload(), error: null, truth: truthEnvelope("secrets_iam.posture_gaps.list") });
    };
    const client = new EshuApiClient({ baseUrl: "/eshu-api/", fetcher });

    const review = await loadSecretsIamPosture(client, { scopeId: "scope-prod" });

    expect(review.summary.status).toBe("ready");
    expect(review.trustChains.status).toBe("unavailable");
    expect(review.trustChains.status === "unavailable" ? review.trustChains.error : "").toContain("unsupported_capability");
    expect(review.trustChains.status === "unavailable" ? review.trustChains.error : "").toContain("local-authoritative");
  });
});

function truthEnvelope(capability: string) {
  return {
    basis: "semantic_facts",
    capability,
    freshness: { state: "fresh" },
    level: "exact",
    profile: "local_authoritative"
  };
}

function summaryPayload(): Record<string, unknown> {
  return {
    scope_id: "scope-prod",
    summary: {
      identity_trust_chains_by_state: [{ bucket: "exact", count: 3 }, { bucket: "partial", count: 1 }],
      posture_gaps_by_gap_type: [{ bucket: "missing_evidence", count: 2 }],
      privilege_observations_by_risk_type: [{ bucket: "broad_role", count: 2 }],
      privilege_observations_by_severity: [{ bucket: "high", count: 1 }],
      secret_access_paths_by_state: [{ bucket: "exact", count: 2 }]
    }
  };
}

function trustChainsPayload(): Record<string, unknown> {
  return {
    count: 2,
    identity_trust_chains: [{
      chain_id: "chain-1",
      confidence: "high",
      iam_role_fingerprint: "iam-role:fingerprint-1",
      missing_evidence: ["external_id"],
      service_account_join_key: "service-account:join-1",
      source_generations: ["generation:41"],
      source_scopes: ["scope-prod"],
      state: "partial",
      vault_mount_join_key: "vault-mount:join-1",
      vault_policy_join_keys: ["vault-policy:join-1"],
      workload_kind: "kubernetes",
      workload_object_id: "workload:payments-api"
    }],
    limit: 200,
    next_cursor: { after_chain_id: "chain-2" },
    truncated: true
  };
}

function privilegePayload(): Record<string, unknown> {
  return {
    count: 1,
    limit: 200,
    privilege_posture_observations: [{
      confidence: "medium",
      evidence_fact_ids: ["fact:privilege-1"],
      observation_id: "observation-1",
      reason: "broad role grant requires owner review",
      risk_type: "broad_role",
      severity: "high",
      state: "partial",
      subject_fingerprint: "subject:fingerprint-1"
    }],
    truncated: false
  };
}

function accessPathsPayload(): Record<string, unknown> {
  return {
    count: 1,
    limit: 200,
    secret_access_paths: [{
      capabilities: ["read", "list"],
      chain_id: "chain-1",
      confidence: "high",
      evidence_fact_ids: ["fact:path-1"],
      kv_path_fingerprint: "kv:fingerprint-1",
      path_id: "path-1",
      state: "exact",
      vault_mount_join_key: "vault-mount:join-1",
      vault_policy_join_key: "vault-policy:join-1"
    }],
    truncated: false
  };
}

function gapsPayload(): Record<string, unknown> {
  return {
    count: 1,
    limit: 200,
    posture_gaps: [{
      evidence_fact_ids: ["fact:gap-1"],
      gap_id: "gap-1",
      gap_type: "missing_evidence",
      missing_evidence: ["external_id"],
      reason: "external trust evidence missing",
      service_account_join_key: "service-account:join-1",
      state: "unresolved",
      unsupported_layers: []
    }],
    truncated: false
  };
}
