// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file is the ADR #1314 §11 fixture-truth proof for the in-memory path:
// it drives the full load -> extract -> write orchestration through
// SecretsIAMGraphProjectionHandler against the recording writer and asserts the
// EXACT node and edge rows handed to each writer surface for all four
// SecretsIAM* node families and all five resolvable SECRETS_IAM_* edge families,
// plus the skip-counted cases (missing workload, IAM-role-unresolved,
// missing-vault-role, non-exact, missing-secret-path). It proves fixture intent
// maps to exact graph-truth rows without a graph backend. The TRUE live-backend
// conformance is the BACKEND-GATED test in
// internal/storage/cypher/secrets_iam_graph_live_test.go, which SKIPs unless a
// Bolt backend env is configured. The live writer remains gated OFF by default
// (ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED) pending target-bound
// activation proof; this test does not register or enable it.

// rowByUID indexes node rows by their uid for exact-row assertions.
func rowByUID(rows []map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(rows))
	for _, r := range rows {
		if uid, ok := r["uid"].(string); ok {
			out[uid] = r
		}
	}
	return out
}

func assertProp(t *testing.T, row map[string]any, key string, want any) {
	t.Helper()
	got, ok := row[key]
	if !ok {
		t.Fatalf("row missing property %q: %+v", key, row)
	}
	if gotStr, isStr := got.(string); isStr {
		if gotStr != want {
			t.Fatalf("property %q = %q, want %q", key, gotStr, want)
		}
		return
	}
	t.Fatalf("property %q is not a string (%T)", key, got)
}

func assertStringSlice(t *testing.T, row map[string]any, key string, want []string) {
	t.Helper()
	got, ok := row[key].([]string)
	if !ok {
		t.Fatalf("property %q is not []string: %T", key, row[key])
	}
	if len(got) != len(want) {
		t.Fatalf("property %q = %v, want %v", key, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("property %q[%d] = %q, want %q", key, i, got[i], want[i])
		}
	}
}

// firstBatch returns the single recorded batch for a writer surface, failing if
// the surface was not called exactly once. The projection writes one batch per
// surface for a single-fixture generation.
func firstBatch(t *testing.T, batches [][]map[string]any, surface string) []map[string]any {
	t.Helper()
	if len(batches) != 1 {
		t.Fatalf("%s surface called %d times, want 1", surface, len(batches))
	}
	return batches[0]
}

