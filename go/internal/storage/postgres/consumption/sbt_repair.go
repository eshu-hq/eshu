// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package consumption

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const (
	// SBTRepairMarker names the marker row that records a finished sbt key
	// repair (#8961). Binaries older than the repair never read it.
	SBTRepairMarker = "package_manifest_consumption_keys_sbt_repair_v1"
	// DefaultSBTRepairScopesPerPass bounds the scopes one repair pass walks.
	DefaultSBTRepairScopesPerPass = 100

	sbtRepairInsertedSuffix = ":inserted"
)

// ScopeLister returns up to limit scope IDs greater than after, in scope_id
// order, that the repair must visit.
type ScopeLister func(ctx context.Context, queryer db.Queryer, after string, limit int) ([]string, error)

// KeyInserter inserts the sidecar keys derived from envelopes inside tx and
// returns how many rows it inserted. It must never delete a key or overwrite
// an existing one: the repair only adds keys that are absent.
type KeyInserter func(ctx context.Context, tx db.ExecQueryer, envelopes []facts.Envelope) (int64, error)

// SBTRepair gives stored sbt dependency rows the registry keys the code before
// #8961 never wrote for them (#8961). It is additive: it adds missing keys one
// fact page at a time, holds the scope row FOR NO KEY UPDATE for that page
// only, and never deletes, never sets the sidecar-writer setting, and never
// writes a dirty row, so reader readiness cannot flip.
//
// The repair walks the scopes that hold sbt rows in scope_id order, keeping a
// cursor in the backfill progress table. A walk that inserted any key restarts
// from the beginning; the marker is written only after a complete walk that
// inserted nothing, which also catches keys a pre-upgrade writer dropped
// before the marker. Beginner, ListScopes, LoadPage, Insert, and PageSize are
// required.
type SBTRepair struct {
	Beginner      db.Beginner
	ListScopes    ScopeLister
	LoadPage      PageLoader
	Insert        KeyInserter
	PageSize      int
	ScopesPerPass int
	LockTimeout   time.Duration
}

// SBTRepairReport is what one repair pass did, for the pass log line.
type SBTRepairReport struct {
	// Pending is true while the repair has more to do, including a walk that
	// inserted rows and so restarts.
	Pending bool
	// Completed is true when this pass wrote the marker.
	Completed bool
	// Restarted is true when this pass finished a walk that inserted rows.
	Restarted bool
	// Cursor is the last scope the repair finished, empty before the first.
	Cursor string
	// Scopes, Pages, and Facts count the work of this pass.
	Scopes int
	Pages  int
	Facts  int
	// InsertedRows is the number of keys this pass inserted; WalkInsertedRows
	// is the running total for the current walk.
	InsertedRows     int64
	WalkInsertedRows int64
	// MaxTransaction and MaxLockWait are the slowest page transaction and the
	// longest scope-row lock wait of this pass.
	MaxTransaction time.Duration
	MaxLockWait    time.Duration
	Duration       time.Duration
}

// Pass runs one bounded repair pass. It is a no-op once the marker exists. An
// error leaves the cursor at the last finished scope; pages already committed
// stay, so the next pass resumes without losing work.
func (r SBTRepair) Pass(ctx context.Context, database db.ExecQueryer) (SBTRepairReport, error) {
	started := time.Now()
	report, err := r.pass(ctx, database)
	report.Duration = time.Since(started)
	return report, err
}

func (r SBTRepair) pass(ctx context.Context, database db.ExecQueryer) (SBTRepairReport, error) {
	var report SBTRepairReport
	if err := r.validate(); err != nil {
		return report, err
	}
	done, err := queryBool(ctx, database, sbtRepairMarkerPresentSQL, SBTRepairMarker)
	if err != nil {
		return report, fmt.Errorf("check sbt key repair marker: %w", err)
	}
	if done {
		return report, nil
	}
	report.Pending = true
	cursor, err := loadProgress(ctx, database, SBTRepairMarker)
	if err != nil {
		return report, err
	}
	report.Cursor = cursor
	scopes, err := r.ListScopes(ctx, database, cursor, r.scopesPerPass())
	if err != nil {
		return report, fmt.Errorf("list sbt key repair scopes: %w", err)
	}
	for _, scopeID := range scopes {
		if err := r.repairScope(ctx, scopeID, &report); err != nil {
			return report, err
		}
		report.Cursor = scopeID
		report.Scopes++
	}
	if len(scopes) == r.scopesPerPass() {
		err := r.loadWalkTotal(ctx, database, &report)
		return report, err
	}
	err = r.finishWalk(ctx, database, &report)
	return report, err
}

func (r SBTRepair) validate() error {
	if r.Beginner == nil || r.ListScopes == nil || r.LoadPage == nil || r.Insert == nil || r.PageSize <= 0 {
		return errors.New("sbt key repair requires a beginner, scope lister, page loader, key inserter, and positive page size")
	}
	return nil
}

func (r SBTRepair) scopesPerPass() int {
	if r.ScopesPerPass > 0 {
		return r.ScopesPerPass
	}
	return DefaultSBTRepairScopesPerPass
}

func (r SBTRepair) lockTimeout() time.Duration {
	if r.LockTimeout > 0 {
		return r.LockTimeout
	}
	return DefaultLockTimeout
}

