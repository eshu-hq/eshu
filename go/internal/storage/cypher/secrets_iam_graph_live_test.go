// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
// (ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED) pending target-bound
// activation proof; this test exercises only the standalone writer against a
// developer-supplied backend and cleans up every node it creates.

const secretsIAMLiveEnv = "ESHU_SECRETS_IAM_GRAPH_LIVE"

func secretsIAMLiveEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(secretsIAMLiveEnv))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func secretsIAMLiveTestRunID(t *testing.T) string {
	t.Helper()

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatalf("generate live secrets/iam test nonce: %v", err)
	}
	testName := strings.NewReplacer("/", "-", " ", "-", ":", "-", "#", "-").Replace(t.Name())
	return fmt.Sprintf("%s-%s", testName, hex.EncodeToString(nonce[:]))
}

func TestSecretsIAMLiveTestRunIDUsesNamespacedNonce(t *testing.T) {
	first := secretsIAMLiveTestRunID(t)
	second := secretsIAMLiveTestRunID(t)
	prefix := strings.NewReplacer("/", "-", " ", "-", ":", "-", "#", "-").Replace(t.Name()) + "-"

	if first == second {
		t.Fatalf("live test run IDs collided: %q", first)
	}
	for _, got := range []string{first, second} {
		if !strings.HasPrefix(got, prefix) {
			t.Fatalf("live test run ID %q missing test namespace prefix %q", got, prefix)
		}
		nonce := strings.TrimPrefix(got, prefix)
		if len(nonce) != 32 {
			t.Fatalf("live test run ID %q nonce length = %d, want 32", got, len(nonce))
		}
		if _, err := hex.DecodeString(nonce); err != nil {
			t.Fatalf("live test run ID %q nonce is not hex: %v", got, err)
		}
	}
}

func registerSecretsIAMLiveDriverClose(t *testing.T, closeFn func(context.Context) error) {
	t.Helper()

	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = closeFn(closeCtx)
	})
}

func TestSecretsIAMLiveDriverCloseRunsAfterResourceCleanup(t *testing.T) {
	var events []string

	t.Run("order", func(t *testing.T) {
		registerSecretsIAMLiveDriverClose(t, func(context.Context) error {
			events = append(events, "close")
			return nil
		})
		t.Cleanup(func() {
			events = append(events, "resources")
		})
	})

	if got, want := strings.Join(events, ","), "resources,close"; got != want {
		t.Fatalf("cleanup order = %q, want %q", got, want)
	}
}

// liveSecretsIAMExecutor adapts a Bolt driver to the cypher.Executor and
// GroupExecutor seams plus a read helper for read-back assertions.
type liveSecretsIAMExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (e liveSecretsIAMExecutor) Execute(ctx context.Context, stmt cypher.Statement) error {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return fmt.Errorf("execute write: %w", err)
	}
	if _, err := result.Consume(ctx); err != nil {
		return fmt.Errorf("consume write: %w", err)
	}
	return nil
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

