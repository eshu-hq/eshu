package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type ingesterNeo4jExecutor struct {
	Driver                 neo4jdriver.DriverWithContext
	DatabaseName           string
	TxTimeout              time.Duration
	ProfileGroupStatements bool
}

func (e ingesterNeo4jExecutor) Execute(ctx context.Context, statement sourcecypher.Statement) error {
	if e.Driver == nil {
		return fmt.Errorf("neo4j driver is required")
	}

	session := e.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.DatabaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, statement.Cypher, statement.Parameters, e.transactionConfigurers()...)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

func (e ingesterNeo4jExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if e.Driver == nil {
		return fmt.Errorf("neo4j driver is required")
	}
	if len(stmts) == 0 {
		return nil
	}

	session := e.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.DatabaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		err := sourcecypher.ExecuteProfiledStatementGroup(ctx, stmts, func(ctx context.Context, stmt sourcecypher.Statement) error {
			result, runErr := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
			if runErr != nil {
				return runErr
			}
			if _, consumeErr := result.Consume(ctx); consumeErr != nil {
				return consumeErr
			}
			return nil
		}, e.ProfileGroupStatements, nil)
		if err != nil {
			return nil, err
		}
		return nil, nil
	}, e.transactionConfigurers()...)
	return err
}

func (e ingesterNeo4jExecutor) transactionConfigurers() []func(*neo4jdriver.TransactionConfig) {
	if e.TxTimeout <= 0 {
		return nil
	}
	return []func(*neo4jdriver.TransactionConfig){neo4jdriver.WithTxTimeout(e.TxTimeout)}
}

func canonicalTransactionTimeout(graphBackend runtimecfg.GraphBackend, getenv func(string) string) time.Duration {
	if graphBackend != runtimecfg.GraphBackendNornicDB {
		return 0
	}
	return nornicDBCanonicalWriteTimeout(getenv)
}

func neo4jProfileGroupStatements(getenv func(string) string) (bool, error) {
	raw := strings.TrimSpace(getenv("ESHU_NEO4J_PROFILE_GROUP_STATEMENTS"))
	if raw == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse ESHU_NEO4J_PROFILE_GROUP_STATEMENTS=%q: %w", raw, err)
	}
	return enabled, nil
}

type ingesterNeo4jDriverCloser struct {
	Driver neo4jdriver.DriverWithContext
}

func (c ingesterNeo4jDriverCloser) Close() error {
	return closeIngesterNeo4jDriver(c.Driver)
}

func closeIngesterNeo4jDriver(driver neo4jdriver.DriverWithContext) error {
	if driver == nil {
		return nil
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), ingesterConnectionTimeout)
	defer cancel()
	return driver.Close(closeCtx)
}
