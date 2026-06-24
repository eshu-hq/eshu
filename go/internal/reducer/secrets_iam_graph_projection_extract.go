// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

// SecretsIAMGraphEvidenceSource tags every reducer-owned Secrets/IAM graph node
// and edge so scoped retraction can remove only this projection's writes
// without touching CloudResource or KubernetesWorkload state (ADR #1314 §8).
const SecretsIAMGraphEvidenceSource = "reducer/secrets-iam-graph"

// Static relationship tokens (ADR #1314 §6). Closed vocabulary; no value is ever
// encoded into a relationship type.
const (
	secretsIAMRelUsesServiceAccount     = string(edgetype.SecretsIamUsesServiceAccount)
	secretsIAMRelAssumesIAMRole         = string(edgetype.SecretsIamAssumesIamRole)
	secretsIAMRelAuthenticatesVaultRole = string(edgetype.SecretsIamAuthenticatesToVaultRole)
	secretsIAMRelUsesVaultPolicy        = string(edgetype.SecretsIamUsesVaultPolicy)
	secretsIAMRelGrantsSecretRead       = string(edgetype.SecretsIamGrantsSecretRead)
)

// Static node labels (ADR #1314 §5).
const (
	secretsIAMLabelServiceAccount     = "SecretsIAMServiceAccount"
	secretsIAMLabelVaultAuthRole      = "SecretsIAMVaultAuthRole"
	secretsIAMLabelVaultPolicy        = "SecretsIAMVaultPolicy"
	secretsIAMLabelSecretMetadataPath = "SecretsIAMSecretMetadataPath"
)

// Skip reasons (bounded enum, ADR #1314 §13).
const (
	secretsIAMSkipNonExactState         = "non_exact_state"
	secretsIAMSkipMissingServiceAccount = "missing_service_account_join_key"
	// secretsIAMSkipMissingVaultRole counts an exact chain that has no Vault hop
	// (the ServiceAccount node still projects). It is an expected shape, not a
	// failure.
	secretsIAMSkipMissingVaultRole  = "missing_vault_role_join_key"
	secretsIAMSkipMissingWorkload   = "missing_workload_endpoint"
	secretsIAMSkipMissingSecretPath = "missing_secret_path_identity"
	// secretsIAMSkipIAMRoleUnresolved marks the ASSUMES_IAM_ROLE edge that
	// cannot resolve its CloudResource endpoint from the current read model. It is
	// counted, never fabricated, when the read-model row carries only the one-way
	// iam_role_fingerprint and no CloudResource-joinable IAM-role identity
	// (iam_role_cloud_resource_uid). When that joinable uid is present the edge
	// promotes instead (ADR #1314 §5.1).
	secretsIAMSkipIAMRoleUnresolved = "iam_role_endpoint_unresolved_pending_read_model"
)

// Bounded assume-mode enum for the ASSUMES_IAM_ROLE edge. The reducer derives it
// from the IAM-role evidence kind; it never encodes a role name, ARN, or
// account-specific value. An empty value is allowed when the read-model row does
// not classify the assume mode.
const (
	secretsIAMAssumeModeWebIdentity = "web_identity"
	secretsIAMAssumeModePodIdentity = "pod_identity"
)

// SecretsIAMGraphTally is the bounded-enum projection accounting used for
// telemetry and result evidence. All keys are static labels/types/reasons.
type SecretsIAMGraphTally struct {
	NodesByLabel    map[string]int
	EdgesByType     map[string]int
	SkippedByReason map[string]int
}

func newSecretsIAMGraphTally() SecretsIAMGraphTally {
	return SecretsIAMGraphTally{
		NodesByLabel:    map[string]int{},
		EdgesByType:     map[string]int{},
		SkippedByReason: map[string]int{},
	}
}

// SecretsIAMGraphRows is the deduped, sorted, redaction-safe set of node and
// edge rows promoted from exact reducer read-model facts. Every map holds only
// join keys, fingerprints, bounded enums, and scope/generation/evidence
// metadata — never a secret value, path, ARN, namespace, or policy name.
type SecretsIAMGraphRows struct {
	ServiceAccountNodes     []map[string]any
	VaultAuthRoleNodes      []map[string]any
	VaultPolicyNodes        []map[string]any
	SecretMetadataPathNodes []map[string]any

	UsesServiceAccountEdges     []map[string]any
	AssumesIAMRoleEdges         []map[string]any
	AuthenticatesVaultRoleEdges []map[string]any
	UsesVaultPolicyEdges        []map[string]any
	GrantsSecretReadEdges       []map[string]any

	Tally SecretsIAMGraphTally
}

