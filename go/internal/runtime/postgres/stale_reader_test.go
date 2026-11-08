// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// staleFixtureConn is a fake standby that answers the identity fence, the
// replay-age staleness probe, and one business row. replayAge is the age the
// probe reports; a negative age scans NULL (never replayed).
type staleFixtureConn struct {
	recovery   bool
	replayAge  time.Duration
	systemID   string
	businessOn *bool
}

func (c *staleFixtureConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (c *staleFixtureConn) Close() error                        { return nil }
func (c *staleFixtureConn) Begin() (driver.Tx, error)           { return queryIdentityTx{}, nil }
func (c *staleFixtureConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return queryIdentityTx{}, nil
}

func (c *staleFixtureConn) QueryContext(_ context.Context, statement string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(statement, "pg_control_system()"):
		return &queryIdentityRows{columns: []string{"read_only", "recovery", "system_id", "database", "server_version", "server_version_num"}, values: []driver.Value{"on", c.recovery, c.systemID, "eshu", "18.7", int64(180007)}}, nil
	case strings.Contains(statement, "pg_last_xact_replay_timestamp()"):
		if c.replayAge < 0 {
			return &queryIdentityRows{columns: []string{"in_recovery", "replay_age_seconds"}, values: []driver.Value{c.recovery, nil}}, nil
		}
		return &queryIdentityRows{columns: []string{"in_recovery", "replay_age_seconds"}, values: []driver.Value{c.recovery, c.replayAge.Seconds()}}, nil
	}
	if c.businessOn != nil {
		*c.businessOn = true
	}
	return &queryIdentityRows{columns: []string{"value"}, values: []driver.Value{int64(42)}}, nil
}

type staleFixtureConnector struct{ conn *staleFixtureConn }

func (c staleFixtureConnector) Connect(context.Context) (driver.Conn, error) {
	return c.conn, nil
}
func (staleFixtureConnector) Driver() driver.Driver { return terminalErrorDriver{} }

// newStaleFixtureAccess returns an Access over one fake standby with the
// stale-audits routing mode on. The context carries no writer checkpoint:
// the stale path must never require one.
func newStaleFixtureAccess(tb testing.TB, conn *staleFixtureConn, observer Observer) *Access {
	tb.Helper()
	pool := sql.OpenDB(staleFixtureConnector{conn: conn})
	tb.Cleanup(func() { _ = pool.Close() })
	permits := make(chan struct{}, 1)
	permits <- struct{}{}
	return &Access{
		reader: pool, readerPermits: permits, observer: observer, replayTimeout: time.Second,
		lineage:           newWriterLineage(physicalIdentity{systemID: "7", database: "eshu"}, lineageObservation{}, nil),
		readRouting:       ReadRoutingStaleAudits,
		staleReplayBudget: DefaultStaleReplayBudget,
		snapshotExport:    newSnapshotExportQualification(nil),
	}
}

