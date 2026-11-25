// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// This file bounds the corpus-wide relationship-backfill reads (issue
// #11460). A 3h37m anchor-scoped read with no timeout held one snapshot for
// the whole load and stalled a production migration whose concurrent index
// build waited for that old snapshot. Every unbounded loader now pages by the
// (observed_at, fact_id) keyset with a bounded LIMIT, runs each page in its
// own short transaction carrying SET LOCAL statement_timeout and a
// path-distinct application_name, and releases that transaction before the
// next page begins. No read transaction is ever held across client-side
// processing, and no cursor WITH HOLD exists anywhere on these paths.

const (
	// BackfillApplicationNamePerCommit identifies the ingester per-commit
	// new-repository backfill in pg_stat_activity.
	BackfillApplicationNamePerCommit = "eshu-backfill-per-commit"
	// BackfillApplicationNameDeferred identifies the deferred-maintenance
	// worker backfill in pg_stat_activity.
	BackfillApplicationNameDeferred = "eshu-backfill-deferred"
	// BackfillApplicationNameTargeted identifies the targeted-maintenance
	// closure backfill in pg_stat_activity.
	BackfillApplicationNameTargeted = "eshu-backfill-targeted"

	// backfillApplicationNameFallback covers direct loader calls that carry
	// no path name. Every production entry point sets its path name, so this
	// only fires in unit tests.
	backfillApplicationNameFallback = "eshu-backfill"
)

const (
	// anchorScopedStatementTimeout bounds one page of an unbounded
	// anchor-scoped or ArgoCD config fact load. It is a LOCAL backstop: the
	// pages complete in seconds, so anything approaching this fails loudly
	// instead of growing into another multi-hour snapshot.
	anchorScopedStatementTimeout = 300 * time.Second
	// anchorScopedOverallTimeout bounds the whole unbounded load, including
	// client-side row decoding between pages. It sits just above the
	// per-statement backstop so a hung client-side stall still cancels
	// loudly; the database fires first on a runaway plan.
	anchorScopedOverallTimeout = 330 * time.Second
	// deferredPartitionStatementTimeout bounds one partition-bounded
	// deferred fact load. Partitions are small by construction, so this is a
	// backstop against a regressed plan, not a latency budget.
	deferredPartitionStatementTimeout = 120 * time.Second
	// backfillFactPageSize bounds one keyset page of the corpus-wide fact
	// loads (5-10k rows; start at 5k and tune by p99 page latency).
	backfillFactPageSize = 5000
)

// backfillAppNameKey carries the calling backfill path's application_name.
type backfillAppNameKey struct{}

// WithBackfillApplicationName returns a context that names the calling
// backfill path. The page runner stamps every page transaction it opens with
// that name, so the three paths stay distinguishable in pg_stat_activity.
func WithBackfillApplicationName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, backfillAppNameKey{}, name)
}

// ensureBackfillApplicationName sets the path name when the caller has not
// already named a more specific one. Entry points call it with their own
// path default; a name set upstream (for example the targeted closure
// running the shared anchor loader) always wins.
func ensureBackfillApplicationName(ctx context.Context, name string) context.Context {
	if current, ok := ctx.Value(backfillAppNameKey{}).(string); ok && current != "" {
		return ctx
	}
	return WithBackfillApplicationName(ctx, name)
}

// backfillApplicationName resolves the name for one guarded query: the
// caller's path name when set, otherwise the loader's own default.
func backfillApplicationName(ctx context.Context, fallback string) string {
	if name, ok := ctx.Value(backfillAppNameKey{}).(string); ok && name != "" {
		return name
	}
	return fallback
}

// PageConn is one dedicated database connection for a bounded backfill page.
// The runner opens one per page, runs the page in its own transaction on it,
// and closes it when the page finishes, so no pooled connection ever inherits
// the page guards.
type PageConn interface {
	db.ExecQueryer
	Begin(context.Context) (db.Transaction, error)
	Close() error
}