// ExtractSecretsIAMGraphRows builds the graph projection rows from reducer
// read-model facts. Only `state=exact` identity_trust_chain and
// secret_access_path facts are admitted (ADR #1314 §4); every other state or
// missing endpoint is skipped and counted, never fabricated. The function is
// pure so the full positive/negative/duplicate/empty matrix can be proven
// without a graph backend.
func ExtractSecretsIAMGraphRows(envelopes []facts.Envelope) SecretsIAMGraphRows {
	b := newSecretsIAMGraphRowBuilder()
	for _, envelope := range envelopes {
		if envelope.IsTombstone {
			continue
		}
		switch envelope.FactKind {
		case secretsIAMIdentityTrustChainFactKind:
			b.addIdentityTrustChain(envelope)
		case secretsIAMSecretAccessPathFactKind:
			b.addSecretAccessPath(envelope)
		}
	}
	return b.finish()
}

type secretsIAMGraphRowBuilder struct {
	serviceAccounts map[string]map[string]any
	vaultAuthRoles  map[string]map[string]any
	vaultPolicies   map[string]map[string]any
	secretPaths     map[string]map[string]any

	usesServiceAccount     map[string]map[string]any
	assumesIAMRole         map[string]map[string]any
	authenticatesVaultRole map[string]map[string]any
	usesVaultPolicy        map[string]map[string]any
	grantsSecretRead       map[string]map[string]any

	tally SecretsIAMGraphTally
}

func newSecretsIAMGraphRowBuilder() *secretsIAMGraphRowBuilder {
	return &secretsIAMGraphRowBuilder{
		serviceAccounts:        map[string]map[string]any{},
		vaultAuthRoles:         map[string]map[string]any{},
		vaultPolicies:          map[string]map[string]any{},
		secretPaths:            map[string]map[string]any{},
		usesServiceAccount:     map[string]map[string]any{},
		assumesIAMRole:         map[string]map[string]any{},
		authenticatesVaultRole: map[string]map[string]any{},
		usesVaultPolicy:        map[string]map[string]any{},
		grantsSecretRead:       map[string]map[string]any{},
		tally:                  newSecretsIAMGraphTally(),
	}
}