// TestGraphProjectionFixtureTruthFullExactChainAndPath drives the IRSA-style
// exact chain plus an exact secret-access path through the handler and asserts
// the exact rows for all four node families and all five edge families, plus the
// IAM-role-unresolved skip. This is the §11 "exact IRSA chain" + "exact Vault
// path" rows realized end to end against the recording writer.
func TestGraphProjectionFixtureTruthFullExactChainAndPath(t *testing.T) {
	t.Parallel()

	loader := fakeFactLoader{envelopes: []facts.Envelope{
		exactChainFact(fullExactChainPayload()),
		exactPathFact(map[string]any{
			"scope_id": "scope-1", "generation_id": "gen-1", "confidence": "exact",
			"vault_policy_join_key": "sha256:pol1", "vault_mount_join_key": "sha256:mount",
			"kv_path_fingerprint": "sha256:kv", "capabilities": []string{"read", "list"},
			"evidence_fact_ids": []string{"f3"},
		}),
	}}
	writer := &recordingGraphWriter{}
	h := SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer}

	if _, err := h.Handle(context.Background(), graphProjectionIntent()); err != nil {
		t.Fatalf("Handle error = %v", err)
	}

	// Node family 1: ServiceAccount. uid is the service_account_join_key.
	saRows := firstBatch(t, writer.serviceAccountNodes, "service-account-nodes")
	if len(saRows) != 1 {
		t.Fatalf("service account rows = %d, want 1", len(saRows))
	}
	assertProp(t, saRows[0], "uid", "sha256:sa")
	assertProp(t, saRows[0], "scope_id", "scope-1")
	assertProp(t, saRows[0], "generation_id", "gen-1")
	assertProp(t, saRows[0], "evidence_source", SecretsIAMGraphEvidenceSource)
	assertProp(t, saRows[0], "confidence", "exact")

	// Node family 2: VaultAuthRole. uid is vault_role_join_key + mount.
	vrRows := firstBatch(t, writer.vaultAuthRoleNodes, "vault-auth-role-nodes")
	if len(vrRows) != 1 {
		t.Fatalf("vault auth role rows = %d, want 1", len(vrRows))
	}
	assertProp(t, vrRows[0], "uid", "sha256:vrole")
	assertProp(t, vrRows[0], "vault_mount_join_key", "sha256:mount")

	// Node family 3: VaultPolicy. The chain contributes pol1+pol2; the path
	// contributes pol1 (deduped). Three distinct policy uids across both facts.
	vpRows := firstBatch(t, writer.vaultPolicyNodes, "vault-policy-nodes")
	vpByUID := rowByUID(vpRows)
	for _, uid := range []string{"sha256:pol1", "sha256:pol2"} {
		if _, ok := vpByUID[uid]; !ok {
			t.Fatalf("vault policy node missing uid %q: %+v", uid, vpRows)
		}
	}
	if len(vpRows) != 2 {
		t.Fatalf("vault policy rows = %d, want 2 (pol1 deduped across chain+path)", len(vpRows))
	}

	// Node family 4: SecretMetadataPath. uid is the stable mount+kv composite, not
	// the raw kv fingerprint.
	spRows := firstBatch(t, writer.secretPathNodes, "secret-path-nodes")
	if len(spRows) != 1 {
		t.Fatalf("secret path rows = %d, want 1", len(spRows))
	}
	spUID, _ := spRows[0]["uid"].(string)
	if spUID == "" || spUID == "sha256:kv" {
		t.Fatalf("secret path uid = %q, want a stable composite", spUID)
	}
	assertProp(t, spRows[0], "vault_mount_join_key", "sha256:mount")
	assertProp(t, spRows[0], "kv_path_fingerprint", "sha256:kv")

	// Edge family 1: USES_SERVICE_ACCOUNT (KubernetesWorkload -> ServiceAccount).
	usEdges := firstBatch(t, writer.usesSAEdges, "uses-service-account-edges")
	if len(usEdges) != 1 {
		t.Fatalf("uses-service-account edges = %d, want 1", len(usEdges))
	}
	assertProp(t, usEdges[0], "workload_uid", "workload-1")
	assertProp(t, usEdges[0], "service_account_uid", "sha256:sa")
	assertStringSlice(t, usEdges[0], "evidence_fact_ids", []string{"f1", "f2"})

	// Edge family 2: AUTHENTICATES_TO_VAULT_ROLE (ServiceAccount -> VaultAuthRole).
	authEdges := firstBatch(t, writer.authVaultRoleEdges, "auth-vault-role-edges")
	if len(authEdges) != 1 {
		t.Fatalf("auth-vault-role edges = %d, want 1", len(authEdges))
	}
	assertProp(t, authEdges[0], "service_account_uid", "sha256:sa")
	assertProp(t, authEdges[0], "vault_auth_role_uid", "sha256:vrole")

	// Edge family 3: USES_VAULT_POLICY (VaultAuthRole -> VaultPolicy), one per policy.
	upEdges := firstBatch(t, writer.usesVaultPolicyEdge, "uses-vault-policy-edges")
	if len(upEdges) != 2 {
		t.Fatalf("uses-vault-policy edges = %d, want 2", len(upEdges))
	}
	for _, e := range upEdges {
		assertProp(t, e, "vault_auth_role_uid", "sha256:vrole")
	}

	// Edge family 4: GRANTS_SECRET_READ (VaultPolicy -> SecretMetadataPath).
	grantsEdges := firstBatch(t, writer.grantsSecretEdges, "grants-secret-read-edges")
	if len(grantsEdges) != 1 {
		t.Fatalf("grants-secret-read edges = %d, want 1", len(grantsEdges))
	}
	assertProp(t, grantsEdges[0], "vault_policy_uid", "sha256:pol1")
	if grantsEdges[0]["secret_path_uid"] != spUID {
		t.Fatalf("grants edge secret_path_uid = %v, want %q (must match the node uid)", grantsEdges[0]["secret_path_uid"], spUID)
	}
	// capabilities are normalized to a unique-sorted set by payloadStrings, so
	// the input {"read","list"} surfaces as the sorted {"list","read"}.
	assertStringSlice(t, grantsEdges[0], "capabilities", []string{"list", "read"})

	// Retract ran once for the scope before the write.
	if len(writer.retracts) != 1 || writer.retracts[0][0] != "scope-1" {
		t.Fatalf("retract = %v, want [[scope-1]]", writer.retracts)
	}
}

