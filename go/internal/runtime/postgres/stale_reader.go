// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// ReadRouting selects the reader routing mode for one process. Off keeps
// every read on the writer-backed path and the stale reader defers; the
// stale-audits mode admits the unfenced stale reader for audit families
// within the replay-age budget. Later slices extend the modes as the query
// family census lands.
type ReadRouting string

// Reader routing modes. The zero value disables the stale path, matching off.
const (
	ReadRoutingOff         ReadRouting = "off"
	ReadRoutingStaleAudits ReadRouting = "stale-audits"
	// ReadRoutingStagedAudits admits the staged standby scan (#10451): the
	// same standby fleet serves per-scope batches instead of the
	// single-statement split scan, for corpora whose full fetch outlives the
	// recovery-conflict window.
	ReadRoutingStagedAudits ReadRouting = "staged-audits"
)

// StaleFamily names the query family a stale read serves. The budget stays a
// process default until per-family overrides land; the family already rides
// the deferral telemetry so overrides can split it later.
type StaleFamily string

// Stale families served without a writer checkpoint.
const (
	StaleFamilyAudits StaleFamily = "audits"
)

// DefaultStaleReplayBudget caps how old a standby's replay timestamp may be
// before the stale reader defers. Audits tolerate minutes-old data; the
// default stays tight until replica lag is measured under load (#10416).
const DefaultStaleReplayBudget = 30 * time.Second

// StaleDeferralReason names why a stale read refused to serve. It is a closed
// set with no endpoint, SQL, or credential detail.
type StaleDeferralReason string

// Stale deferral reasons.
const (
	// StaleDeferralDisabled means the process routes everything writer-backed.
	StaleDeferralDisabled StaleDeferralReason = "disabled"
	// StaleDeferralNotStandby means the borrowed connection is not in recovery.
	StaleDeferralNotStandby StaleDeferralReason = "not_standby"
	// StaleDeferralNeverReplayed means the standby never replayed a transaction.
	StaleDeferralNeverReplayed StaleDeferralReason = "never_replayed"
	// StaleDeferralBudgetExceeded means the replay age passed the budget.
	StaleDeferralBudgetExceeded StaleDeferralReason = "budget_exceeded"
)

// StaleDeferral reports a stale-reader refusal. Callers must defer the work,
// never fall back to the writer: the stale path exists to shed writer load,
// and a silent writer fallback would return that load under pressure. It
// deliberately matches neither db.ErrReaderStale nor ErrReaderUnavailable so
// the query layer cannot misclassify it as a retryable fenced-read failure.
type StaleDeferral struct {
	Family    StaleFamily
	Reason    StaleDeferralReason
	ReplayAge time.Duration
	Budget    time.Duration
}

// Error exposes only the fixed refusal stage. The family, age, and budget
// travel through errors.As for logs, never as metric labels or wire text.
func (e StaleDeferral) Error() string { return "PostgreSQL stale reader deferred" }

// StaleReadDeferred marks a refused stale read for errors.As matching through
// a minimal interface, so a storage caller can recognize the deferral without
// importing this package: any error whose chain carries a StaleDeferral
// answers true, including through the private failure wrapper. It is the
// slice-1 seam the winners-audit split scan matches on; the deferral itself
// and its reasons are unchanged.
func (e StaleDeferral) StaleReadDeferred() bool { return true }

var _ db.ReadStore = staleReadStore{}

// staleReadStore serves guarded reads from the same qualified standby fleet
// as the fenced reader, without a writer checkpoint. Every borrow repeats
// the identity and split-brain qualification, then gates on the standby's
// own replay age. It carries no snapshot-set surface: pinning one snapshot
// across connections is a writer-fenced coordination this path never takes,
// so sets stay on the fenced reader.
type staleReadStore struct {
	access *Access
	family StaleFamily
}

