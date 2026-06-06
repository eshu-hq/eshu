package cypher_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// This file is the ADR #1314 §11 TRUE live-backend conformance proof for the
// secrets/IAM graph writer. It is BACKEND-GATED: it SKIPs cleanly unless
// ESHU_SECRETS_IAM_GRAPH_LIVE is set AND a Bolt backend is configured, so the
// default package test run never requires Docker or remote graph credentials. It
// proves the four SecretsIAM* node families and the five resolvable
// SECRETS_IAM_* edge families MERGE, read back, and scoped-retract against a real
// NornicDB/Neo4j-compatible backend. It NEVER fabricates a passing live proof:
// without a backend it skips, and with a backend it fails on any mismatch.
//
// The live reducer projection remains gated OFF by default
// (ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED) pending §14 sign-off; this
// test exercises only the standalone writer against a developer-supplied backend
// and cleans up every node it creates.

const secretsIAMLiveEnv = "ESHU_SECRETS_IAM_GRAPH_LIVE"

func secretsIAMLiveEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(secretsIAMLiveEnv))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// liveSecretsIAMExecutor adapts a Bolt driver to the cypher.Executor and
// GroupExecutor seams plus a read helper for read-back assertions.
type liveSecretsIAMExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (e liveSecretsIAMExecutor) Execute(ctx context.Context, stmt cypher.Statement) error {
	return e.ExecuteGroup(ctx, []cypher.Statement{stmt})
}

func (e liveSecretsIAMExecutor) ExecuteGroup(ctx context.Context, stmts []cypher.Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()

	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		for _, stmt := range stmts {
			result, runErr := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
			if runErr != nil {
				return nil, runErr
			}
			if _, consumeErr := result.Consume(ctx); consumeErr != nil {
				return nil, consumeErr
			}
		}
		return nil, nil
	})
	if err != nil {
		return fmt.Errorf("execute write group: %w", err)
	}
	return nil
}

func (e liveSecretsIAMExecutor) count(ctx context.Context, cypherText string, params map[string]any) (int64, error) {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypherText, params)
	if err != nil {
		return 0, fmt.Errorf("run count query: %w", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("collect count query: %w", err)
	}
	value, ok := record.Values[0].(int64)
	if !ok {
		return 0, fmt.Errorf("count value is not int64: %T", record.Values[0])
	}
	return value, nil
}

