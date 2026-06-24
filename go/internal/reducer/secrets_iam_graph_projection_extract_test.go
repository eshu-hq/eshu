// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func exactChainFact(payload map[string]any) facts.Envelope {
	payload["state"] = string(SecretsIAMTrustChainStateExact)
	return facts.Envelope{FactKind: secretsIAMIdentityTrustChainFactKind, Payload: payload}
}

func exactPathFact(payload map[string]any) facts.Envelope {
	payload["state"] = string(SecretsIAMTrustChainStateExact)
	return facts.Envelope{FactKind: secretsIAMSecretAccessPathFactKind, Payload: payload}
}

func fullExactChainPayload() map[string]any {
	return map[string]any{
		"scope_id": "scope-1", "generation_id": "gen-1", "confidence": "exact",
		"service_account_join_key": "sha256:sa", "workload_object_id": "workload-1",
		"iam_role_fingerprint": "sha256:role", "vault_role_join_key": "sha256:vrole",
		"vault_mount_join_key": "sha256:mount", "vault_policy_join_keys": []string{"sha256:pol1", "sha256:pol2"},
		"evidence_fact_ids": []string{"f1", "f2"},
	}
}

func TestExtractExactChainProducesResolvableSubgraph(t *testing.T) {
	t.Parallel()

	rows := ExtractSecretsIAMGraphRows([]facts.Envelope{exactChainFact(fullExactChainPayload())})

	if len(rows.ServiceAccountNodes) != 1 || rows.ServiceAccountNodes[0]["uid"] != "sha256:sa" {
		t.Fatalf("service account nodes = %+v", rows.ServiceAccountNodes)
	}
	if len(rows.VaultAuthRoleNodes) != 1 || rows.VaultAuthRoleNodes[0]["uid"] != "sha256:vrole" {
		t.Fatalf("vault auth role nodes = %+v", rows.VaultAuthRoleNodes)
	}
	if len(rows.VaultPolicyNodes) != 2 {
		t.Fatalf("vault policy nodes = %d, want 2", len(rows.VaultPolicyNodes))
	}
	if len(rows.UsesServiceAccountEdges) != 1 || rows.UsesServiceAccountEdges[0]["workload_uid"] != "workload-1" {
		t.Fatalf("uses-service-account edges = %+v", rows.UsesServiceAccountEdges)
	}
	if len(rows.AuthenticatesVaultRoleEdges) != 1 || len(rows.UsesVaultPolicyEdges) != 2 {
		t.Fatalf("vault edges = auth:%d policy:%d", len(rows.AuthenticatesVaultRoleEdges), len(rows.UsesVaultPolicyEdges))
	}
	// ASSUMES_IAM_ROLE is counted as a skip, never an edge (ADR §5.1).
	if rows.Tally.EdgesByType[secretsIAMRelAssumesIAMRole] != 0 {
		t.Fatal("ASSUMES_IAM_ROLE must not be emitted as an edge")
	}
	if rows.Tally.SkippedByReason[secretsIAMSkipIAMRoleUnresolved] != 1 {
		t.Fatalf("iam-role skip = %d, want 1", rows.Tally.SkippedByReason[secretsIAMSkipIAMRoleUnresolved])
	}
	if rows.Tally.NodesByLabel[secretsIAMLabelServiceAccount] != 1 || rows.Tally.EdgesByType[secretsIAMRelUsesVaultPolicy] != 2 {
		t.Fatalf("tally = %+v", rows.Tally)
	}
}