// StaleReader returns guarded row and snapshot reads over the same qualified
// standby fleet as Reader, with no writer checkpoint. Each borrow checks the
// standby itself, pg_is_in_recovery() plus pg_last_xact_replay_timestamp()
// age against the replay budget, with zero writer contact. A standby past the
// budget answers a typed StaleDeferral for refuse-and-defer: the caller
// defers the work instead of retrying or falling back to the writer.
func (a *Access) StaleReader() db.ReadStore {
	return staleReadStore{access: a, family: StaleFamilyAudits}
}

// QueryContext runs one stale read and defers its errors to rows.
func (s staleReadStore) QueryContext(ctx context.Context, statement string, args ...any) (db.Rows, error) {
	a := s.access
	conn, err := a.borrowStale(ctx, s.family)
	if err != nil {
		return nil, err
	}
	a.startReaderQuery(ctx, conn.Conn)
	started := time.Now()
	rows, err := conn.QueryContext(ctx, statement, args...)
	a.observe(ctx, "reader", StageBusinessQuery, started, err)
	if err != nil {
		_ = conn.Close()
		return nil, privateFailure(failureReaderQuery, err)
	}
	return newOwnedRows(ctx, rows, conn), nil
}

// QueryRowContext runs one stale read and defers its errors to Scan.
func (s staleReadStore) QueryRowContext(ctx context.Context, statement string, args ...any) db.Row {
	rows, err := s.QueryContext(ctx, statement, args...)
	return &fencedRow{rows: rows, err: err}
}

// BeginReadOnlySnapshot opens a read-only repeatable-read transaction on one
// staleness-gated connection. The transaction owns the connection thereafter.
func (s staleReadStore) BeginReadOnlySnapshot(ctx context.Context) (db.ReadTransaction, error) {
	a := s.access
	conn, err := a.borrowStale(ctx, s.family)
	if err != nil {
		return nil, err
	}
	owned, beginErr := beginReadTransaction(ctx, conn, a)
	if beginErr != nil {
		_ = conn.Close()
		return nil, privateFailure(failureSnapshotBegin, beginErr)
	}
	return owned, nil
}

// staleReadsEnabled reports whether the process admits unfenced stale reads.
func (a *Access) staleReadsEnabled() bool { return a.readRouting == ReadRoutingStaleAudits }

// staleBudget returns the configured replay-age budget or the default.
func (a *Access) staleBudget() time.Duration {
	if a.staleReplayBudget <= 0 {
		return DefaultStaleReplayBudget
	}
	return a.staleReplayBudget
}

// borrowStale borrows one staleness-gated connection from the same qualified
// fleet the fenced reader uses. Routing off refuses before touching any pool.
func (a *Access) borrowStale(ctx context.Context, family StaleFamily) (*readerConnection, error) {
	if !a.staleReadsEnabled() {
		a.observeStaleDeferral(ctx, family)
		return nil, privateFailure(failureReaderStale, StaleDeferral{Family: family, Reason: StaleDeferralDisabled, Budget: a.staleBudget()})
	}
	if len(a.readerMembers) > 0 {
		return a.borrowStaleFleet(ctx, family)
	}
	return a.borrowStaleFrom(ctx, a.reader, nil, family)
}