// PageConnProvider opens a dedicated connection for a bounded backfill page.
// *sql.DB pools implement it; narrow fakes do not, and the runner falls back
// to direct queries for those (the hermetic path, with no guards to set).
type PageConnProvider interface {
	OpenBackfillPageConn(context.Context) (PageConn, error)
}

// runGuardedBackfillQuery runs one backfill read with a statement timeout and
// an application_name. On a PageConnProvider handle it opens a dedicated
// connection and runs fn inside a short transaction stamped with SET LOCAL
// guards, committed (or rolled back) before returning — the transaction never
// outlives the page. Inside a caller-owned transaction (db.Transaction) it
// stamps that transaction once and runs fn on it: opening a nested
// transaction is impossible there, and the caller owns the longer lifetime.
// Otherwise it runs fn directly on the handle, guarded by the caller's
// context deadline alone.
func runGuardedBackfillQuery(
	ctx context.Context,
	queryer db.Queryer,
	statementTimeout time.Duration,
	appName string,
	run func(q db.Queryer) error,
) error {
	if provider, ok := queryer.(PageConnProvider); ok {
		return runGuardedBackfillPageTx(ctx, provider, statementTimeout, appName, run)
	}
	if tx, ok := queryer.(db.Transaction); ok {
		if err := setBackfillPageGuards(ctx, tx, statementTimeout, appName); err != nil {
			return err
		}
		return run(tx)
	}
	return run(queryer)
}

// runGuardedBackfillPageTx is the dedicated-connection leg of
// runGuardedBackfillQuery: one short transaction per page, SET LOCAL guards
// inside it, commit on success and rollback on any failure. The deferred
// rollback also covers a run function that returns without consuming rows:
// the transaction — and its snapshot — is gone before the next page begins.
func runGuardedBackfillPageTx(
	ctx context.Context,
	provider PageConnProvider,
	statementTimeout time.Duration,
	appName string,
	run func(q db.Queryer) error,
) error {
	conn, err := provider.OpenBackfillPageConn(ctx)
	if err != nil {
		return fmt.Errorf("open backfill page connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin backfill page transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := setBackfillPageGuards(ctx, tx, statementTimeout, appName); err != nil {
		return err
	}
	if err := run(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit backfill page transaction: %w", err)
	}
	committed = true
	return nil
}

// setBackfillPageGuards stamps one page transaction with its statement
// timeout and application_name. Both are LOCAL: they revert automatically at
// transaction end, so a pooled connection can never inherit them.
func setBackfillPageGuards(
	ctx context.Context, tx db.Transaction, statementTimeout time.Duration, appName string,
) error {
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf("SET LOCAL statement_timeout = '%ds'", int(statementTimeout.Seconds())),
	); err != nil {
		return fmt.Errorf("set backfill page statement_timeout: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"SET LOCAL application_name = '"+escapeBackfillLiteral(appName)+"'",
	); err != nil {
		return fmt.Errorf("set backfill page application_name: %w", err)
	}
	return nil
}

// escapeBackfillLiteral escapes a guard value for embedding in a SET LOCAL
// statement. The shipped names need no escaping; this keeps a future caller
// from breaking out of the literal.
func escapeBackfillLiteral(value string) string {
	return strings.ReplaceAll(value, `'`, `''`)
}

// isBackfillTimeoutError reports whether err ended a backfill read by
// timeout: the caller's deadline fired, the server canceled the statement
// (SQLSTATE 57014), or the message names a statement timeout. Anything else
// — including a plain user cancellation — is not a timeout.
func isBackfillTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "57014" {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "statement timeout")
}

// backfillTimeoutExceeded maps a timed-out page to the existing fail-loud
// bound: the load fails with ErrRelationshipFactLoadBudgetExceeded, and the
// caller returns the rows and bytes read so far alongside it — never a silent
// truncation.
func backfillTimeoutExceeded(operation string, timeout time.Duration, err error) error {
	return fmt.Errorf("%s timed out after %s: %w: %v",
		operation, timeout, ErrRelationshipFactLoadBudgetExceeded, err,
	)
}