func TestExtractEmitsAssumesIAMRoleWhenCloudResourceUIDResolves(t *testing.T) {
	t.Parallel()

	// When the read-model row carries a CloudResource-joinable IAM-role identity
	// (iam_role_cloud_resource_uid), the ASSUMES_IAM_ROLE edge promotes from the
	// ServiceAccount node to the existing IAM-role CloudResource node. The raw ARN
	// never appears; only the precomputed redaction-safe uid is the endpoint.
	p := fullExactChainPayload()
	p["iam_role_cloud_resource_uid"] = "cr-uid-iam-role"
	p["iam_role_assume_mode"] = "web_identity"
	rows := ExtractSecretsIAMGraphRows([]facts.Envelope{exactChainFact(p)})

	if rows.Tally.EdgesByType[secretsIAMRelAssumesIAMRole] != 1 {
		t.Fatalf("ASSUMES_IAM_ROLE edge = %d, want 1 (tally=%+v)", rows.Tally.EdgesByType[secretsIAMRelAssumesIAMRole], rows.Tally)
	}
	if rows.Tally.SkippedByReason[secretsIAMSkipIAMRoleUnresolved] != 0 {
		t.Fatalf("resolvable endpoint must not be counted as a skip: %+v", rows.Tally)
	}
	if len(rows.AssumesIAMRoleEdges) != 1 {
		t.Fatalf("assumes-iam-role edges = %+v", rows.AssumesIAMRoleEdges)
	}
	edge := rows.AssumesIAMRoleEdges[0]
	if edge["service_account_uid"] != "sha256:sa" || edge["cloud_resource_uid"] != "cr-uid-iam-role" {
		t.Fatalf("edge endpoints = %+v", edge)
	}
	if edge["assume_mode"] != "web_identity" {
		t.Fatalf("assume_mode = %v, want web_identity", edge["assume_mode"])
	}
}

func TestExtractSkipsAssumesIAMRoleWhenOnlyFingerprintPresent(t *testing.T) {
	t.Parallel()

	// Today's behavior: a chain with only the one-way iam_role_fingerprint (no
	// CloudResource-joinable uid) cannot resolve the IAM-role endpoint. It is
	// counted, never fabricated.
	rows := ExtractSecretsIAMGraphRows([]facts.Envelope{exactChainFact(fullExactChainPayload())})

	if rows.Tally.EdgesByType[secretsIAMRelAssumesIAMRole] != 0 {
		t.Fatal("ASSUMES_IAM_ROLE must not be emitted without a joinable uid")
	}
	if rows.Tally.SkippedByReason[secretsIAMSkipIAMRoleUnresolved] != 1 {
		t.Fatalf("iam-role skip = %d, want 1", rows.Tally.SkippedByReason[secretsIAMSkipIAMRoleUnresolved])
	}
	if len(rows.AssumesIAMRoleEdges) != 0 {
		t.Fatalf("no edge expected: %+v", rows.AssumesIAMRoleEdges)
	}
}

func TestExtractAssumesIAMRoleEdgeIsDedupedAndDeterministic(t *testing.T) {
	t.Parallel()

	p := fullExactChainPayload()
	p["iam_role_cloud_resource_uid"] = "cr-uid-iam-role"
	fact := exactChainFact(p)
	rows := ExtractSecretsIAMGraphRows([]facts.Envelope{fact, fact})

	if len(rows.AssumesIAMRoleEdges) != 1 || rows.Tally.EdgesByType[secretsIAMRelAssumesIAMRole] != 1 {
		t.Fatalf("duplicate delivery not deduped: edges=%d tally=%d", len(rows.AssumesIAMRoleEdges), rows.Tally.EdgesByType[secretsIAMRelAssumesIAMRole])
	}
	// assume_mode defaults to the bounded "web_identity" when unspecified is not
	// asserted here; an absent mode simply carries an empty bounded value.
}

func TestExtractExactSecretAccessPath(t *testing.T) {
	t.Parallel()

	rows := ExtractSecretsIAMGraphRows([]facts.Envelope{exactPathFact(map[string]any{
		"scope_id": "scope-1", "generation_id": "gen-1", "confidence": "exact",
		"vault_policy_join_key": "sha256:pol1", "vault_mount_join_key": "sha256:mount",
		"kv_path_fingerprint": "sha256:kv", "capabilities": []string{"read", "list"},
		"evidence_fact_ids": []string{"f3"},
	})})

	if len(rows.VaultPolicyNodes) != 1 || len(rows.SecretMetadataPathNodes) != 1 {
		t.Fatalf("nodes: policy=%d path=%d", len(rows.VaultPolicyNodes), len(rows.SecretMetadataPathNodes))
	}
	if len(rows.GrantsSecretReadEdges) != 1 {
		t.Fatalf("grants edges = %d, want 1", len(rows.GrantsSecretReadEdges))
	}
	caps, _ := rows.GrantsSecretReadEdges[0]["capabilities"].([]string)
	if len(caps) != 2 {
		t.Fatalf("capabilities = %v", rows.GrantsSecretReadEdges[0]["capabilities"])
	}
	// The SecretMetadataPath uid must be a stable composite, not a raw path.
	if uid, _ := rows.SecretMetadataPathNodes[0]["uid"].(string); uid == "" || uid == "sha256:kv" {
		t.Fatalf("secret path uid = %q, want a stable composite", uid)
	}
}

