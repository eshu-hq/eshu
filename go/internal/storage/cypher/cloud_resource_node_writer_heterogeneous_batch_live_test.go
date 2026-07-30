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

// This file is the live-backend regression proof for issue #5714/#5055:
// CloudResourceNodeWriter.WriteCloudResourceNodes must never persist a
// stringified "row.<key>" literal for a key one row of a heterogeneous batch
// omits. It is BACKEND-GATED: it SKIPs cleanly unless
// ESHU_CLOUDRESOURCE_NODE_WRITER_LIVE is set AND a Bolt backend is configured,
// so the default package test run never requires Docker or a live graph
// backend. Prove-theory-first for the shared-writer fix (Option A: writer
// default-fill vs Option B: Cypher coalesce) is recorded in
// docs/internal/evidence/5714-cloudresource-row-key-defaults.md; both were
// measured viable against the pinned NornicDB image, and Option A was chosen
// because it needs no Cypher rewrite and is unit-testable without a live
// backend (TestCloudResourceNodeWriterDefaultFillsMissingRowKeys).

const cloudResourceNodeWriterLiveEnv = "ESHU_CLOUDRESOURCE_NODE_WRITER_LIVE"

func cloudResourceNodeWriterLiveEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(cloudResourceNodeWriterLiveEnv))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func cloudResourceNodeWriterLiveTestRunID(t *testing.T) string {
	t.Helper()

	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatalf("generate live test nonce: %v", err)
	}
	return hex.EncodeToString(nonce[:])
}

// liveCloudResourceExecutor adapts a Bolt driver to the cypher.Executor and
// GroupExecutor seams plus a read helper for read-back assertions.
type liveCloudResourceExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (e liveCloudResourceExecutor) Execute(ctx context.Context, stmt cypher.Statement) error {
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

func (e liveCloudResourceExecutor) ExecuteGroup(ctx context.Context, stmts []cypher.Statement) error {
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

func (e liveCloudResourceExecutor) readWorkloadID(ctx context.Context, uid string) (string, error) {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, `MATCH (r:CloudResource {uid: $uid}) RETURN r.workload_id AS v`, map[string]any{"uid": uid})
	if err != nil {
		return "", fmt.Errorf("run read: %w", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		return "", fmt.Errorf("collect read: %w", err)
	}
	// Both of these were `_`-discarded originally, which made the whole test
	// vacuous: a missing field, a Cypher null, or a non-string value all
	// collapse to "" — the exact value the bare-node assertion expects — so the
	// test passed without proving the property it advertises (PR #5867 review).
	// Fail loudly on each instead.
	value, ok := record.Get("v")
	if !ok {
		return "", fmt.Errorf("read workload_id: record has no field %q", "v")
	}
	if value == nil {
		return "", fmt.Errorf("read workload_id: field %q is null, want a string", "v")
	}
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("read workload_id: field %q is %T (%v), want string", "v", value, value)
	}
	return s, nil
}

// TestCloudResourceNodeWriterLiveHeterogeneousBatchNeverPersistsLiteral is the
// live-backend, non-vacuous regression proof for issue #5714/#5055: a batch
// with one anchor-bearing row (workload_id present) and one bare row
// (workload_id omitted, the exact shape applyCloudResourceServiceAnchorFields and
// azureCloudResourceNodeRow produced before their fix) must read back "" on
// the bare node's workload_id, never the stringified literal
// "row.workload_id" the pinned NornicDB backend persists for a key missing
// from one row of a heterogeneous UNWIND $rows list. It skips unless
// ESHU_CLOUDRESOURCE_NODE_WRITER_LIVE is enabled.
func TestCloudResourceNodeWriterLiveHeterogeneousBatchNeverPersistsLiteral(t *testing.T) {
	if !cloudResourceNodeWriterLiveEnabled() {
		t.Skipf("set %s=1 (and Bolt env) to run the live CloudResource node writer heterogeneous-batch proof", cloudResourceNodeWriterLiveEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = driver.Close(closeCtx)
	})

	exec := liveCloudResourceExecutor{driver: driver, database: cfg.DatabaseName}
	runID := cloudResourceNodeWriterLiveTestRunID(t)
	anchorUID := "cloud:test:eshu:cloudresource-live:" + runID + ":anchor-bearing"
	bareUID := "cloud:test:eshu:cloudresource-live:" + runID + ":bare"

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = exec.Execute(cleanupCtx, cypher.Statement{
			Cypher:     `MATCH (r:CloudResource) WHERE r.uid IN $uids DETACH DELETE r`,
			Parameters: map[string]any{"uids": []string{anchorUID, bareUID}},
		})
	})

	writer := cypher.NewCloudResourceNodeWriter(exec, 0)

	// The anchor-bearing row supplies workload_id; the bare row deliberately
	// OMITS it — the exact production shape applyCloudResourceServiceAnchorFields
	// (no service-anchor decision) and azureCloudResourceNodeRow produced
	// before the #5714/#5055 fix. Both rows are still missing several other
	// SET-referenced keys entirely (arn, resource_id, name, ... ) to exercise
	// the shared-writer default-fill backstop broadly, not just for
	// workload_id.
	rows := []map[string]any{
		{"uid": anchorUID, "resource_type": "aws_vpclattice_listener", "workload_id": "workload:orders-api"},
		{"uid": bareUID, "resource_type": "aws_ec2_vpc"},
	}
	if err := writer.WriteCloudResourceNodes(ctx, rows, "test/cloudresource-live/"+runID); err != nil {
		t.Fatalf("WriteCloudResourceNodes() error = %v", err)
	}

	anchorValue, err := exec.readWorkloadID(ctx, anchorUID)
	if err != nil {
		t.Fatalf("read anchor-bearing workload_id: %v", err)
	}
	if anchorValue != "workload:orders-api" {
		t.Fatalf("anchor-bearing workload_id = %q, want %q", anchorValue, "workload:orders-api")
	}

	bareValue, err := exec.readWorkloadID(ctx, bareUID)
	if err != nil {
		t.Fatalf("read bare workload_id: %v", err)
	}
	if bareValue == "row.workload_id" {
		t.Fatalf("bare node workload_id = %q: the pinned NornicDB missing-map-key-in-UNWIND "+
			"literal-corruption bug reproduced — the shared-writer default-fill backstop did not run", bareValue)
	}
	if bareValue != "" {
		t.Fatalf("bare node workload_id = %q, want empty string", bareValue)
	}
}
