// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseSecretsIAMGraph names the secrets/IAM graph-projection phase for
// grouped-backend statement metadata and diagnostics (issue #1347, ADR #1314).
const canonicalPhaseSecretsIAMGraph = "secrets_iam_graph"

// Static node labels and relationship types (ADR #1314 §5/§6). Every label and
// relationship token below is a fixed constant baked into the const templates,
// so this writer interpolates NO data-driven token into the Cypher: identity is
// always the uid (nodes) or the two endpoint uids plus the static relationship
// token (edges), and every mutable value lives in a SET property.
const (
	secretsIAMNodeServiceAccount     = "SecretsIAMServiceAccount"
	secretsIAMNodeVaultAuthRole      = "SecretsIAMVaultAuthRole"
	secretsIAMNodeVaultPolicy        = "SecretsIAMVaultPolicy"
	secretsIAMNodeSecretMetadataPath = "SecretsIAMSecretMetadataPath"
)

// Node upsert templates. MERGE keys on uid only; mutable properties are SET
// after the identity MERGE so duplicate delivery converges on one node.
const (
	// #nosec G101 -- Cypher template; const name contains "Secret"/"Account" but the value is a parameterized graph-write query, not a credential literal
	secretsIAMServiceAccountNodeUpsertCypher = `UNWIND $rows AS row
MERGE (n:SecretsIAMServiceAccount {uid: row.uid})
SET n.scope_id = row.scope_id,
    n.generation_id = row.generation_id,
    n.evidence_source = row.evidence_source,
    n.confidence = row.confidence`

	// #nosec G101 -- Cypher template; const name contains "Secret"/"Vault" but the value is a parameterized graph-write query, not a credential literal
	secretsIAMVaultAuthRoleNodeUpsertCypher = `UNWIND $rows AS row
MERGE (n:SecretsIAMVaultAuthRole {uid: row.uid})
SET n.vault_mount_join_key = row.vault_mount_join_key,
    n.scope_id = row.scope_id,
    n.generation_id = row.generation_id,
    n.evidence_source = row.evidence_source,
    n.confidence = row.confidence`

	// #nosec G101 -- Cypher template; const name contains "Secret"/"Policy" but the value is a parameterized graph-write query, not a credential literal
	secretsIAMVaultPolicyNodeUpsertCypher = `UNWIND $rows AS row
MERGE (n:SecretsIAMVaultPolicy {uid: row.uid})
SET n.scope_id = row.scope_id,
    n.generation_id = row.generation_id,
    n.evidence_source = row.evidence_source,
    n.confidence = row.confidence`

	// #nosec G101 -- Cypher template; const name contains "Secret"/"Metadata" but the value is a parameterized graph-write query, not a credential literal
	secretsIAMSecretMetadataPathNodeUpsertCypher = `UNWIND $rows AS row
MERGE (n:SecretsIAMSecretMetadataPath {uid: row.uid})
SET n.vault_mount_join_key = row.vault_mount_join_key,
    n.kv_path_fingerprint = row.kv_path_fingerprint,
    n.scope_id = row.scope_id,
    n.generation_id = row.generation_id,
    n.evidence_source = row.evidence_source,
    n.confidence = row.confidence`
)