func (b *secretsIAMGraphRowBuilder) addIdentityTrustChain(envelope facts.Envelope) {
	if payloadString(envelope.Payload, "state") != string(SecretsIAMTrustChainStateExact) {
		b.tally.SkippedByReason[secretsIAMSkipNonExactState]++
		return
	}
	saKey := payloadString(envelope.Payload, "service_account_join_key")
	if saKey == "" {
		b.tally.SkippedByReason[secretsIAMSkipMissingServiceAccount]++
		return
	}
	scopeID := payloadString(envelope.Payload, "scope_id")
	generationID := payloadString(envelope.Payload, "generation_id")
	confidence := payloadString(envelope.Payload, "confidence")
	evidence := payloadStrings(envelope.Payload, "", "evidence_fact_ids")

	b.putNode(b.serviceAccounts, secretsIAMLabelServiceAccount, map[string]any{
		"uid": saKey, "scope_id": scopeID, "generation_id": generationID,
		"evidence_source": SecretsIAMGraphEvidenceSource, "confidence": confidence,
	})

	// USES_SERVICE_ACCOUNT: KubernetesWorkload(uid=workload_object_id) -> ServiceAccount.
	// Optional: the workload node may not be materialized; the row is emitted and
	// the writer's MATCH skips a missing endpoint. A blank workload id is counted
	// as a skip and the ServiceAccount subgraph still projects (ADR §5).
	if workloadUID := payloadString(envelope.Payload, "workload_object_id"); workloadUID != "" {
		b.putEdge(b.usesServiceAccount, secretsIAMRelUsesServiceAccount, workloadUID, saKey, map[string]any{
			"workload_uid": workloadUID, "service_account_uid": saKey,
			"scope_id": scopeID, "generation_id": generationID,
			"evidence_source": SecretsIAMGraphEvidenceSource, "confidence": confidence,
			"evidence_fact_ids": evidence,
		})
	} else {
		b.tally.SkippedByReason[secretsIAMSkipMissingWorkload]++
	}

	// ASSUMES_IAM_ROLE: ServiceAccount -> existing IAM-role CloudResource node.
	// The edge promotes only when the read-model row carries a
	// CloudResource-joinable IAM-role identity (iam_role_cloud_resource_uid). When
	// the row carries only the one-way iam_role_fingerprint the endpoint is not
	// resolvable, so the edge is counted and never fabricated (ADR §5.1). A blank
	// cloud_resource_uid is endpoint-no-op-safe: the writer MATCH skips a missing
	// node, so a stale uid cannot fabricate a CloudResource.
	if cloudResourceUID := payloadString(envelope.Payload, "iam_role_cloud_resource_uid"); cloudResourceUID != "" {
		b.putEdge(b.assumesIAMRole, secretsIAMRelAssumesIAMRole, saKey, cloudResourceUID, map[string]any{
			"service_account_uid": saKey, "cloud_resource_uid": cloudResourceUID,
			"assume_mode": secretsIAMAssumeMode(payloadString(envelope.Payload, "iam_role_assume_mode")),
			"scope_id":    scopeID, "generation_id": generationID,
			"evidence_source": SecretsIAMGraphEvidenceSource, "confidence": confidence,
			"evidence_fact_ids": evidence,
		})
	} else if payloadString(envelope.Payload, "iam_role_fingerprint") != "" {
		b.tally.SkippedByReason[secretsIAMSkipIAMRoleUnresolved]++
	}

	vaultRoleKey := payloadString(envelope.Payload, "vault_role_join_key")
	if vaultRoleKey == "" {
		// No Vault hop in this chain; the ServiceAccount node still stands.
		b.tally.SkippedByReason[secretsIAMSkipMissingVaultRole]++
		return
	}
	b.putNode(b.vaultAuthRoles, secretsIAMLabelVaultAuthRole, map[string]any{
		"uid": vaultRoleKey, "vault_mount_join_key": payloadString(envelope.Payload, "vault_mount_join_key"),
		"scope_id": scopeID, "generation_id": generationID,
		"evidence_source": SecretsIAMGraphEvidenceSource, "confidence": confidence,
	})
	b.putEdge(b.authenticatesVaultRole, secretsIAMRelAuthenticatesVaultRole, saKey, vaultRoleKey, map[string]any{
		"service_account_uid": saKey, "vault_auth_role_uid": vaultRoleKey,
		"scope_id": scopeID, "generation_id": generationID,
		"evidence_source": SecretsIAMGraphEvidenceSource, "confidence": confidence,
		"evidence_fact_ids": evidence,
	})

	for _, policyKey := range payloadStrings(envelope.Payload, "", "vault_policy_join_keys") {
		b.putNode(b.vaultPolicies, secretsIAMLabelVaultPolicy, map[string]any{
			"uid": policyKey, "scope_id": scopeID, "generation_id": generationID,
			"evidence_source": SecretsIAMGraphEvidenceSource, "confidence": confidence,
		})
		b.putEdge(b.usesVaultPolicy, secretsIAMRelUsesVaultPolicy, vaultRoleKey, policyKey, map[string]any{
			"vault_auth_role_uid": vaultRoleKey, "vault_policy_uid": policyKey,
			"scope_id": scopeID, "generation_id": generationID,
			"evidence_source": SecretsIAMGraphEvidenceSource, "confidence": confidence,
			"evidence_fact_ids": evidence,
		})
	}
}