func TestStaleReaderServesFreshStandbyWithoutCheckpoint(t *testing.T) {
	t.Parallel()
	access := newStaleFixtureAccess(t, &staleFixtureConn{recovery: true, replayAge: 2 * time.Second, systemID: "7"}, nil)
	rows, err := access.StaleReader().QueryContext(context.Background(), "SELECT 42")
	if err != nil {
		t.Fatalf("fresh standby read: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("fresh standby read returned no rows")
	}
	var value int
	if err := rows.Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != 42 {
		t.Fatalf("value = %d, want 42", value)
	}
}

func TestStaleReaderDefersBeyondReplayBudget(t *testing.T) {
	t.Parallel()
	var business bool
	observer := &staleDeferralRecorder{}
	access := newStaleFixtureAccess(t, &staleFixtureConn{recovery: true, replayAge: 61 * time.Second, systemID: "7", businessOn: &business}, observer)
	_, err := access.StaleReader().QueryContext(context.Background(), "SELECT 42")
	var deferral StaleDeferral
	if !errors.As(err, &deferral) {
		t.Fatalf("lagged standby error = %v, want a typed stale deferral", err)
	}
	if deferral.Family != StaleFamilyAudits || deferral.Reason != StaleDeferralBudgetExceeded {
		t.Fatalf("deferral = %+v, want family audits and budget-exceeded reason", deferral)
	}
	if business {
		t.Fatal("lagged standby ran business SQL before the deferral")
	}
	if observer.count(StaleFamilyAudits) != 1 {
		t.Fatalf("deferrals for audits = %d, want 1", observer.count(StaleFamilyAudits))
	}
	if errors.Is(err, db.ErrReaderStale) || errors.Is(err, ErrReaderUnavailable) {
		t.Fatalf("stale deferral must not classify as retryable reader failure: %v", err)
	}
}

func TestStaleReaderDefersWhenNotAStandby(t *testing.T) {
	t.Parallel()
	// A same-primary pool passes identity with recovery off, so the
	// staleness probe is what refuses it: the stale path serves standbys.
	access := newStaleFixtureAccess(t, &staleFixtureConn{recovery: false, replayAge: 0, systemID: "7"}, nil)
	access.samePrimary = true
	_, err := access.StaleReader().QueryContext(context.Background(), "SELECT 42")
	var deferral StaleDeferral
	if !errors.As(err, &deferral) {
		t.Fatalf("primary read error = %v, want a typed stale deferral", err)
	}
	if deferral.Reason != StaleDeferralNotStandby {
		t.Fatalf("deferral reason = %q, want not-standby", deferral.Reason)
	}
}

func TestStaleReaderDefersWhenNeverReplayed(t *testing.T) {
	t.Parallel()
	access := newStaleFixtureAccess(t, &staleFixtureConn{recovery: true, replayAge: -1, systemID: "7"}, nil)
	_, err := access.StaleReader().QueryContext(context.Background(), "SELECT 42")
	var deferral StaleDeferral
	if !errors.As(err, &deferral) {
		t.Fatalf("never-replayed error = %v, want a typed stale deferral", err)
	}
	if deferral.Reason != StaleDeferralNeverReplayed {
		t.Fatalf("deferral reason = %q, want never-replayed", deferral.Reason)
	}
}

func TestStaleReaderKeepsSplitBrainFencing(t *testing.T) {
	t.Parallel()
	access := newStaleFixtureAccess(t, &staleFixtureConn{recovery: true, replayAge: time.Second, systemID: "other"}, nil)
	_, err := access.StaleReader().QueryContext(context.Background(), "SELECT 42")
	if !errors.Is(err, ErrWrongTopology) {
		t.Fatalf("split-brain read error = %v, want topology refusal", err)
	}
}

func TestStaleReaderRefusesWhenRoutingOff(t *testing.T) {
	t.Parallel()
	observer := &staleDeferralRecorder{}
	access := &Access{readRouting: ReadRoutingOff, observer: observer, replayTimeout: time.Second}
	_, err := access.StaleReader().QueryContext(context.Background(), "SELECT 42")
	var deferral StaleDeferral
	if !errors.As(err, &deferral) {
		t.Fatalf("routing-off error = %v, want a typed stale deferral", err)
	}
	if deferral.Reason != StaleDeferralDisabled {
		t.Fatalf("deferral reason = %q, want disabled", deferral.Reason)
	}
	if observer.count(StaleFamilyAudits) != 1 {
		t.Fatalf("deferrals for audits = %d, want 1", observer.count(StaleFamilyAudits))
	}
}

func TestStaleReaderCarriesNoSnapshotSetSurface(t *testing.T) {
	t.Parallel()
	access := newStaleFixtureAccess(t, &staleFixtureConn{recovery: true, replayAge: time.Second, systemID: "7"}, nil)
	if setter, ok := access.StaleReader().(db.ReadSnapshotSetBeginner); ok {
		t.Fatalf("stale reader exposes snapshot sets: %T", setter)
	}
}

func TestStaleReaderSnapshotSurvivesCancellation(t *testing.T) {
	t.Parallel()
	access := newStaleFixtureAccess(t, &staleFixtureConn{recovery: true, replayAge: time.Second, systemID: "7"}, nil)
	tx, err := access.StaleReader().BeginReadOnlySnapshot(context.Background())
	if err != nil {
		t.Fatalf("stale snapshot begin: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("stale snapshot rollback: %v", err)
	}
}

func TestLoadConfigReadRoutingDefaultsOff(t *testing.T) {
	t.Parallel()
	cfg, err := LoadConfig(func(key string) string {
		if key == "ESHU_POSTGRES_DSN" {
			return "postgres://fixture:fixture@localhost:5432/eshu?sslmode=disable"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReadRouting != ReadRoutingOff {
		t.Fatalf("ReadRouting = %q, want off", cfg.ReadRouting)
	}
}

func TestLoadConfigReadRoutingAcceptsStaleAudits(t *testing.T) {
	t.Parallel()
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return "postgres://fixture:fixture@localhost:5432/eshu?sslmode=disable"
		case "ESHU_POSTGRES_READ_ROUTING":
			return "stale-audits"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReadRouting != ReadRoutingStaleAudits {
		t.Fatalf("ReadRouting = %q, want stale-audits", cfg.ReadRouting)
	}
}

func TestLoadConfigReadRoutingRejectsInvalid(t *testing.T) {
	t.Parallel()
	_, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return "postgres://fixture:fixture@localhost:5432/eshu?sslmode=disable"
		case "ESHU_POSTGRES_READ_ROUTING":
			return "sometimes"
		default:
			return ""
		}
	})
	if err == nil || !strings.Contains(err.Error(), "ESHU_POSTGRES_READ_ROUTING") {
		t.Fatalf("invalid routing error = %v, want a startup failure naming the variable", err)
	}
}

func TestStaleReaderDefersWithoutObserver(t *testing.T) {
	t.Parallel()
	access := &Access{readRouting: ReadRoutingOff, replayTimeout: time.Second}
	_, err := access.StaleReader().QueryContext(context.Background(), "SELECT 42")
	var deferral StaleDeferral
	if !errors.As(err, &deferral) {
		t.Fatalf("routing-off error = %v, want a typed stale deferral", err)
	}
}

func TestStaleDeferralErrorStaysFixed(t *testing.T) {
	t.Parallel()
	deferral := StaleDeferral{Family: StaleFamilyAudits, Reason: StaleDeferralBudgetExceeded, ReplayAge: 61 * time.Second, Budget: DefaultStaleReplayBudget}
	if got := deferral.Error(); got != "PostgreSQL stale reader deferred" {
		t.Fatalf("StaleDeferral.Error() = %q, want the fixed refusal message", got)
	}
}

// staleDeferralRecorder is a test Observer that counts stale deferrals by family.
type staleDeferralRecorder struct {
	seen map[StaleFamily]int
}

func (o *staleDeferralRecorder) Observe(string, Stage, Outcome, time.Duration) {}
func (o *staleDeferralRecorder) ObserveStaleDeferral(_ context.Context, family StaleFamily) {
	if o.seen == nil {
		o.seen = map[StaleFamily]int{}
	}
	o.seen[family]++
}

func (o *staleDeferralRecorder) count(family StaleFamily) int { return o.seen[family] }
