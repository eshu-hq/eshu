// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// AWSCloudRuntimeDriftAdmissionBeginner adapts a postgres Beginner (for
// example the instrumented reducer DB) into the reducer's narrow AWS
// cloud-runtime-drift transaction surface (#5848), so the writer can commit
// its insert-admission check, versioned upsert, and generation-authoritative
// retire atomically over the shared instrumented connection. Mirrors
// ServiceMaterializationBeginner.
type AWSCloudRuntimeDriftAdmissionBeginner struct {
	Beginner Beginner
}

// BeginAWSCloudRuntimeDriftTx opens a transaction wrapped in the reducer's
// drift-writer surface.
func (b AWSCloudRuntimeDriftAdmissionBeginner) BeginAWSCloudRuntimeDriftTx(
	ctx context.Context,
) (reducer.AWSCloudRuntimeDriftTx, error) {
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return awsCloudRuntimeDriftAdmissionTx{tx: tx}, nil
}

type awsCloudRuntimeDriftAdmissionTx struct {
	tx Transaction
}

func (t awsCloudRuntimeDriftAdmissionTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	return t.tx.ExecContext(ctx, query, args...)
}

func (t awsCloudRuntimeDriftAdmissionTx) Commit() error   { return t.tx.Commit() }
func (t awsCloudRuntimeDriftAdmissionTx) Rollback() error { return t.tx.Rollback() }