func (b *secretsIAMGraphRowBuilder) addSecretAccessPath(envelope facts.Envelope) {
	if payloadString(envelope.Payload, "state") != string(SecretsIAMTrustChainStateExact) {
		b.tally.SkippedByReason[secretsIAMSkipNonExactState]++
		return
	}
	policyKey := payloadString(envelope.Payload, "vault_policy_join_key")
	mountKey := payloadString(envelope.Payload, "vault_mount_join_key")
	kvFingerprint := payloadString(envelope.Payload, "kv_path_fingerprint")
	// A blank mount join key would collapse the SecretMetadataPath uid to
	// kv_path_fingerprint alone, colliding unrelated paths across mounts/clusters
	// into one node. Treat it as missing secret-path identity: skip and count.
	if policyKey == "" || mountKey == "" || kvFingerprint == "" {
		b.tally.SkippedByReason[secretsIAMSkipMissingSecretPath]++
		return
	}
	scopeID := payloadString(envelope.Payload, "scope_id")
	generationID := payloadString(envelope.Payload, "generation_id")
	confidence := payloadString(envelope.Payload, "confidence")
	evidence := payloadStrings(envelope.Payload, "", "evidence_fact_ids")

	pathUID := facts.StableID(secretsIAMLabelSecretMetadataPath, map[string]any{
		"vault_mount_join_key": mountKey,
		"kv_path_fingerprint":  kvFingerprint,
	})

	b.putNode(b.vaultPolicies, secretsIAMLabelVaultPolicy, map[string]any{
		"uid": policyKey, "scope_id": scopeID, "generation_id": generationID,
		"evidence_source": SecretsIAMGraphEvidenceSource, "confidence": confidence,
	})
	b.putNode(b.secretPaths, secretsIAMLabelSecretMetadataPath, map[string]any{
		"uid": pathUID, "vault_mount_join_key": mountKey, "kv_path_fingerprint": kvFingerprint,
		"scope_id": scopeID, "generation_id": generationID,
		"evidence_source": SecretsIAMGraphEvidenceSource, "confidence": confidence,
	})
	b.putEdge(b.grantsSecretRead, secretsIAMRelGrantsSecretRead, policyKey, pathUID, map[string]any{
		"vault_policy_uid": policyKey, "secret_path_uid": pathUID,
		"capabilities": payloadStrings(envelope.Payload, "", "capabilities"),
		"scope_id":     scopeID, "generation_id": generationID,
		"evidence_source": SecretsIAMGraphEvidenceSource, "confidence": confidence,
		"evidence_fact_ids": evidence,
	})
}

// secretsIAMAssumeMode maps the read-model assume-mode value onto the bounded
// edge enum. An unrecognized or empty value yields "" so the edge carries no
// out-of-vocabulary mode rather than leaking an unexpected source string.
func secretsIAMAssumeMode(mode string) string {
	switch mode {
	case secretsIAMAssumeModeWebIdentity:
		return secretsIAMAssumeModeWebIdentity
	case secretsIAMAssumeModePodIdentity:
		return secretsIAMAssumeModePodIdentity
	default:
		return ""
	}
}

// putNode dedupes a node by uid and counts the first insertion per label.
func (b *secretsIAMGraphRowBuilder) putNode(set map[string]map[string]any, label string, row map[string]any) {
	uid, _ := row["uid"].(string)
	if uid == "" {
		return
	}
	if _, exists := set[uid]; exists {
		return
	}
	set[uid] = row
	b.tally.NodesByLabel[label]++
}

// putEdge dedupes an edge by (source_uid, type, target_uid) and counts the first
// insertion per relationship type (ADR §9).
func (b *secretsIAMGraphRowBuilder) putEdge(set map[string]map[string]any, relType, sourceUID, targetUID string, row map[string]any) {
	key := sourceUID + "\x00" + relType + "\x00" + targetUID
	if _, exists := set[key]; exists {
		return
	}
	set[key] = row
	b.tally.EdgesByType[relType]++
}

func (b *secretsIAMGraphRowBuilder) finish() SecretsIAMGraphRows {
	return SecretsIAMGraphRows{
		ServiceAccountNodes:     sortedRowsByUID(b.serviceAccounts),
		VaultAuthRoleNodes:      sortedRowsByUID(b.vaultAuthRoles),
		VaultPolicyNodes:        sortedRowsByUID(b.vaultPolicies),
		SecretMetadataPathNodes: sortedRowsByUID(b.secretPaths),

		UsesServiceAccountEdges:     sortedEdgeRows(b.usesServiceAccount),
		AssumesIAMRoleEdges:         sortedEdgeRows(b.assumesIAMRole),
		AuthenticatesVaultRoleEdges: sortedEdgeRows(b.authenticatesVaultRole),
		UsesVaultPolicyEdges:        sortedEdgeRows(b.usesVaultPolicy),
		GrantsSecretReadEdges:       sortedEdgeRows(b.grantsSecretRead),

		Tally: b.tally,
	}
}

func sortedRowsByUID(set map[string]map[string]any) []map[string]any {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, set[k])
	}
	return out
}

func sortedEdgeRows(set map[string]map[string]any) []map[string]any {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, set[k])
	}
	return out
}
