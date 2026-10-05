// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type checkpoint struct {
	owner       *Access
	lsn         string
	systemID    string
	database    string
	incarnation string
}
type checkpointKey struct{}

var (
	// ErrMissingCheckpoint means business SQL was refused before borrowing a reader.
	ErrMissingCheckpoint = errors.New("PostgreSQL reader requires writer checkpoint")
	// ErrWrongTopology means a role, database, system identity, or primary
	// history differs. It is the shared db.ErrWrongTopology so the query layer and
	// the identity resolver can classify it without importing this package.
	ErrWrongTopology = db.ErrWrongTopology
	// ErrReaderStale means replay did not reach the writer checkpoint in time.
	// It is the shared db.ErrReaderStale so the query layer can classify it
	// without importing this package.
	ErrReaderStale = db.ErrReaderStale
	// ErrReaderUnavailable means reader acquisition, identity, or replay failed.
	// It is the shared db.ErrReaderUnavailable.
	ErrReaderUnavailable = db.ErrReaderUnavailable
	// ErrWriterUnavailable means the writer checkpoint could not be obtained.
	ErrWriterUnavailable = errors.New("PostgreSQL writer unavailable")
)

// ContextWithCheckpoint captures one acknowledged writer insertion point after
// caller authorization. The returned context carries an immutable private
// checkpoint bound to this Access. The writer connection is released first.
func (a *Access) ContextWithCheckpoint(ctx context.Context) (context.Context, error) {
	started := time.Now()
	checkpointCtx, cancel := context.WithTimeout(ctx, a.replayTimeout)
	defer cancel()
	var point checkpoint
	var recovery bool
	var readOnly string
	var defaultReadOnly string
	err := a.writer.QueryRowContext(checkpointCtx, `SELECT pg_current_wal_insert_lsn()::text, system_identifier::text, current_database(), pg_is_in_recovery(), current_setting('transaction_read_only'), current_setting('default_transaction_read_only'), (extract(epoch from pg_postmaster_start_time())*1000000)::bigint::text FROM pg_control_system()`).Scan(&point.lsn, &point.systemID, &point.database, &recovery, &readOnly, &defaultReadOnly, &point.incarnation)
	if err != nil {
		a.observe(ctx, "writer", StageWriterCheckpoint, started, err)
		return nil, privateFailure(failureWriterCheckpoint, errors.Join(ErrWriterUnavailable, err))
	}
	if recovery || readOnly != "off" || defaultReadOnly != "off" || point.lsn == "" || point.systemID != a.identity.systemID || point.database != a.identity.database || point.incarnation != a.identity.incarnation {
		err = ErrWrongTopology
		a.observe(ctx, "writer", StageWriterCheckpoint, started, err)
		return nil, privateFailure(failureWriterCheckpoint, err)
	}
	point.owner = a
	a.observe(ctx, "writer", StageWriterCheckpoint, started, nil)
	return context.WithValue(ctx, checkpointKey{}, point), nil
}