func (a *Access) borrowStaleFrom(ctx context.Context, pool *sql.DB, member *physicalReaderMember, family StaleFamily) (*readerConnection, error) {
	fenceCtx, cancel := context.WithTimeout(ctx, a.replayTimeout)
	defer cancel()
	borrowed := time.Now()
	if a.readerPermits != nil {
		select {
		case <-a.readerPermits:
		case <-fenceCtx.Done():
			borrowErr := fenceCtx.Err()
			if ctx.Err() == nil && errors.Is(borrowErr, context.DeadlineExceeded) {
				borrowErr = errors.Join(errReaderPermitTimeout, borrowErr)
			}
			a.observe(ctx, "reader", StageReaderBorrow, borrowed, borrowErr)
			return nil, privateFailure(failureReaderBorrow, errors.Join(ErrReaderUnavailable, borrowErr))
		}
	}
	conn, err := pool.Conn(fenceCtx)
	a.observe(ctx, "reader", StageReaderBorrow, borrowed, err)
	if err != nil {
		if a.readerPermits != nil {
			a.readerPermits <- struct{}{}
		}
		return nil, privateFailure(failureReaderBorrow, errors.Join(ErrReaderUnavailable, err))
	}
	if err := a.checkStale(fenceCtx, conn, member, family); err != nil {
		if errors.Is(err, ErrWrongTopology) {
			// This physical connection failed its borrowed-session identity
			// check. Keep it out of the pool like the fenced path does.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
		if a.readerPermits != nil {
			a.readerPermits <- struct{}{}
		}
		return nil, err
	}
	return &readerConnection{Conn: conn, permits: a.readerPermits, member: member}, nil
}

// staleReplayAgeSQL reads the standby's own recovery state and replay age in
// one round trip. A NULL age means the standby never replayed.
const staleReplayAgeSQL = `SELECT pg_is_in_recovery(), EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp()))::float8`

// checkStale fences one borrowed connection exactly like the writer-fenced
// reader, then gates on the standby's own replay age instead of a writer
// checkpoint. There is zero writer contact on this path.
func (a *Access) checkStale(ctx context.Context, conn *sql.Conn, member *physicalReaderMember, family StaleFamily) error {
	_, err := a.fenceReaderIdentity(ctx, conn, member, nil)
	if err != nil {
		return err
	}
	started := time.Now()
	var recovery bool
	var ageSeconds sql.NullFloat64
	probeErr := conn.QueryRowContext(ctx, staleReplayAgeSQL).Scan(&recovery, &ageSeconds)
	a.observe(ctx, "reader", StageReaderStaleness, started, probeErr)
	if probeErr != nil {
		if ctx.Err() != nil {
			return replayContextError(ctx.Err())
		}
		return privateFailure(failureReaderReplay, errors.Join(ErrReaderUnavailable, probeErr))
	}
	budget := a.staleBudget()
	deferral := StaleDeferral{Family: family, Budget: budget}
	switch {
	case !recovery:
		deferral.Reason = StaleDeferralNotStandby
	case !ageSeconds.Valid:
		deferral.Reason = StaleDeferralNeverReplayed
	default:
		deferral.ReplayAge = time.Duration(ageSeconds.Float64 * float64(time.Second))
		if deferral.ReplayAge > budget {
			deferral.Reason = StaleDeferralBudgetExceeded
		}
	}
	if deferral.Reason == "" {
		return nil
	}
	a.observeStaleDeferral(ctx, family)
	return privateFailure(failureReaderStale, deferral)
}

// borrowStaleFleet borrows one staleness-gated connection from the direct
// member fleet, retrying across members exactly like the fenced fleet path
// but without a writer checkpoint.
func (a *Access) borrowStaleFleet(ctx context.Context, family StaleFamily) (*readerConnection, error) {
	conn, err := runStaleFleet(a, ctx, func(tryCtx context.Context, reservation *readerReservation) (*readerConnection, error) {
		return a.borrowStaleReserved(tryCtx, reservation, family)
	})
	if err != nil {
		return nil, privateFailure(failureReaderBorrow, err)
	}
	return conn, nil
}

func (a *Access) borrowStaleReserved(ctx context.Context, reservation *readerReservation, family StaleFamily) (*readerConnection, error) {
	member := &a.readerMembers[reservation.member]
	borrowed := time.Now()
	conn, err := member.pool.Conn(ctx)
	a.observe(ctx, "reader", StageReaderBorrow, borrowed, err)
	if err != nil {
		reservation.ReleaseOne()
		return nil, privateFailure(failureReaderBorrow, errors.Join(ErrReaderUnavailable, err))
	}
	if err := a.checkStale(ctx, conn, member, family); err != nil {
		if errors.Is(err, ErrWrongTopology) {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
		reservation.ReleaseOne()
		return nil, err
	}
	return &readerConnection{Conn: conn, reservation: reservation, member: member}, nil
}

// runStaleFleet reserves one staleness-gated operation on a member, retrying
// across members before returning a connection to the caller. It mirrors the
// fenced runFleet loop for single borrows; stale snapshot sets refuse above,
// so count stays one and no exporter qualification filtering applies.
func runStaleFleet(access *Access, ctx context.Context, attempt func(context.Context, *readerReservation) (*readerConnection, error)) (*readerConnection, error) {
	bounded, cancel := context.WithTimeout(ctx, access.replayTimeout)
	defer cancel()
	order := access.memberOrder(1)
	excluded := make(map[int]bool)
	tried := make(map[int]bool)
	var failures error
	for bounded.Err() == nil {
		eligible := make([]int, 0, len(order))
		for _, member := range order {
			if !excluded[member] && !tried[member] {
				eligible = append(eligible, member)
			}
		}
		if len(eligible) == 0 {
			if len(excluded) == len(order) {
				break
			}
			clear(tried)
			select {
			case <-bounded.Done():
			case <-time.After(20 * time.Millisecond):
			}
			continue
		}
		borrowed := time.Now()
		reservation, reserveErr := access.allocator.reserve(bounded, eligible, 1)
		access.observe(ctx, "reader", StageReaderBorrow, borrowed, reserveErr)
		if reserveErr != nil {
			failures = errors.Join(failures, reserveErr)
			break
		}
		if bounded.Err() != nil {
			reservation.Release()
			break
		}
		tryCtx, stop := fleetAttemptContext(bounded, len(eligible))
		if tryCtx.Err() != nil {
			failures = errors.Join(failures, ErrReaderUnavailable, tryCtx.Err())
			stop()
			reservation.Release()
			tried[reservation.member] = true
			continue
		}
		result, attemptErr := attempt(tryCtx, reservation)
		stop()
		access.observeMemberAttempt(access.readerMembers[reservation.member].ordinal, attemptErr)
		if attemptErr == nil {
			return result, nil
		}
		reservation.Release()
		failures = errors.Join(failures, attemptErr)
		var deferral StaleDeferral
		if errors.As(attemptErr, &deferral) {
			// One lagged member must not fail the tick while a fresh
			// sibling may serve: a deferral is member-local, so the
			// fleet tries the next member. When every member defers,
			// the joined failures below still unwrap to a typed
			// StaleDeferral, and the caller defers with no writer
			// contact and no writer fallback.
			tried[reservation.member] = true
			continue
		}
		if sharedFleetFailure(bounded, attemptErr) {
			return nil, attemptErr
		}
		var local memberLocalTopology
		if errors.As(attemptErr, &local) {
			excluded[reservation.member] = true
			continue
		}
		if !retryableFleetFailure(bounded, attemptErr) {
			return nil, attemptErr
		}
		tried[reservation.member] = true
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if failures == nil {
		failures = ErrReaderUnavailable
	}
	if errors.Is(bounded.Err(), context.DeadlineExceeded) {
		failures = errors.Join(failures, ErrReaderUnavailable, bounded.Err())
	}
	return nil, failures
}

// staleDeferralObserver is the optional Observer extension that counts stale
// deferrals. Access prefers it through this interface so tests and future
// observers can record deferrals without the OTEL SDK.
type staleDeferralObserver interface {
	ObserveStaleDeferral(context.Context, StaleFamily)
}

// observeStaleDeferral counts one refused stale read by family. A missing
// accumulator and observer cost one interface assertion.
func (a *Access) observeStaleDeferral(ctx context.Context, family StaleFamily) {
	if a.observer == nil {
		return
	}
	observer, ok := a.observer.(staleDeferralObserver)
	if !ok {
		return
	}
	observer.ObserveStaleDeferral(ctx, family)
}
