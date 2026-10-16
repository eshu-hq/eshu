// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Begin gives the in-memory relationship fake the transaction the #7597
// activation wake needs. The wake's own statements have no in-memory model
// (the live tests own their behavior); every other statement delegates.
func (database *relationshipTestDB) Begin(context.Context) (db.Transaction, error) {
	return relationshipTestTx{database: database}, nil
}

// relationshipTestTx runs a transaction's statements against the fake.
type relationshipTestTx struct {
	database *relationshipTestDB
}

func (tx relationshipTestTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "set_config('lock_timeout'") || query == wakeProducerTerminalConsumersSQL {
		return proofResult{}, nil
	}
	return tx.database.ExecContext(ctx, query, args...)
}

func (tx relationshipTestTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if query == lockInflightOwnGateConsumersSQL {
		return newRelationshipRows(nil), nil
	}
	return tx.database.QueryContext(ctx, query, args...)
}

func (relationshipTestTx) Commit() error   { return nil }
func (relationshipTestTx) Rollback() error { return nil }
