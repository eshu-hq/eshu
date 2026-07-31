// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
)

// fakeAWSCloudRuntimeDriftTx adapts fakeWorkloadIdentityExecer into
// AWSCloudRuntimeDriftTx so writer tests can drive the transactional writer
// (#5848: insert-admission check, versioned upsert, and
// generation-authoritative retire) over the SAME captured exec-call slice
// non-transactional tests already assert on. Commit/Rollback are recorded but
// otherwise no-ops; the fake has no real transactional isolation, only call
// sequencing.
type fakeAWSCloudRuntimeDriftTx struct {
	execer     *fakeWorkloadIdentityExecer
	committed  bool
	rolledBack bool
}

// BeginAWSCloudRuntimeDriftTx lets *fakeWorkloadIdentityExecer double as an
// AWSCloudRuntimeDriftBeginner for writer tests.
func (f *fakeWorkloadIdentityExecer) BeginAWSCloudRuntimeDriftTx(context.Context) (AWSCloudRuntimeDriftTx, error) {
	return &fakeAWSCloudRuntimeDriftTx{execer: f}, nil
}

func (tx *fakeAWSCloudRuntimeDriftTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	return tx.execer.ExecContext(ctx, query, args...)
}

func (tx *fakeAWSCloudRuntimeDriftTx) Commit() error {
	tx.committed = true
	return nil
}

func (tx *fakeAWSCloudRuntimeDriftTx) Rollback() error {
	tx.rolledBack = true
	return nil
}