func TestExtractSkipsNonExactStates(t *testing.T) {
	t.Parallel()

	for _, state := range []string{"partial", "unresolved", "stale", "permission_hidden", "unsupported"} {
		state := state
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			p := fullExactChainPayload()
			p["state"] = state
			rows := ExtractSecretsIAMGraphRows([]facts.Envelope{
				{FactKind: secretsIAMIdentityTrustChainFactKind, Payload: p},
			})
			if len(rows.ServiceAccountNodes) != 0 || rows.Tally.SkippedByReason[secretsIAMSkipNonExactState] != 1 {
				t.Fatalf("state %q produced graph rows or wrong skip: nodes=%d tally=%+v", state, len(rows.ServiceAccountNodes), rows.Tally)
			}
		})
	}
}

func TestExtractSkipsSecretAccessPathWithBlankMountJoinKey(t *testing.T) {
	t.Parallel()

	// A blank vault_mount_join_key would collapse the SecretMetadataPath uid to
	// kv_path_fingerprint alone, colliding unrelated paths across mounts/clusters
	// into one node and GRANTS_SECRET_READ edge. Such a row is missing secret-path
	// identity and must be skipped+counted, never projected.
	rows := ExtractSecretsIAMGraphRows([]facts.Envelope{exactPathFact(map[string]any{
		"scope_id": "scope-1", "generation_id": "gen-1", "confidence": "exact",
		"vault_policy_join_key": "sha256:pol1", "vault_mount_join_key": "",
		"kv_path_fingerprint": "sha256:kv", "capabilities": []string{"read"},
	})})

	if len(rows.SecretMetadataPathNodes) != 0 || len(rows.GrantsSecretReadEdges) != 0 {
		t.Fatalf("blank mount join key projected rows: nodes=%d edges=%d", len(rows.SecretMetadataPathNodes), len(rows.GrantsSecretReadEdges))
	}
	if rows.Tally.SkippedByReason[secretsIAMSkipMissingSecretPath] != 1 {
		t.Fatalf("missing-secret-path skip = %d, want 1 (tally=%+v)", rows.Tally.SkippedByReason[secretsIAMSkipMissingSecretPath], rows.Tally)
	}
}

func TestExtractMissingWorkloadStillProjectsServiceAccount(t *testing.T) {
	t.Parallel()

	p := fullExactChainPayload()
	delete(p, "workload_object_id")
	rows := ExtractSecretsIAMGraphRows([]facts.Envelope{exactChainFact(p)})

	if len(rows.ServiceAccountNodes) != 1 {
		t.Fatalf("service account node missing without workload: %+v", rows.ServiceAccountNodes)
	}
	if len(rows.UsesServiceAccountEdges) != 0 {
		t.Fatal("workload edge must be skipped when workload_object_id is absent")
	}
	if rows.Tally.SkippedByReason[secretsIAMSkipMissingWorkload] != 1 {
		t.Fatalf("missing-workload skip = %d, want 1", rows.Tally.SkippedByReason[secretsIAMSkipMissingWorkload])
	}
	// The Vault subgraph still projects.
	if len(rows.AuthenticatesVaultRoleEdges) != 1 {
		t.Fatal("vault subgraph should still project without the workload edge")
	}
}

func TestExtractDeduplicatesAndIsDeterministic(t *testing.T) {
	t.Parallel()

	fact := exactChainFact(fullExactChainPayload())
	a := ExtractSecretsIAMGraphRows([]facts.Envelope{fact, fact, fact})
	b := ExtractSecretsIAMGraphRows([]facts.Envelope{fact})

	if len(a.ServiceAccountNodes) != 1 || len(a.UsesVaultPolicyEdges) != 2 {
		t.Fatalf("duplicate delivery not deduped: sa=%d pol=%d", len(a.ServiceAccountNodes), len(a.UsesVaultPolicyEdges))
	}
	// Deterministic identity across duplicate vs single delivery.
	if a.UsesVaultPolicyEdges[0]["vault_policy_uid"] != b.UsesVaultPolicyEdges[0]["vault_policy_uid"] {
		t.Fatal("non-deterministic edge ordering")
	}
}