func (e liveSecretsIAMExecutor) values(ctx context.Context, cypherText string, params map[string]any) ([]any, error) {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypherText, params)
	if err != nil {
		return nil, fmt.Errorf("run values query: %w", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect values query: %w", err)
	}
	values := make([]any, 0, len(records))
	for _, record := range records {
		values = append(values, record.Values...)
	}
	return values, nil
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
	registerSecretsIAMLiveDriverClose(t, driver.Close)

	exec := liveSecretsIAMExecutor{driver: driver, database: cfg.DatabaseName}
	runID := secretsIAMLiveTestRunID(t)
	scope := "scope:test:secrets-iam-live:" + runID
	evidence := "test/reducer/secrets-iam-graph/" + runID
	workloadUID := "k8s://test/eshu/secrets-iam/" + runID + "/workload"
	cloudResourceUID := "cloud:test:eshu:secrets-iam:" + runID + ":iam-role"
	serviceAccountUID := "test:eshu:secrets-iam:" + runID + ":service-account"
	vaultAuthRoleUID := "test:eshu:secrets-iam:" + runID + ":vault-auth-role"
	vaultPolicyUID := "test:eshu:secrets-iam:" + runID + ":vault-policy"
	secretPathUID := "test:eshu:secrets-iam:" + runID + ":secret-path"
	vaultMountJoinKey := "test:eshu:secrets-iam:" + runID + ":vault-mount"
	kvPathFingerprint := "test:eshu:secrets-iam:" + runID + ":kv-path"

	// The workload and IAM-role edges need retained endpoint nodes to MATCH.
	// Create them (and clean them up) so the endpoint edges resolve rather than
	// no-oping.
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MERGE (w:KubernetesWorkload {uid: $uid})`,
		Parameters: map[string]any{"uid": workloadUID},
	}); err != nil {
		t.Fatalf("seed workload endpoint: %v", err)
	}
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MERGE (c:CloudResource {uid: $uid})`,
		Parameters: map[string]any{"uid": cloudResourceUID},
	}); err != nil {
		t.Fatalf("seed iam role endpoint: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = exec.Execute(cleanupCtx, cypher.Statement{
			Cypher:     `MATCH (w:KubernetesWorkload {uid: $uid}) DETACH DELETE w`,
			Parameters: map[string]any{"uid": workloadUID},
		})
		_ = exec.Execute(cleanupCtx, cypher.Statement{
			Cypher:     `MATCH (c:CloudResource {uid: $uid}) DETACH DELETE c`,
			Parameters: map[string]any{"uid": cloudResourceUID},
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
			return w.WriteServiceAccountNodes(ctx, []map[string]any{node(map[string]any{"uid": serviceAccountUID})})
		}},
		{"vault-auth-role", func() error {
			return w.WriteVaultAuthRoleNodes(ctx, []map[string]any{node(map[string]any{"uid": vaultAuthRoleUID, "vault_mount_join_key": vaultMountJoinKey})})
		}},
		{"vault-policy", func() error {
			return w.WriteVaultPolicyNodes(ctx, []map[string]any{node(map[string]any{"uid": vaultPolicyUID})})
		}},
		{"secret-path", func() error {
			return w.WriteSecretMetadataPathNodes(ctx, []map[string]any{node(map[string]any{"uid": secretPathUID, "vault_mount_join_key": vaultMountJoinKey, "kv_path_fingerprint": kvPathFingerprint})})
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
			return w.WriteUsesServiceAccountEdges(ctx, []map[string]any{node(map[string]any{"workload_uid": workloadUID, "service_account_uid": serviceAccountUID, "evidence_fact_ids": []string{"f1"}})})
		}},
		{"assumes-iam-role", func() error {
			return w.WriteAssumesIAMRoleEdges(ctx, []map[string]any{node(map[string]any{"service_account_uid": serviceAccountUID, "cloud_resource_uid": cloudResourceUID, "assume_mode": "web_identity", "evidence_fact_ids": []string{"f1"}})})
		}},
		{"authenticates-vault-role", func() error {
			return w.WriteAuthenticatesVaultRoleEdges(ctx, []map[string]any{node(map[string]any{"service_account_uid": serviceAccountUID, "vault_auth_role_uid": vaultAuthRoleUID, "evidence_fact_ids": []string{"f1"}})})
		}},
		{"uses-vault-policy", func() error {
			return w.WriteUsesVaultPolicyEdges(ctx, []map[string]any{node(map[string]any{"vault_auth_role_uid": vaultAuthRoleUID, "vault_policy_uid": vaultPolicyUID, "evidence_fact_ids": []string{"f1"}})})
		}},
		{"grants-secret-read", func() error {
			return w.WriteGrantsSecretReadEdges(ctx, []map[string]any{node(map[string]any{"vault_policy_uid": vaultPolicyUID, "secret_path_uid": secretPathUID, "capabilities": []string{"read"}, "evidence_fact_ids": []string{"f1"}})})
		}},
	}
	for _, edge := range edgeWrites {
		if err := edge.fn(); err != nil {
			t.Fatalf("write %s edge: %v", edge.name, err)
		}
	}

	// Read-back: every node family and every edge family must be present once.
	nodeCounts := map[string]struct {
		query string
		uid   string
	}{
		"SecretsIAMServiceAccount": {
			query: `MATCH (n:SecretsIAMServiceAccount {uid: $uid, scope_id: $scope}) RETURN count(n)`,
			uid:   serviceAccountUID,
		},
		"SecretsIAMVaultAuthRole": {
			query: `MATCH (n:SecretsIAMVaultAuthRole {uid: $uid, scope_id: $scope}) RETURN count(n)`,
			uid:   vaultAuthRoleUID,
		},
		"SecretsIAMVaultPolicy": {
			query: `MATCH (n:SecretsIAMVaultPolicy {uid: $uid, scope_id: $scope}) RETURN count(n)`,
			uid:   vaultPolicyUID,
		},
		"SecretsIAMSecretMetadataPath": {
			query: `MATCH (n:SecretsIAMSecretMetadataPath {uid: $uid, scope_id: $scope}) RETURN count(n)`,
			uid:   secretPathUID,
		},
	}
	for label, count := range nodeCounts {
		got, err := exec.count(ctx, count.query, map[string]any{"scope": scope, "uid": count.uid})
		if err != nil {
			t.Fatalf("count %s: %v", label, err)
		}
		if got != 1 {
			t.Fatalf("%s read-back count = %d, want 1", label, got)
		}
		t.Logf("sanitized node count %s=%d", label, got)
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
		t.Logf("sanitized relationship count %s=%d", relType, got)
	}

	suspiciousValues, err := countSuspiciousSecretsIAMLiveValues(ctx, exec, scope, evidence)
	if err != nil {
		t.Fatalf("sensitive property spot-check: %v", err)
	}
	if suspiciousValues != 0 {
		t.Fatalf("sensitive property spot-check found %d suspicious values, want 0", suspiciousValues)
	}
	t.Logf("sanitized sensitive property spot-check suspicious_values=%d", suspiciousValues)

	// Scoped retract removes the four reducer-owned node families (and their
	// edges), leaving the retained KubernetesWorkload and CloudResource endpoints
	// intact.
	if err := w.RetractScope(ctx, []string{scope}, evidence); err != nil {
		t.Fatalf("retract scope: %v", err)
	}
	for label, count := range nodeCounts {
		got, err := exec.count(ctx, count.query, map[string]any{"scope": scope, "uid": count.uid})
		if err != nil {
			t.Fatalf("post-retract count %s: %v", label, err)
		}
		if got != 0 {
			t.Fatalf("%s survived retract: count = %d, want 0", label, got)
		}
	}
	workloadCount, err := exec.count(ctx, `MATCH (w:KubernetesWorkload {uid: $uid}) RETURN count(w)`, map[string]any{"uid": workloadUID})
	if err != nil {
		t.Fatalf("post-retract workload count: %v", err)
	}
	if workloadCount != 1 {
		t.Fatalf("retract deleted the retained KubernetesWorkload endpoint: count = %d, want 1", workloadCount)
	}
	cloudResourceCount, err := exec.count(ctx, `MATCH (c:CloudResource {uid: $uid}) RETURN count(c)`, map[string]any{"uid": cloudResourceUID})
	if err != nil {
		t.Fatalf("post-retract CloudResource count: %v", err)
	}
	if cloudResourceCount != 1 {
		t.Fatalf("retract deleted the retained CloudResource endpoint: count = %d, want 1", cloudResourceCount)
	}
}