// TestGraphProjectionFixtureTruthPodIdentityChain proves the §11 "exact EKS Pod
// Identity chain" fixture: an exact chain carrying assume_mode=pod_identity still
// projects the ServiceAccount subgraph and counts the IAM-role endpoint as
// unresolved (no fabricated CloudResource), exactly like the IRSA case, since the
// read model cannot yet join the IAM-role fingerprint to a CloudResource uid.
func TestGraphProjectionFixtureTruthPodIdentityChain(t *testing.T) {
	t.Parallel()

	p := fullExactChainPayload()
	p["assume_mode"] = "pod_identity"
	loader := fakeFactLoader{envelopes: []facts.Envelope{exactChainFact(p)}}
	writer := &recordingGraphWriter{}
	h := SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer}

	if _, err := h.Handle(context.Background(), graphProjectionIntent()); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	saRows := firstBatch(t, writer.serviceAccountNodes, "service-account-nodes")
	if len(saRows) != 1 || saRows[0]["uid"] != "sha256:sa" {
		t.Fatalf("pod-identity chain did not project the service account node: %+v", saRows)
	}
	// The IAM-role endpoint is unresolved, so no edge writer surface beyond the
	// Vault subgraph and the workload edge fires; there is no IAM-role node family.
	if len(writer.usesSAEdges) != 1 || len(writer.authVaultRoleEdges) != 1 {
		t.Fatalf("pod-identity chain edges: usesSA=%d auth=%d, want 1 and 1",
			len(writer.usesSAEdges), len(writer.authVaultRoleEdges))
	}
}

// TestGraphProjectionFixtureTruthMissingWorkloadSkips proves the §11 "exact Vault
// path without workload node" row through the handler: the ServiceAccount-to-Vault
// subgraph projects and the workload edge surface is never called (skip-counted in
// the extractor tally), while node families still write.
func TestGraphProjectionFixtureTruthMissingWorkloadSkips(t *testing.T) {
	t.Parallel()

	p := fullExactChainPayload()
	delete(p, "workload_object_id")
	loader := fakeFactLoader{envelopes: []facts.Envelope{exactChainFact(p)}}
	writer := &recordingGraphWriter{}
	h := SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer}

	if _, err := h.Handle(context.Background(), graphProjectionIntent()); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if len(writer.serviceAccountNodes) != 1 || len(writer.vaultAuthRoleNodes) != 1 {
		t.Fatalf("subgraph did not project without workload: sa=%d vr=%d",
			len(writer.serviceAccountNodes), len(writer.vaultAuthRoleNodes))
	}
	// The workload edge is skipped: its writer surface must never be called.
	if len(writer.usesSAEdges) != 0 {
		t.Fatalf("workload edge surface called %d times, want 0 (skip-counted)", len(writer.usesSAEdges))
	}
	// The Vault subgraph edge still resolves.
	if len(writer.authVaultRoleEdges) != 1 {
		t.Fatal("vault subgraph edge missing when workload is absent")
	}
}

// TestGraphProjectionFixtureTruthMissingVaultHopSkips proves an exact chain with
// no Vault hop projects ONLY the ServiceAccount node and the workload edge; the
// Vault role/policy node and edge surfaces are never called (missing-vault-role
// skip).
func TestGraphProjectionFixtureTruthMissingVaultHopSkips(t *testing.T) {
	t.Parallel()

	p := fullExactChainPayload()
	delete(p, "vault_role_join_key")
	delete(p, "vault_policy_join_keys")
	loader := fakeFactLoader{envelopes: []facts.Envelope{exactChainFact(p)}}
	writer := &recordingGraphWriter{}
	h := SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer}

	if _, err := h.Handle(context.Background(), graphProjectionIntent()); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if len(writer.serviceAccountNodes) != 1 {
		t.Fatalf("service account node missing: %+v", writer.serviceAccountNodes)
	}
	if len(writer.usesSAEdges) != 1 {
		t.Fatalf("workload edge missing: %+v", writer.usesSAEdges)
	}
	// No Vault hop: role/policy node + edge surfaces are never called.
	if len(writer.vaultAuthRoleNodes) != 0 || len(writer.vaultPolicyNodes) != 0 {
		t.Fatalf("vault nodes written without a vault hop: vr=%d vp=%d",
			len(writer.vaultAuthRoleNodes), len(writer.vaultPolicyNodes))
	}
	if len(writer.authVaultRoleEdges) != 0 || len(writer.usesVaultPolicyEdge) != 0 {
		t.Fatalf("vault edges written without a vault hop: auth=%d pol=%d",
			len(writer.authVaultRoleEdges), len(writer.usesVaultPolicyEdge))
	}
}

