import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";

import { SecretsIamPage } from "./SecretsIamPage";
import type { EshuApiClient } from "../api/client";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";

describe("SecretsIamPage", () => {
  it("loads a deep-linked posture review with truth, buckets, and drilldowns", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
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
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/secrets-iam?scope_id=scope-prod&state=partial&limit=25"]}>
        <SecretsIamPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
      </MemoryRouter>
    );

    expect(await screen.findByRole("heading", { name: "Secrets/IAM posture" })).toBeInTheDocument();
    await waitFor(() => expect(calls).toHaveLength(5));
    expect(calls).toContain("/api/v0/secrets-iam/posture-summary?scope_id=scope-prod");
    expect(calls).toContain("/api/v0/secrets-iam/identity-trust-chains?scope_id=scope-prod&limit=25&state=partial");
    expect(screen.getByText("read model")).toBeInTheDocument();
    expect(screen.getByText("Graph projection gated")).toBeInTheDocument();
    expect(screen.getByText("secrets_iam.identity_trust_chains.list")).toBeInTheDocument();
    expect(screen.getAllByText("exact").length).toBeGreaterThan(0);
    expect(screen.getAllByText("chain-1").length).toBeGreaterThan(0);
    expect(screen.getAllByText("service-account:join-1").length).toBeGreaterThan(0);
    expect(screen.getByText("broad role grant requires owner review")).toBeInTheDocument();
    expect(screen.getByText("kv:fingerprint-1")).toBeInTheDocument();
    expect(screen.getByText(/Next chain cursor chain-2\./)).toBeInTheDocument();
    expect(screen.getByText("external trust evidence missing")).toBeInTheDocument();
  });

  it("does not call the API until a scope is provided", async () => {
    const client = { get: vi.fn() } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/secrets-iam"]}>
        <SecretsIamPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
      </MemoryRouter>
    );

    expect(await screen.findByRole("heading", { name: "Secrets/IAM posture" })).toBeInTheDocument();
    expect(client.get).not.toHaveBeenCalled();
    expect(screen.getByText("Add scope_id to load reducer-owned secrets/IAM posture.")).toBeInTheDocument();
  });

  it("submits bounded filters into a deep-linkable URL", async () => {
    const client = {
      get: vi.fn(async (path: string) => {
        if (path.includes("/posture-summary")) return { data: summaryPayload(), error: null, truth: truthEnvelope("secrets_iam.posture_summary.read") };
        if (path.includes("/identity-trust-chains")) return { data: trustChainsPayload(), error: null, truth: truthEnvelope("secrets_iam.identity_trust_chains.list") };
        if (path.includes("/privilege-posture-observations")) return { data: privilegePayload(), error: null, truth: truthEnvelope("secrets_iam.privilege_posture_observations.list") };
        if (path.includes("/secret-access-paths")) return { data: accessPathsPayload(), error: null, truth: truthEnvelope("secrets_iam.secret_access_paths.list") };
        return { data: gapsPayload(), error: null, truth: truthEnvelope("secrets_iam.posture_gaps.list") };
      })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/secrets-iam"]}>
        <Routes>
          <Route
            element={(
              <>
                <SecretsIamPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
                <LocationProbe />
              </>
            )}
            path="/secrets-iam"
          />
        </Routes>
      </MemoryRouter>
    );

    await screen.findByRole("heading", { name: "Secrets/IAM posture" });
    fireEvent.change(screen.getByLabelText("Scope id"), { target: { value: "scope-prod" } });
    fireEvent.change(screen.getByLabelText("State"), { target: { value: "permission_hidden" } });
    fireEvent.change(screen.getByLabelText("Limit"), { target: { value: "50" } });
    fireEvent.click(screen.getByRole("button", { name: "Load posture" }));

    expect(await screen.findByTestId("secrets-iam-location")).toHaveTextContent(
      "/secrets-iam?scope_id=scope-prod&state=permission_hidden&limit=50"
    );
    await waitFor(() => expect(client.get).toHaveBeenCalledTimes(5));
  });

  it("renders an unavailable section instead of a fabricated empty state", async () => {
    const client = {
      get: vi.fn(async (path: string) => {
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
        if (path.includes("/posture-summary")) return { data: summaryPayload(), error: null, truth: truthEnvelope("secrets_iam.posture_summary.read") };
        if (path.includes("/privilege-posture-observations")) return { data: privilegePayload(), error: null, truth: truthEnvelope("secrets_iam.privilege_posture_observations.list") };
        if (path.includes("/secret-access-paths")) return { data: accessPathsPayload(), error: null, truth: truthEnvelope("secrets_iam.secret_access_paths.list") };
        return { data: gapsPayload(), error: null, truth: truthEnvelope("secrets_iam.posture_gaps.list") };
      })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/secrets-iam?scope_id=scope-prod"]}>
        <SecretsIamPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
      </MemoryRouter>
    );

    expect(await screen.findByText("unsupported_capability: identity trust chains require local-authoritative profile")).toBeInTheDocument();
    expect(screen.queryByText("No secrets/IAM posture data loaded.")).not.toBeInTheDocument();
    expect(screen.getByText("broad role grant requires owner review")).toBeInTheDocument();
  });
});

function LocationProbe(): React.JSX.Element {
  const location = useLocation();
  return <output data-testid="secrets-iam-location">{location.pathname + location.search}</output>;
}

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
    count: 1,
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
    limit: 25,
    next_cursor: { after_chain_id: "chain-2" },
    truncated: true
  };
}

function privilegePayload(): Record<string, unknown> {
  return {
    count: 1,
    limit: 25,
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
    limit: 25,
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
    limit: 25,
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
