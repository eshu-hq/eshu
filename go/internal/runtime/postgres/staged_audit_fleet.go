// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// stagedAuditLegacyMemberID names the single-pool reader on topologies
// without a direct member fleet. Member affinity is trivial there: every
// borrow already lands on the same pool.
const stagedAuditLegacyMemberID = "reader"

// StagedAuditAccessFleet pins one qualified reader member per staged audit
// tick (#10451). PinMember qualifies members through the same staleness gate
// every statement borrow repeats (standby identity plus replay-age budget),
// and RunOnMember borrows only from the pinned member: a failed borrow aborts
// the tick through the runner's deferral path instead of hopping to another
// member mid-tick. Each statement is its own short transaction with a fresh
// standby snapshot, and the connection returns to the pool before the next
// statement runs.
type StagedAuditAccessFleet struct {
	access *Access
}

// NewStagedAuditAccessFleet builds the production fleet adapter over access.
// A nil access fails every operation closed; nothing panics.
func NewStagedAuditAccessFleet(access *Access) *StagedAuditAccessFleet {
	return &StagedAuditAccessFleet{access: access}
}

var _ StagedAuditFleet = (*StagedAuditAccessFleet)(nil)

// PinMember qualifies one reader member for the tick: the first member whose
// staleness gate passes. The qualifying borrow closes immediately; statements
// borrow fresh connections on the same member afterwards.
func (f *StagedAuditAccessFleet) PinMember(ctx context.Context) (string, error) {
	if f == nil || f.access == nil {
		return "", errors.New("staged audit fleet access is required")
	}
	access := f.access
	if len(access.readerMembers) == 0 {
		if access.reader == nil {
			return "", errors.New("staged audit fleet has no reader members")
		}
		conn, err := access.borrowStaleFrom(ctx, access.reader, nil, StaleFamilyAudits)
		if err != nil {
			return "", fmt.Errorf("qualify staged audit reader: %w", err)
		}
		_ = conn.Close()
		return stagedAuditLegacyMemberID, nil
	}
	var failures error
	for i := range access.readerMembers {
		member := &access.readerMembers[i]
		conn, err := access.borrowStaleFrom(ctx, member.pool, member, StaleFamilyAudits)
		if err != nil {
			failures = errors.Join(failures, fmt.Errorf("member %s: %w", member.id, err))
			continue
		}
		_ = conn.Close()
		return member.id, nil
	}
	return "", fmt.Errorf("qualify staged audit reader member: %w", failures)
}

// RunOnMember runs fn on one staleness-gated connection borrowed from the
// pinned member only. The borrowed connection backs the Queryer handed to fn
// and closes when fn returns, so no statement outlives its short transaction.
func (f *StagedAuditAccessFleet) RunOnMember(ctx context.Context, member string, fn func(ctx context.Context, reader db.Queryer) error) error {
	if f == nil || f.access == nil {
		return errors.New("staged audit fleet access is required")
	}
	pool, physical, err := f.resolve(member)
	if err != nil {
		return err
	}
	conn, err := f.access.borrowStaleFrom(ctx, pool, physical, StaleFamilyAudits)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	return fn(ctx, stagedAuditMemberQueryer{conn: conn.Conn})
}

// ReplayLag reports the pinned member's current replay age. The per-statement
// staleness gate already refused over-budget members, so a failure here means
// the member moved mid-tick and the runner aborts.
func (f *StagedAuditAccessFleet) ReplayLag(ctx context.Context, member string) (time.Duration, error) {
	var lag time.Duration
	err := f.RunOnMember(ctx, member, func(stmtCtx context.Context, reader db.Queryer) error {
		rows, err := reader.QueryContext(stmtCtx, stagedAuditReplayLagSQL)
		if err != nil {
			return fmt.Errorf("read staged audit replay lag: %w", err)
		}
		defer func() { _ = rows.Close() }()
		var ageSeconds sql.NullFloat64
		if !rows.Next() {
			return errors.New("staged audit replay lag returned no rows")
		}
		if err := rows.Scan(&ageSeconds); err != nil {
			return fmt.Errorf("scan staged audit replay lag: %w", err)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read staged audit replay lag: %w", err)
		}
		if !ageSeconds.Valid {
			return errors.New("staged audit member never replayed")
		}
		lag = time.Duration(ageSeconds.Float64 * float64(time.Second))
		return nil
	})
	if err != nil {
		return 0, err
	}
	return lag, nil
}

// stagedAuditReplayLagSQL reads only the standby's replay age. Recovery
// itself was already gated by the borrow; a NULL age aborts the tick.
const stagedAuditReplayLagSQL = `SELECT EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp()))::float8`

// resolve maps a pinned member id back to its pool. Unknown ids abort; the
// runner never invents a member mid-tick.
func (f *StagedAuditAccessFleet) resolve(member string) (*sql.DB, *physicalReaderMember, error) {
	access := f.access
	if len(access.readerMembers) == 0 {
		if member != stagedAuditLegacyMemberID {
			return nil, nil, fmt.Errorf("unknown staged audit member %q", member)
		}
		if access.reader == nil {
			return nil, nil, errors.New("staged audit fleet has no reader pool")
		}
		return access.reader, nil, nil
	}
	for i := range access.readerMembers {
		if access.readerMembers[i].id == member {
			physical := &access.readerMembers[i]
			return physical.pool, physical, nil
		}
	}
	return nil, nil, fmt.Errorf("unknown staged audit member %q", member)
}

// stagedAuditMemberQueryer serves one statement on a borrowed member
// connection. *sql.Rows satisfies db.Rows, so no cursor wrapper is needed.
type stagedAuditMemberQueryer struct {
	conn *sql.Conn
}

func (q stagedAuditMemberQueryer) QueryContext(ctx context.Context, statement string, args ...any) (db.Rows, error) {
	return q.conn.QueryContext(ctx, statement, args...)
}
