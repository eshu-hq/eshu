// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// The helpers below are copies of the live-Bolt scaffolding that moved to
// the cloud/aws/iam leaf with the secrets/IAM graph writer (#6694). They
// are generic Bolt read/write/count scaffolding shared by the semantic-entity
// live tests that stayed in this package: external test packages cannot
// import another package's _test files, so each side carries its own copy.
// Keep the copies behavior-identical. The SecretsIAM names are kept so the
// long-standing call sites need no churn.

// liveSecretsIAMExecutor adapts a Bolt driver to the cypher.Executor and
// GroupExecutor seams plus read helpers for read-back assertions.
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

func secretsIAMLiveTestRunID(t *testing.T) string {
	t.Helper()

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatalf("generate live secrets/iam test nonce: %v", err)
	}
	testName := strings.NewReplacer("/", "-", " ", "-", ":", "-", "#", "-").Replace(t.Name())
	return fmt.Sprintf("%s-%s", testName, hex.EncodeToString(nonce[:]))
}

func registerSecretsIAMLiveDriverClose(t *testing.T, closeFn func(context.Context) error) {
	t.Helper()

	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = closeFn(closeCtx)
	})
}