func (r SBTRepair) loadWalkTotal(ctx context.Context, database db.Queryer, report *SBTRepairReport) error {
	total, err := loadWalkInserted(ctx, database)
	report.WalkInsertedRows = total
	return err
}

// finishWalk ends a walk that listed fewer scopes than the pass limit. A walk
// that inserted nothing writes the marker; a walk that inserted any key resets
// the cursor and the running total, so the next pass verifies the whole set
// again.
func (r SBTRepair) finishWalk(ctx context.Context, database db.ExecQueryer, report *SBTRepairReport) error {
	if err := r.loadWalkTotal(ctx, database, report); err != nil {
		return err
	}
	now := time.Now().UTC()
	if report.WalkInsertedRows == 0 {
		if _, err := database.ExecContext(ctx, markSBTRepairSQL, SBTRepairMarker, now); err != nil {
			return fmt.Errorf("mark sbt key repair complete: %w", err)
		}
		report.Pending, report.Completed = false, true
		return nil
	}
	for _, reset := range []struct{ name, value string }{
		{SBTRepairMarker, ""},
		{SBTRepairMarker + sbtRepairInsertedSuffix, "0"},
	} {
		if _, err := database.ExecContext(ctx, saveProgressSQL, reset.name, reset.value, now); err != nil {
			return fmt.Errorf("restart sbt key repair walk: %w", err)
		}
	}
	report.Restarted = true
	report.Cursor = ""
	return nil
}

// repairScope repairs one scope a page at a time. Each page is its own
// transaction, so no transaction holds the scope row across two pages.
func (r SBTRepair) repairScope(ctx context.Context, scopeID string, report *SBTRepairReport) error {
	after := ""
	for {
		last, done, err := r.repairPage(ctx, scopeID, after, report)
		if err != nil || done {
			return err
		}
		after = last
	}
}

// repairPage inserts the missing keys for one page of the scope's sbt facts.
// The scope row lock is what keeps the repair from inserting a key from a
// payload the ingester is rewriting: the ingester takes the same row, deletes
// by fact_id, and reinserts. A lock timeout returns an error and the cursor
// does not advance.
func (r SBTRepair) repairPage(ctx context.Context, scopeID, after string, report *SBTRepairReport) (last string, done bool, err error) {
	began := time.Now()
	tx, err := r.Beginner.Begin(ctx)
	if err != nil {
		return "", false, fmt.Errorf("begin sbt key repair transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, sbtRepairBeginSQL, lockTimeoutSetting(r.lockTimeout())); err != nil {
		return "", false, fmt.Errorf("configure sbt key repair transaction: %w", err)
	}
	lockStarted := time.Now()
	locked, err := queryExists(ctx, tx, lockScopeSQL, scopeID)
	if wait := time.Since(lockStarted); wait > report.MaxLockWait {
		report.MaxLockWait = wait
	}
	if err != nil {
		return "", false, fmt.Errorf("lock sbt key repair scope %q: %w", scopeID, err)
	}
	loaded, inserted := 0, int64(0)
	if !locked {
		// The scope was deleted; its facts and keys went with it.
		done = true
	} else {
		envelopes, err := r.LoadPage(ctx, tx, scopeID, after)
		if err != nil {
			return "", false, err
		}
		if inserted, err = r.Insert(ctx, tx, envelopes); err != nil {
			return "", false, err
		}
		loaded = len(envelopes)
		if loaded == r.PageSize {
			last = envelopes[loaded-1].FactID
		} else {
			done = true
		}
	}
	now := time.Now().UTC()
	if inserted > 0 {
		if _, err := tx.ExecContext(ctx, addProgressSQL, SBTRepairMarker+sbtRepairInsertedSuffix, strconv.FormatInt(inserted, 10), now); err != nil {
			return "", false, fmt.Errorf("count sbt key repair inserts: %w", err)
		}
	}
	if done {
		if _, err := tx.ExecContext(ctx, saveProgressSQL, SBTRepairMarker, scopeID, now); err != nil {
			return "", false, fmt.Errorf("save sbt key repair cursor: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return "", false, fmt.Errorf("commit sbt key repair transaction: %w", err)
	}
	committed = true
	if elapsed := time.Since(began); elapsed > report.MaxTransaction {
		report.MaxTransaction = elapsed
	}
	if loaded > 0 {
		report.Pages++
	}
	report.Facts += loaded
	report.InsertedRows += inserted
	return last, done, nil
}

func loadWalkInserted(ctx context.Context, queryer db.Queryer) (int64, error) {
	value, err := loadProgress(ctx, queryer, SBTRepairMarker+sbtRepairInsertedSuffix)
	if err != nil || value == "" {
		return 0, err
	}
	total, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse sbt key repair insert total %q: %w", value, err)
	}
	return total, nil
}

func loadProgress(ctx context.Context, queryer db.Queryer, name string) (string, error) {
	rows, err := queryer.QueryContext(ctx, loadProgressSQL, name)
	if err != nil {
		return "", fmt.Errorf("load sbt key repair progress: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return "", rows.Err()
	}
	var value string
	if err := rows.Scan(&value); err != nil {
		return "", fmt.Errorf("scan sbt key repair progress: %w", err)
	}
	return value, rows.Err()
}
