// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"errors"
)

type containerImageIdentityRetireBeginner struct {
	tx    ContainerImageIdentityTransaction
	calls int
}

type containerImageIdentityRetireCutoverLookup struct {
	exists bool
	err    error
	calls  int
}

type containerImageIdentityRetireCleanupLookup struct {
	complete bool
	err      error
	calls    int
}

func (l *containerImageIdentityRetireCleanupLookup) ContainerImageIdentityLegacyCleanupComplete(
	context.Context,
	string,
	string,
) (bool, error) {
	l.calls++
	return l.complete, l.err
}

func (l *containerImageIdentityRetireCutoverLookup) ContainerImageIdentityCutoverExists(
	context.Context,
	string,
	string,
) (bool, error) {
	l.calls++
	return l.exists, l.err
}

func (b *containerImageIdentityRetireBeginner) BeginContainerImageIdentityTx(
	context.Context,
) (ContainerImageIdentityTransaction, error) {
	b.calls++
	return b.tx, nil
}

type containerImageIdentityRetireOutsideDB struct{}

func (*containerImageIdentityRetireOutsideDB) ExecContext(
	context.Context,
	string,
	...any,
) (sql.Result, error) {
	return nil, errors.New("write escaped container image identity transaction")
}

type containerImageIdentityRetireDirectDB struct {
	queries    []string
	args       [][]any
	retired    int64
	claimValid bool
}

func (db *containerImageIdentityRetireDirectDB) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	db.queries = append(db.queries, query)
	db.args = append(db.args, append([]any(nil), args...))
	return containerImageIdentityRetireResult(db.retired), nil
}

func (db *containerImageIdentityRetireDirectDB) ExecContainerImageIdentityClaimed(
	_ context.Context,
	query string,
	args ...any,
) (int, bool, error) {
	db.queries = append(db.queries, query)
	db.args = append(db.args, append([]any(nil), args...))
	return int(db.retired), db.claimValid, nil
}

// ExecContainerImageIdentityClaimedAdmission is the method the writer's
// completed-cutover single-round-trip path actually calls (#5874). This fake
// predates the admission CAS and its owning tests exercise the claim/legacy
// cleanup control flow, not admission itself, so admitted mirrors claimValid
// (matching the previous always-succeeds-when-claim-valid behavior).
func (db *containerImageIdentityRetireDirectDB) ExecContainerImageIdentityClaimedAdmission(
	_ context.Context,
	query string,
	args ...any,
) (int, bool, bool, error) {
	db.queries = append(db.queries, query)
	db.args = append(db.args, append([]any(nil), args...))
	return int(db.retired), db.claimValid, db.claimValid, nil
}

type containerImageIdentityRetireTx struct {
	queries    []string
	args       [][]any
	failQuery  string
	retired    int64
	claimValid bool
	committed  bool
	rolledBack bool
}

func (tx *containerImageIdentityRetireTx) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	tx.queries = append(tx.queries, query)
	tx.args = append(tx.args, append([]any(nil), args...))
	if query == tx.failQuery {
		return nil, errors.New("synthetic transaction failure")
	}
	if query == containerImageIdentityPublishAndLegacyCleanupQuery {
		return containerImageIdentityRetireResult(tx.retired), nil
	}
	if query == containerImageIdentityAdmissionQuery {
		// The admission CAS (#5874) is the FIRST statement this fake's owning
		// tests now see; they predate admission and exercise the surrounding
		// cutover/legacy-cleanup control flow, not admission itself, so it
		// always reports one row affected (admitted) unless the test wired
		// this exact query as tx.failQuery above.
		return containerImageIdentityRetireResult(1), nil
	}
	return containerImageIdentityRetireResult(0), nil
}

func (tx *containerImageIdentityRetireTx) ExecContainerImageIdentityClaimed(
	_ context.Context,
	query string,
	args ...any,
) (int, bool, error) {
	tx.queries = append(tx.queries, query)
	tx.args = append(tx.args, append([]any(nil), args...))
	if query == tx.failQuery {
		return 0, false, errors.New("synthetic transaction failure")
	}
	return int(tx.retired), tx.claimValid, nil
}

// ExecContainerImageIdentityClaimedAdmission satisfies
// ContainerImageIdentityClaimedExecer's admission-aware method (#5874) so
// tests routing cutoverComplete through the transactional path (the
// oversized-batch shape, not the single-round-trip fast path) still succeed
// the tx.(ContainerImageIdentityClaimedExecer) assertion in
// writeContainerImageIdentityRows. This fake's owning tests exercise the
// claim/legacy cleanup control flow, not admission itself, so admitted
// mirrors claimValid.
func (tx *containerImageIdentityRetireTx) ExecContainerImageIdentityClaimedAdmission(
	_ context.Context,
	query string,
	args ...any,
) (int, bool, bool, error) {
	tx.queries = append(tx.queries, query)
	tx.args = append(tx.args, append([]any(nil), args...))
	if query == tx.failQuery {
		return 0, false, false, errors.New("synthetic transaction failure")
	}
	return int(tx.retired), tx.claimValid, tx.claimValid, nil
}

func (tx *containerImageIdentityRetireTx) Commit() error {
	tx.committed = true
	return nil
}

func (tx *containerImageIdentityRetireTx) Rollback() error {
	tx.rolledBack = true
	return nil
}

type containerImageIdentityRetireResult int64

func (r containerImageIdentityRetireResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r containerImageIdentityRetireResult) RowsAffected() (int64, error) {
	return int64(r), nil
}

func equalRetireIDArgument(got any, want []string) bool {
	gotStrings, ok := got.([]string)
	if !ok || len(gotStrings) != len(want) {
		return false
	}
	for index := range gotStrings {
		if gotStrings[index] != want[index] {
			return false
		}
	}
	return true
}
