// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"database/sql"
)

// fakeAWSCloudRuntimeDriftExecer is a package-local ExecContext double
// (issue #6061): it used to be the reducer root's shared
// fakeWorkloadIdentityExecer, extended in this file with
// BeginAWSCloudRuntimeDriftTx. Go cannot add a method to a type owned by
// another package, so once this family moved to [awscloud] the fake had to
// become a local type; the shape and behavior are unchanged from the root
// original.
type fakeAWSCloudRuntimeDriftExecer struct {
	execs []fakeAWSCloudRuntimeDriftExecCall
}

// fakeAWSCloudRuntimeDriftExecCall records one ExecContext invocation this
// fake captured.
type fakeAWSCloudRuntimeDriftExecCall struct {
	query string
	args  []any
}

func (f *fakeAWSCloudRuntimeDriftExecer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.execs = append(f.execs, fakeAWSCloudRuntimeDriftExecCall{query: query, args: args})
	return fakeAWSCloudRuntimeDriftResult{}, nil
}

type fakeAWSCloudRuntimeDriftResult struct{}

func (fakeAWSCloudRuntimeDriftResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeAWSCloudRuntimeDriftResult) RowsAffected() (int64, error) { return 1, nil }

// fakeAWSCloudRuntimeDriftTx adapts fakeAWSCloudRuntimeDriftExecer into
// AWSCloudRuntimeDriftTx so writer tests can drive the transactional writer
// (#5848: insert-admission check, versioned upsert, and
// generation-authoritative retire) over the SAME captured exec-call slice
// non-transactional tests already assert on. Commit/Rollback are recorded but
// otherwise no-ops; the fake has no real transactional isolation, only call
// sequencing.
type fakeAWSCloudRuntimeDriftTx struct {
	execer     *fakeAWSCloudRuntimeDriftExecer
	committed  bool
	rolledBack bool
}

// BeginAWSCloudRuntimeDriftTx lets *fakeAWSCloudRuntimeDriftExecer double as
// an AWSCloudRuntimeDriftBeginner for writer tests.
func (f *fakeAWSCloudRuntimeDriftExecer) BeginAWSCloudRuntimeDriftTx(context.Context) (AWSCloudRuntimeDriftTx, error) {
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