func TestExtractHandlesJSONDecodedSliceFields(t *testing.T) {
	t.Parallel()

	// In production the read-model facts are JSON-decoded, so list fields arrive
	// as []any, not []string. Lock that path.
	rows := ExtractSecretsIAMGraphRows([]facts.Envelope{exactChainFact(map[string]any{
		"scope_id": "scope-1", "generation_id": "gen-1", "confidence": "exact",
		"service_account_join_key": "sha256:sa", "vault_role_join_key": "sha256:vrole",
		"vault_policy_join_keys": []any{"sha256:pol1", "sha256:pol2"},
		"evidence_fact_ids":      []any{"f1", "f2"},
	})})

	if len(rows.VaultPolicyNodes) != 2 || len(rows.UsesVaultPolicyEdges) != 2 {
		t.Fatalf("[]any slice fields not handled: policies=%d edges=%d", len(rows.VaultPolicyNodes), len(rows.UsesVaultPolicyEdges))
	}
}

func TestExtractEmptyInput(t *testing.T) {
	t.Parallel()

	rows := ExtractSecretsIAMGraphRows(nil)
	if rows.ServiceAccountNodes != nil || len(rows.Tally.NodesByLabel) != 0 || len(rows.Tally.SkippedByReason) != 0 {
		t.Fatalf("empty input produced rows: %+v", rows)
	}
}

func TestExtractTombstoneAndForeignKindsIgnored(t *testing.T) {
	t.Parallel()

	rows := ExtractSecretsIAMGraphRows([]facts.Envelope{
		{FactKind: secretsIAMIdentityTrustChainFactKind, IsTombstone: true, Payload: fullExactChainPayload()},
		{FactKind: "reducer_secrets_iam_posture_gap", Payload: map[string]any{"state": "exact"}},
		{FactKind: "some_other_fact", Payload: map[string]any{}},
	})
	if len(rows.ServiceAccountNodes) != 0 || len(rows.Tally.SkippedByReason) != 0 {
		t.Fatalf("tombstone/foreign kinds were processed: %+v", rows)
	}
}

func TestExtractRowsCarryNoForbiddenProperties(t *testing.T) {
	t.Parallel()

	// Per ADR §7, only join keys / fingerprints / bounded enums / scope metadata
	// may appear. Assert the row property keys are within the allowlist.
	allowed := map[string]bool{
		"uid": true, "scope_id": true, "generation_id": true, "evidence_source": true,
		"confidence": true, "vault_mount_join_key": true, "kv_path_fingerprint": true,
		"workload_uid": true, "service_account_uid": true, "vault_auth_role_uid": true,
		"vault_policy_uid": true, "secret_path_uid": true, "evidence_fact_ids": true,
		"capabilities": true, "assume_mode": true, "cloud_resource_uid": true,
	}
	chainPayload := fullExactChainPayload()
	chainPayload["iam_role_cloud_resource_uid"] = "sha256:cr-uid"
	chainPayload["iam_role_assume_mode"] = "web_identity"
	rows := ExtractSecretsIAMGraphRows([]facts.Envelope{
		exactChainFact(chainPayload),
		exactPathFact(map[string]any{
			"scope_id": "scope-1", "generation_id": "gen-1", "confidence": "exact",
			"vault_policy_join_key": "sha256:pol1", "vault_mount_join_key": "sha256:mount",
			"kv_path_fingerprint": "sha256:kv", "capabilities": []string{"read"},
		}),
	})
	all := [][]map[string]any{
		rows.ServiceAccountNodes, rows.VaultAuthRoleNodes, rows.VaultPolicyNodes, rows.SecretMetadataPathNodes,
		rows.UsesServiceAccountEdges, rows.AssumesIAMRoleEdges, rows.AuthenticatesVaultRoleEdges, rows.UsesVaultPolicyEdges, rows.GrantsSecretReadEdges,
	}
	for _, set := range all {
		for _, row := range set {
			for key, val := range row {
				if !allowed[key] {
					t.Fatalf("row carries forbidden property %q = %v", key, val)
				}
				// Identity/key values must not look like a raw ARN or path.
				// evidence_source is the fixed reducer tag (an allowlisted
				// constant, not derived from source data), so it is exempt.
				if key == "evidence_source" {
					continue
				}
				if s, ok := val.(string); ok && (strings.HasPrefix(s, "arn:") || strings.Contains(s, "/")) {
					t.Fatalf("property %q = %q looks like raw (ARN/path) material", key, s)
				}
			}
		}
	}
}