// TestSecretsIAMGraphWriterLiveConformance writes all four node families and all
// five edges, reads them back, then proves scoped retract removes only the
// reducer-owned rows. It skips unless ESHU_SECRETS_IAM_GRAPH_LIVE is enabled.
func TestSecretsIAMGraphWriterLiveConformance(t *testing.T) {
	if !secretsIAMLiveEnabled() {
		t.Skipf("set %s=1 (and Bolt env) to run live secrets/iam graph conformance", secretsIAMLiveEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = driver.Close(closeCtx)
	}()

	exec := liveSecretsIAMExecutor{driver: driver, database: cfg.DatabaseName}
	const scope = "scope:secrets-iam-live"
	const evidence = "reducer/secrets-iam-graph"

	// The workload and IAM-role edges need retained endpoint nodes to MATCH.
	// Create them (and clean them up) so the endpoint edges resolve rather than
	// no-oping.
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MERGE (w:KubernetesWorkload {uid: $uid})`,
		Parameters: map[string]any{"uid": "k8s://live/w-1"},
	}); err != nil {
		t.Fatalf("seed workload endpoint: %v", err)
	}
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MERGE (c:CloudResource {uid: $uid})`,
		Parameters: map[string]any{"uid": "cloud:iam-role-live"},
	}); err != nil {
		t.Fatalf("seed iam role endpoint: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = exec.Execute(cleanupCtx, cypher.Statement{
			Cypher:     `MATCH (w:KubernetesWorkload {uid: $uid}) DETACH DELETE w`,
			Parameters: map[string]any{"uid": "k8s://live/w-1"},
		})
		_ = exec.Execute(cleanupCtx, cypher.Statement{
			Cypher:     `MATCH (c:CloudResource {uid: $uid}) DETACH DELETE c`,
			Parameters: map[string]any{"uid": "cloud:iam-role-live"},
		})
		_ = cypher.NewSecretsIAMGraphWriter(exec, 0).RetractScope(cleanupCtx, []string{scope}, evidence)
	})

	w := cypher.NewSecretsIAMGraphWriter(exec, 0)
	meta := map[string]any{"scope_id": scope, "generation_id": "gen-1", "evidence_source": evidence, "confidence": "exact"}
	node := func(extra map[string]any) map[string]any {
		row := map[string]any{}
		for k, v := range meta {
			row[k] = v
		}
		for k, v := range extra {
			row[k] = v
		}
		return row
	}

	writes := []struct {
		name string
		fn   func() error
	}{
		{"service-account", func() error {
			return w.WriteServiceAccountNodes(ctx, []map[string]any{node(map[string]any{"uid": "sha256:sa1"})})
		}},
		{"vault-auth-role", func() error {
			return w.WriteVaultAuthRoleNodes(ctx, []map[string]any{node(map[string]any{"uid": "sha256:vr1", "vault_mount_join_key": "sha256:mount"})})
		}},
		{"vault-policy", func() error {
			return w.WriteVaultPolicyNodes(ctx, []map[string]any{node(map[string]any{"uid": "sha256:pol1"})})
		}},
		{"secret-path", func() error {
			return w.WriteSecretMetadataPathNodes(ctx, []map[string]any{node(map[string]any{"uid": "sha256:path1", "vault_mount_join_key": "sha256:mount", "kv_path_fingerprint": "sha256:kv"})})
		}},
	}
	for _, write := range writes {
		if err := write.fn(); err != nil {
			t.Fatalf("write %s nodes: %v", write.name, err)
		}
	}

	edgeWrites := []struct {
		name string
		fn   func() error
	}{
		{"uses-service-account", func() error {
			return w.WriteUsesServiceAccountEdges(ctx, []map[string]any{node(map[string]any{"workload_uid": "k8s://live/w-1", "service_account_uid": "sha256:sa1", "evidence_fact_ids": []string{"f1"}})})
		}},
		{"assumes-iam-role", func() error {
			return w.WriteAssumesIAMRoleEdges(ctx, []map[string]any{node(map[string]any{"service_account_uid": "sha256:sa1", "cloud_resource_uid": "cloud:iam-role-live", "assume_mode": "web_identity", "evidence_fact_ids": []string{"f1"}})})
		}},
		{"authenticates-vault-role", func() error {
			return w.WriteAuthenticatesVaultRoleEdges(ctx, []map[string]any{node(map[string]any{"service_account_uid": "sha256:sa1", "vault_auth_role_uid": "sha256:vr1", "evidence_fact_ids": []string{"f1"}})})
		}},
		{"uses-vault-policy", func() error {
			return w.WriteUsesVaultPolicyEdges(ctx, []map[string]any{node(map[string]any{"vault_auth_role_uid": "sha256:vr1", "vault_policy_uid": "sha256:pol1", "evidence_fact_ids": []string{"f1"}})})
		}},
		{"grants-secret-read", func() error {
			return w.WriteGrantsSecretReadEdges(ctx, []map[string]any{node(map[string]any{"vault_policy_uid": "sha256:pol1", "secret_path_uid": "sha256:path1", "capabilities": []string{"read"}, "evidence_fact_ids": []string{"f1"}})})
		}},
	}
	for _, edge := range edgeWrites {
		if err := edge.fn(); err != nil {
			t.Fatalf("write %s edge: %v", edge.name, err)
		}
	}

	// Read-back: every node family and every edge family must be present once.
	nodeCounts := map[string]string{
		"SecretsIAMServiceAccount":     `MATCH (n:SecretsIAMServiceAccount {uid: "sha256:sa1", scope_id: $scope}) RETURN count(n)`,
		"SecretsIAMVaultAuthRole":      `MATCH (n:SecretsIAMVaultAuthRole {uid: "sha256:vr1", scope_id: $scope}) RETURN count(n)`,
		"SecretsIAMVaultPolicy":        `MATCH (n:SecretsIAMVaultPolicy {uid: "sha256:pol1", scope_id: $scope}) RETURN count(n)`,
		"SecretsIAMSecretMetadataPath": `MATCH (n:SecretsIAMSecretMetadataPath {uid: "sha256:path1", scope_id: $scope}) RETURN count(n)`,
	}
	for label, query := range nodeCounts {
		got, err := exec.count(ctx, query, map[string]any{"scope": scope})
		if err != nil {
			t.Fatalf("count %s: %v", label, err)
		}
		if got != 1 {
			t.Fatalf("%s read-back count = %d, want 1", label, got)
		}
	}

	edgeCounts := map[string]string{
		"SECRETS_IAM_USES_SERVICE_ACCOUNT":        `MATCH (:KubernetesWorkload)-[r:SECRETS_IAM_USES_SERVICE_ACCOUNT]->(:SecretsIAMServiceAccount) WHERE r.scope_id = $scope RETURN count(r)`,
		"SECRETS_IAM_ASSUMES_IAM_ROLE":            `MATCH (:SecretsIAMServiceAccount)-[r:SECRETS_IAM_ASSUMES_IAM_ROLE]->(:CloudResource) WHERE r.scope_id = $scope RETURN count(r)`,
		"SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE": `MATCH (:SecretsIAMServiceAccount)-[r:SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE]->(:SecretsIAMVaultAuthRole) WHERE r.scope_id = $scope RETURN count(r)`,
		"SECRETS_IAM_USES_VAULT_POLICY":           `MATCH (:SecretsIAMVaultAuthRole)-[r:SECRETS_IAM_USES_VAULT_POLICY]->(:SecretsIAMVaultPolicy) WHERE r.scope_id = $scope RETURN count(r)`,
		"SECRETS_IAM_GRANTS_SECRET_READ":          `MATCH (:SecretsIAMVaultPolicy)-[r:SECRETS_IAM_GRANTS_SECRET_READ]->(:SecretsIAMSecretMetadataPath) WHERE r.scope_id = $scope RETURN count(r)`,
	}
	for relType, query := range edgeCounts {
		got, err := exec.count(ctx, query, map[string]any{"scope": scope})
		if err != nil {
			t.Fatalf("count %s: %v", relType, err)
		}
		if got != 1 {
			t.Fatalf("%s read-back count = %d, want 1", relType, got)
		}
	}

	// Scoped retract removes the four reducer-owned node families (and their
	// edges), leaving the retained KubernetesWorkload and CloudResource endpoints
	// intact.
	if err := w.RetractScope(ctx, []string{scope}, evidence); err != nil {
		t.Fatalf("retract scope: %v", err)
	}
	for label, query := range nodeCounts {
		got, err := exec.count(ctx, query, map[string]any{"scope": scope})
		if err != nil {
			t.Fatalf("post-retract count %s: %v", label, err)
		}
		if got != 0 {
			t.Fatalf("%s survived retract: count = %d, want 0", label, got)
		}
	}
	workloadCount, err := exec.count(ctx, `MATCH (w:KubernetesWorkload {uid: "k8s://live/w-1"}) RETURN count(w)`, nil)
	if err != nil {
		t.Fatalf("post-retract workload count: %v", err)
	}
	if workloadCount != 1 {
		t.Fatalf("retract deleted the retained KubernetesWorkload endpoint: count = %d, want 1", workloadCount)
	}
	cloudResourceCount, err := exec.count(ctx, `MATCH (c:CloudResource {uid: "cloud:iam-role-live"}) RETURN count(c)`, nil)
	if err != nil {
		t.Fatalf("post-retract CloudResource count: %v", err)
	}
	if cloudResourceCount != 1 {
		t.Fatalf("retract deleted the retained CloudResource endpoint: count = %d, want 1", cloudResourceCount)
	}
}