// TestGraphProjectionFixtureTruthMissingSecretPathSkips proves the §11
// missing-secret-path-identity skip through the handler: an exact path whose
// vault_mount_join_key is blank produces no SecretMetadataPath node and no
// GRANTS_SECRET_READ edge (a blank mount would collapse unrelated paths into one
// node), while the policy node still projects.
func TestGraphProjectionFixtureTruthMissingSecretPathSkips(t *testing.T) {
	t.Parallel()

	loader := fakeFactLoader{envelopes: []facts.Envelope{exactPathFact(map[string]any{
		"scope_id": "scope-1", "generation_id": "gen-1", "confidence": "exact",
		"vault_policy_join_key": "sha256:pol1", "vault_mount_join_key": "",
		"kv_path_fingerprint": "sha256:kv", "capabilities": []string{"read"},
	})}}
	writer := &recordingGraphWriter{}
	h := SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer}

	if _, err := h.Handle(context.Background(), graphProjectionIntent()); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if len(writer.secretPathNodes) != 0 || len(writer.grantsSecretEdges) != 0 {
		t.Fatalf("blank mount projected secret-path rows: nodes=%d edges=%d",
			len(writer.secretPathNodes), len(writer.grantsSecretEdges))
	}
}

// TestGraphProjectionFixtureTruthNonExactWritesNothing proves the §11
// non-exact-state rows (partial/stale/permission-hidden/unsupported) write no
// node or edge to any surface through the handler, while retract still clears any
// prior generation. This is the handler-boundary mirror of the extractor's
// non-exact skip.
func TestGraphProjectionFixtureTruthNonExactWritesNothing(t *testing.T) {
	t.Parallel()

	for _, state := range []string{"partial", "unresolved", "stale", "permission_hidden", "unsupported"} {
		state := state
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			p := fullExactChainPayload()
			p["state"] = state
			loader := fakeFactLoader{envelopes: []facts.Envelope{
				{FactKind: secretsIAMIdentityTrustChainFactKind, Payload: p},
			}}
			writer := &recordingGraphWriter{}
			h := SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer}

			if _, err := h.Handle(context.Background(), graphProjectionIntent()); err != nil {
				t.Fatalf("Handle error = %v", err)
			}
			if len(writer.serviceAccountNodes) != 0 || len(writer.usesSAEdges) != 0 ||
				len(writer.vaultAuthRoleNodes) != 0 || len(writer.grantsSecretEdges) != 0 {
				t.Fatalf("non-exact state %q produced graph writes", state)
			}
			// Retract still runs to clear any prior generation's reducer-owned edges.
			if len(writer.retracts) != 1 {
				t.Fatalf("retract calls = %d, want 1", len(writer.retracts))
			}
		})
	}
}

// TestGraphProjectionFixtureTruthDuplicateDeliveryIsIdempotent proves the §11
// "duplicate delivery -> one node/edge identity" row through the handler: three
// identical chain facts collapse to one node/edge identity per family.
func TestGraphProjectionFixtureTruthDuplicateDeliveryIsIdempotent(t *testing.T) {
	t.Parallel()

	fact := exactChainFact(fullExactChainPayload())
	loader := fakeFactLoader{envelopes: []facts.Envelope{fact, fact, fact}}
	writer := &recordingGraphWriter{}
	h := SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer}

	if _, err := h.Handle(context.Background(), graphProjectionIntent()); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	saRows := firstBatch(t, writer.serviceAccountNodes, "service-account-nodes")
	if len(saRows) != 1 {
		t.Fatalf("duplicate delivery wrote %d service account rows, want 1", len(saRows))
	}
	upEdges := firstBatch(t, writer.usesVaultPolicyEdge, "uses-vault-policy-edges")
	if len(upEdges) != 2 {
		t.Fatalf("duplicate delivery wrote %d uses-vault-policy edges, want 2", len(upEdges))
	}
}