// Edge upsert templates. Two MATCHes precede the MERGE so a row whose endpoint
// node is absent (an unmaterialized KubernetesWorkload, for example) produces no
// edge and no fabricated node. Both anchors are uid-indexed lookups (no scan).
// MERGE keys on the two endpoint uids plus the static relationship token only.
const (
	// #nosec G101 -- Cypher template; const name contains "Secret"/"Account" but the value is a parameterized graph-write query, not a credential literal
	secretsIAMUsesServiceAccountEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (w:KubernetesWorkload {uid: row.workload_uid})
MATCH (s:SecretsIAMServiceAccount {uid: row.service_account_uid})
MERGE (w)-[rel:SECRETS_IAM_USES_SERVICE_ACCOUNT]->(s)
SET rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source,
    rel.confidence = row.confidence,
    rel.evidence_fact_ids = row.evidence_fact_ids`

	// #nosec G101 -- Cypher template; const name contains "Secret"/"IAMRole" but the value is a parameterized graph-write query, not a credential literal
	secretsIAMAssumesIAMRoleEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (s:SecretsIAMServiceAccount {uid: row.service_account_uid})
MATCH (c:CloudResource {uid: row.cloud_resource_uid})
MERGE (s)-[rel:SECRETS_IAM_ASSUMES_IAM_ROLE]->(c)
SET rel.assume_mode = row.assume_mode,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source,
    rel.confidence = row.confidence,
    rel.evidence_fact_ids = row.evidence_fact_ids`

	// #nosec G101 -- Cypher template; const name contains "Secret"/"Vault" but the value is a parameterized graph-write query, not a credential literal
	secretsIAMAuthenticatesVaultRoleEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (s:SecretsIAMServiceAccount {uid: row.service_account_uid})
MATCH (v:SecretsIAMVaultAuthRole {uid: row.vault_auth_role_uid})
MERGE (s)-[rel:SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE]->(v)
SET rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source,
    rel.confidence = row.confidence,
    rel.evidence_fact_ids = row.evidence_fact_ids`

	// #nosec G101 -- Cypher template; const name contains "Secret"/"Policy" but the value is a parameterized graph-write query, not a credential literal
	secretsIAMUsesVaultPolicyEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (v:SecretsIAMVaultAuthRole {uid: row.vault_auth_role_uid})
MATCH (p:SecretsIAMVaultPolicy {uid: row.vault_policy_uid})
MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)
SET rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source,
    rel.confidence = row.confidence,
    rel.evidence_fact_ids = row.evidence_fact_ids`

	// #nosec G101 -- Cypher template; const name contains "Secret"/"Read" but the value is a parameterized graph-write query, not a credential literal
	secretsIAMGrantsSecretReadEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (p:SecretsIAMVaultPolicy {uid: row.vault_policy_uid})
MATCH (s:SecretsIAMSecretMetadataPath {uid: row.secret_path_uid})
MERGE (p)-[rel:SECRETS_IAM_GRANTS_SECRET_READ]->(s)
SET rel.capabilities = row.capabilities,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source,
    rel.confidence = row.confidence,
    rel.evidence_fact_ids = row.evidence_fact_ids`
)

// Retract templates. Each scopes by the reducer's own scope_id + evidence_source
// so retract removes only this projection's writes. Node retract uses
// DETACH DELETE (drops the node and any of its reducer-owned edges); the
// SecretsIAM* nodes carry the reducer scope_id, unlike the CloudResource /
// KubernetesWorkload endpoints, so a node-scoped predicate is correct here.
const (
	// #nosec G101 -- Cypher retract template; const name contains "Secret"/"Account" but the value is a scoped DETACH DELETE query with bound parameters, not a credential literal
	secretsIAMServiceAccountNodeRetractCypher = `MATCH (n:SecretsIAMServiceAccount)
WHERE n.scope_id = $scope_id AND n.evidence_source = $evidence_source
DETACH DELETE n`

	// #nosec G101 -- Cypher retract template; const name contains "Secret"/"Vault" but the value is a scoped DETACH DELETE query with bound parameters, not a credential literal
	secretsIAMVaultAuthRoleNodeRetractCypher = `MATCH (n:SecretsIAMVaultAuthRole)
WHERE n.scope_id = $scope_id AND n.evidence_source = $evidence_source
DETACH DELETE n`

	// #nosec G101 -- Cypher retract template; const name contains "Secret"/"Policy" but the value is a scoped DETACH DELETE query with bound parameters, not a credential literal
	secretsIAMVaultPolicyNodeRetractCypher = `MATCH (n:SecretsIAMVaultPolicy)
WHERE n.scope_id = $scope_id AND n.evidence_source = $evidence_source
DETACH DELETE n`

	// #nosec G101 -- Cypher retract template; const name contains "Secret"/"Metadata" but the value is a scoped DETACH DELETE query with bound parameters, not a credential literal
	secretsIAMSecretMetadataPathNodeRetractCypher = `MATCH (n:SecretsIAMSecretMetadataPath)
WHERE n.scope_id = $scope_id AND n.evidence_source = $evidence_source
DETACH DELETE n`

	// Edge retract is a stale-edge safety pass for reducer-owned
	// SECRETS_IAM_* edges whose START node is a retained endpoint
	// (KubernetesWorkload). Normal rows are removed by the ServiceAccount
	// DETACH DELETE below; this pass intentionally runs after node retraction so
	// Bolt-backed NornicDB does not have to delete a relationship and then
	// DETACH DELETE its target in one managed transaction.
	// #nosec G101 -- Cypher retract template; const name contains "Secret"/"Account" but the value is a scoped DELETE query with bound parameters, not a credential literal
	secretsIAMUsesServiceAccountEdgeRetractCypher = `MATCH (:KubernetesWorkload)-[rel:SECRETS_IAM_USES_SERVICE_ACCOUNT]->()
WHERE rel.scope_id = $scope_id AND rel.evidence_source = $evidence_source
DELETE rel`
)

// SecretsIAMGraphWriter materializes the reducer-owned secrets/IAM graph
// projection: the four SecretsIAM* nodes and the five resolvable
// SECRETS_IAM_* edges, plus scoped retract. It writes through the
// backend-neutral Executor seam and persists only the redaction-safe rows the
// extractor produced — it performs no judgement of its own.
type SecretsIAMGraphWriter struct {
	executor  Executor
	batchSize int
}

// NewSecretsIAMGraphWriter returns a writer backed by the given Executor. A
// batchSize of 0 or less uses DefaultBatchSize (500).
func NewSecretsIAMGraphWriter(executor Executor, batchSize int) *SecretsIAMGraphWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &SecretsIAMGraphWriter{executor: executor, batchSize: batchSize}
}

// WriteServiceAccountNodes upserts SecretsIAMServiceAccount nodes.
func (w *SecretsIAMGraphWriter) WriteServiceAccountNodes(ctx context.Context, rows []map[string]any) error {
	return w.writeBatched(ctx, secretsIAMServiceAccountNodeUpsertCypher, secretsIAMNodeServiceAccount, rows)
}

// WriteVaultAuthRoleNodes upserts SecretsIAMVaultAuthRole nodes.
func (w *SecretsIAMGraphWriter) WriteVaultAuthRoleNodes(ctx context.Context, rows []map[string]any) error {
	return w.writeBatched(ctx, secretsIAMVaultAuthRoleNodeUpsertCypher, secretsIAMNodeVaultAuthRole, rows)
}

// WriteVaultPolicyNodes upserts SecretsIAMVaultPolicy nodes.
func (w *SecretsIAMGraphWriter) WriteVaultPolicyNodes(ctx context.Context, rows []map[string]any) error {
	return w.writeBatched(ctx, secretsIAMVaultPolicyNodeUpsertCypher, secretsIAMNodeVaultPolicy, rows)
}

// WriteSecretMetadataPathNodes upserts SecretsIAMSecretMetadataPath nodes.
func (w *SecretsIAMGraphWriter) WriteSecretMetadataPathNodes(ctx context.Context, rows []map[string]any) error {
	return w.writeBatched(ctx, secretsIAMSecretMetadataPathNodeUpsertCypher, secretsIAMNodeSecretMetadataPath, rows)
}

// WriteUsesServiceAccountEdges upserts KubernetesWorkload->ServiceAccount edges.
func (w *SecretsIAMGraphWriter) WriteUsesServiceAccountEdges(ctx context.Context, rows []map[string]any) error {
	return w.writeBatched(ctx, secretsIAMUsesServiceAccountEdgeUpsertCypher, "SECRETS_IAM_USES_SERVICE_ACCOUNT", rows)
}

// WriteAssumesIAMRoleEdges upserts SecretsIAMServiceAccount->CloudResource
// (IAM role) edges. The CloudResource MATCH resolves the existing IAM-role node
// by uid; a row whose CloudResource endpoint is absent is a no-op, never a
// fabricated CloudResource node. The edge's START node is the reducer-owned
// ServiceAccount, so the node DETACH DELETE retract removes it (no separate edge
// retract needed, unlike the workload edge).
func (w *SecretsIAMGraphWriter) WriteAssumesIAMRoleEdges(ctx context.Context, rows []map[string]any) error {
	return w.writeBatched(ctx, secretsIAMAssumesIAMRoleEdgeUpsertCypher, "SECRETS_IAM_ASSUMES_IAM_ROLE", rows)
}

// WriteAuthenticatesVaultRoleEdges upserts ServiceAccount->VaultAuthRole edges.
func (w *SecretsIAMGraphWriter) WriteAuthenticatesVaultRoleEdges(ctx context.Context, rows []map[string]any) error {
	return w.writeBatched(ctx, secretsIAMAuthenticatesVaultRoleEdgeUpsertCypher, "SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE", rows)
}

// WriteUsesVaultPolicyEdges upserts VaultAuthRole->VaultPolicy edges.
func (w *SecretsIAMGraphWriter) WriteUsesVaultPolicyEdges(ctx context.Context, rows []map[string]any) error {
	return w.writeBatched(ctx, secretsIAMUsesVaultPolicyEdgeUpsertCypher, "SECRETS_IAM_USES_VAULT_POLICY", rows)
}

// WriteGrantsSecretReadEdges upserts VaultPolicy->SecretMetadataPath edges.
func (w *SecretsIAMGraphWriter) WriteGrantsSecretReadEdges(ctx context.Context, rows []map[string]any) error {
	return w.writeBatched(ctx, secretsIAMGrantsSecretReadEdgeUpsertCypher, "SECRETS_IAM_GRANTS_SECRET_READ", rows)
}

// RetractScope removes every reducer-owned SecretsIAM* node and edge for the
// given scopes before a fresh generation reprojects them. It is a no-op for an
// empty scope set. It never touches CloudResource or KubernetesWorkload nodes.
func (w *SecretsIAMGraphWriter) RetractScope(
	ctx context.Context,
	scopeIDs []string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("secrets/iam graph writer executor is required")
	}
	// DETACH DELETE reducer-owned nodes first. That removes normal
	// workload->ServiceAccount edges while keeping retained endpoint nodes
	// intact. The final edge retract is only a stale-edge safety pass for
	// malformed legacy rows whose target node is not present. Each statement
	// carries its own bounded entity label for 3 AM operator diagnostics.
	retracts := []struct {
		label  string
		cypher string
	}{
		{secretsIAMNodeServiceAccount, secretsIAMServiceAccountNodeRetractCypher},
		{secretsIAMNodeVaultAuthRole, secretsIAMVaultAuthRoleNodeRetractCypher},
		{secretsIAMNodeVaultPolicy, secretsIAMVaultPolicyNodeRetractCypher},
		{secretsIAMNodeSecretMetadataPath, secretsIAMSecretMetadataPathNodeRetractCypher},
		{"SECRETS_IAM_USES_SERVICE_ACCOUNT", secretsIAMUsesServiceAccountEdgeRetractCypher},
	}
	stmts := make([]Statement, 0, len(retracts)*len(scopeIDs))
	for _, scopeID := range scopeIDs {
		for _, r := range retracts {
			stmts = append(stmts, Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    r.cypher,
				Parameters: map[string]any{
					"scope_id":                      scopeID,
					"evidence_source":               evidenceSource,
					StatementMetadataPhaseKey:       canonicalPhaseSecretsIAMGraph,
					StatementMetadataEntityLabelKey: r.label,
					StatementMetadataSummaryKey:     fmt.Sprintf("retract entity=%s scope=1", r.label),
				},
			})
		}
	}
	// Retracts commit one bounded entity cleanup at a time. The statements are
	// idempotent by scope+evidence_source, and this keeps NornicDB from holding
	// one mixed DELETE/DETACH DELETE transaction across all secrets/IAM labels.
	return w.dispatchSequential(ctx, stmts)
}

// writeBatched upserts rows in deterministic batches. When the executor
// implements GroupExecutor all batches dispatch in one atomic transaction;
// otherwise sequentially. The write is idempotent: a duplicate row converges on
// one node/edge identity, and a missing edge endpoint is a no-op (no fabricated
// node).
func (w *SecretsIAMGraphWriter) writeBatched(ctx context.Context, cypher, label string, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("secrets/iam graph writer executor is required")
	}
	stmts := buildBatchedStatements(cypher, rows, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Operation = OperationCanonicalUpsert
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseSecretsIAMGraph
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = label
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf("entity=%s rows=%d", label, len(batchRows))
	}
	return w.dispatch(ctx, stmts)
}

func (w *SecretsIAMGraphWriter) dispatch(ctx context.Context, stmts []Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	if ge, ok := w.executor.(GroupExecutor); ok {
		if err := ge.ExecuteGroup(ctx, stmts); err != nil {
			return WrapRetryableNeo4jError(err)
		}
		return nil
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}

func (w *SecretsIAMGraphWriter) dispatchSequential(ctx context.Context, stmts []Statement) error {
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
