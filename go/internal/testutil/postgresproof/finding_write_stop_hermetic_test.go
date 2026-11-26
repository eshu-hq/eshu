// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

// This file proves the finding write stop helpers hermetically: a scripted
// database/sql driver answers the trigger-probe queries, so no test here
// needs Docker, a live Postgres, or a proof database. The allowlist scanner
// (finding_write_stop_allowlist_test.go) skips files with this file's
// finding_write_stop prefix, as it does the implementation, because these
// calls never touch a real database.

// fwsStubRows serves one scripted row, then io.EOF.
type fwsStubRows struct {
	columns []string
	values  []driver.Value
	used    bool
}

// Columns returns the scripted column names.
func (r *fwsStubRows) Columns() []string { return r.columns }

// Close is a no-op: the scripted rows hold no resources.
func (r *fwsStubRows) Close() error { return nil }

// Next serves the scripted values once, then reports exhaustion.
func (r *fwsStubRows) Next(dest []driver.Value) error {
	if r.used {
		return io.EOF
	}
	r.used = true
	copy(dest, r.values)
	return nil
}

// fwsStubResult is the scripted outcome of a stubbed Exec.
type fwsStubResult struct {
	rows int64
}

// LastInsertId reports no insert id: the stub never inserts.
func (r fwsStubResult) LastInsertId() (int64, error) { return 0, nil }

// RowsAffected reports the scripted affected-row count.
func (r fwsStubResult) RowsAffected() (int64, error) { return r.rows, nil }

// fwsStubConn answers queries and execs from test-supplied closures.
type fwsStubConn struct {
	query func(query string) (driver.Rows, error)
	exec  func(query string) (driver.Result, error)
}

// Prepare is unsupported: the helpers only use QueryContext and ExecContext.
func (c *fwsStubConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("finding_write_stop hermetic stub: Prepare not supported")
}

// Close is a no-op: the stub holds no connection.
func (c *fwsStubConn) Close() error { return nil }

// Begin is unsupported: the helpers never open a transaction.
func (c *fwsStubConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("finding_write_stop hermetic stub: Begin not supported")
}

// QueryContext answers through the test's query closure.
func (c *fwsStubConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	return c.query(query)
}

// ExecContext answers through the test's exec closure.
func (c *fwsStubConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	return c.exec(query)
}

// fwsStubDriver opens stub connections from a test-supplied constructor.
type fwsStubDriver struct {
	open func() (driver.Conn, error)
}

// Open returns a fresh stub connection.
func (d *fwsStubDriver) Open(string) (driver.Conn, error) { return d.open() }

var fwsStubDriverSeq atomic.Int64

// openFWSStubDB registers a uniquely named stub driver answering queries and
// execs from the given closures, and returns a *sql.DB over it. Each test
// gets its own driver name so tests stay parallel-safe.
func openFWSStubDB(t *testing.T, query func(string) (driver.Rows, error), exec func(string) (driver.Result, error)) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("fwsstub-%d", fwsStubDriverSeq.Add(1))
	sql.Register(name, &fwsStubDriver{open: func() (driver.Conn, error) {
		return &fwsStubConn{query: query, exec: exec}, nil
	}})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("open the stub database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close the stub database: %v", err)
		}
	})
	return db
}

// fwsQueryRouter answers the two probe shapes the helpers issue: the armed
// read (tgenabled) and the presence probe (EXISTS). Anything else is a test
// bug, surfaced loudly rather than read as a negative.
func fwsQueryRouter(armed driver.Value, armedErr error, present driver.Value, presentErr error) func(string) (driver.Rows, error) {
	return func(query string) (driver.Rows, error) {
		switch {
		case strings.Contains(query, "tgenabled"):
			if armedErr != nil {
				return nil, fmt.Errorf("stub the armed read: %w", armedErr)
			}
			return &fwsStubRows{columns: []string{"armed"}, values: []driver.Value{armed}}, nil
		case strings.Contains(query, "EXISTS"):
			if presentErr != nil {
				return nil, fmt.Errorf("stub the presence probe: %w", presentErr)
			}
			return &fwsStubRows{columns: []string{"exists"}, values: []driver.Value{present}}, nil
		default:
			return nil, fmt.Errorf("finding_write_stop hermetic stub: unexpected query %q", query)
		}
	}
}

// fwsExecRouter records exec calls and answers with execErr. A nil execErr
// reports success; wantVerb, when non-empty, must appear in the statement.
func fwsExecRouter(calls *int, execErr error, wantVerb string) func(string) (driver.Result, error) {
	return func(query string) (driver.Result, error) {
		*calls++
		if wantVerb != "" && !strings.Contains(query, wantVerb) {
			return nil, fmt.Errorf("finding_write_stop hermetic stub: exec %q misses verb %q", query, wantVerb)
		}
		if !strings.Contains(query, FindingWriteStopTrigger) {
			return nil, fmt.Errorf("finding_write_stop hermetic stub: exec %q misses the trigger name", query)
		}
		if execErr != nil {
			return nil, fmt.Errorf("stub the trigger exec: %w", execErr)
		}
		return fwsStubResult{}, nil
	}
}

// fwsFakeTB scripts the failure surface the helpers take: Fatalf records
// instead of exiting, and Cleanup defers run explicitly by the test.
type fwsFakeTB struct {
	fatals   []string
	errors   []string
	cleanups []func()
}

// Helper is a no-op: the fake has no test log to attribute to.
func (f *fwsFakeTB) Helper() {}

// Fatalf records a fatal failure instead of exiting the test.
func (f *fwsFakeTB) Fatalf(format string, args ...any) {
	f.fatals = append(f.fatals, fmt.Sprintf(format, args...))
}

// Errorf records a non-fatal failure.
func (f *fwsFakeTB) Errorf(format string, args ...any) {
	f.errors = append(f.errors, fmt.Sprintf(format, args...))
}

// Cleanup defers a function the test runs explicitly with runCleanups.
func (f *fwsFakeTB) Cleanup(fn func()) { f.cleanups = append(f.cleanups, fn) }

// runCleanups runs deferred functions last-in-first-out, as testing.TB does.
func (f *fwsFakeTB) runCleanups() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

// requireClean fails the test when the fake recorded any failure.
func (f *fwsFakeTB) requireClean(t *testing.T) {
	t.Helper()
	if len(f.fatals) != 0 {
		t.Fatalf("fake TB recorded fatals: %q", f.fatals)
	}
	if len(f.errors) != 0 {
		t.Fatalf("fake TB recorded errors: %q", f.errors)
	}
}

// TestFindingWriteStopArmedReadsTheTrigger proves the armed read reports the
// trigger state and wraps a query failure instead of returning it raw.
func TestFindingWriteStopArmedReadsTheTrigger(t *testing.T) {
	t.Parallel()

	t.Run("armed", func(t *testing.T) {
		t.Parallel()

		db := openFWSStubDB(t, fwsQueryRouter(true, nil, false, nil), fwsExecRouter(new(int), nil, ""))
		armed, err := FindingWriteStopArmed(context.Background(), db)
		if err != nil {
			t.Fatalf("FindingWriteStopArmed() error = %v", err)
		}
		if !armed {
			t.Fatal("FindingWriteStopArmed() = false, want true for an enabled trigger")
		}
	})

	t.Run("disarmed schema reads false", func(t *testing.T) {
		t.Parallel()

		db := openFWSStubDB(t, fwsQueryRouter(false, nil, false, nil), fwsExecRouter(new(int), nil, ""))
		armed, err := FindingWriteStopArmed(context.Background(), db)
		if err != nil {
			t.Fatalf("FindingWriteStopArmed() error = %v", err)
		}
		if armed {
			t.Fatal("FindingWriteStopArmed() = true, want false for a disabled trigger")
		}
	})

	t.Run("query error is wrapped", func(t *testing.T) {
		t.Parallel()

		db := openFWSStubDB(t, fwsQueryRouter(false, errFWSBoom, false, nil), fwsExecRouter(new(int), nil, ""))
		if _, err := FindingWriteStopArmed(context.Background(), db); err == nil {
			t.Fatal("FindingWriteStopArmed() = nil error, want the wrapped query error")
		} else if !strings.Contains(err.Error(), "read finding write stop state") {
			t.Fatalf("error = %v, want it to name the finding write stop read", err)
		}
	})
}

// errFWSBoom is the canned backend failure the stubbed queries and execs return.
var errFWSBoom = fmt.Errorf("finding_write_stop hermetic stub: boom")

// TestFindingWriteStopSetArmAndDisarm proves the arm/disarm primitives: the
// absent-trigger no-op issues no ALTER, the present-trigger path verbs the
// statement correctly, and probe and exec failures are wrapped with the verb.
func TestFindingWriteStopSetArmAndDisarm(t *testing.T) {
	t.Parallel()

	t.Run("absent trigger is a no-op", func(t *testing.T) {
		t.Parallel()

		var calls int
		db := openFWSStubDB(t, fwsQueryRouter(false, nil, false, nil), fwsExecRouter(&calls, nil, ""))
		if err := DisarmFindingWriteStop(context.Background(), db); err != nil {
			t.Fatalf("DisarmFindingWriteStop(absent) error = %v", err)
		}
		if calls != 0 {
			t.Fatalf("DisarmFindingWriteStop(absent) issued %d exec(s), want none", calls)
		}
		if err := ArmFindingWriteStop(context.Background(), db); err != nil {
			t.Fatalf("ArmFindingWriteStop(absent) error = %v", err)
		}
		if calls != 0 {
			t.Fatalf("ArmFindingWriteStop(absent) issued %d exec(s), want none", calls)
		}
	})

	t.Run("present trigger verbs the alter", func(t *testing.T) {
		t.Parallel()

		var calls int
		db := openFWSStubDB(t, fwsQueryRouter(false, nil, true, nil), fwsExecRouter(&calls, nil, ""))
		if err := DisarmFindingWriteStop(context.Background(), db); err != nil {
			t.Fatalf("DisarmFindingWriteStop(present) error = %v", err)
		}
		if err := ArmFindingWriteStop(context.Background(), db); err != nil {
			t.Fatalf("ArmFindingWriteStop(present) error = %v", err)
		}
		if calls != 2 {
			t.Fatalf("arm plus disarm issued %d exec(s), want 2", calls)
		}
	})

	t.Run("verbs name disable and enable", func(t *testing.T) {
		t.Parallel()

		var verbs []string
		verbatim := func(query string) (driver.Result, error) {
			verbs = append(verbs, query)
			return fwsStubResult{}, nil
		}
		db := openFWSStubDB(t, fwsQueryRouter(false, nil, true, nil), verbatim)
		if err := DisarmFindingWriteStop(context.Background(), db); err != nil {
			t.Fatalf("DisarmFindingWriteStop error = %v", err)
		}
		if err := ArmFindingWriteStop(context.Background(), db); err != nil {
			t.Fatalf("ArmFindingWriteStop error = %v", err)
		}
		if len(verbs) != 2 || !strings.Contains(verbs[0], "DISABLE") || !strings.Contains(verbs[1], "ENABLE") {
			t.Fatalf("exec verbs = %q, want DISABLE then ENABLE", verbs)
		}
	})

	t.Run("probe error is wrapped", func(t *testing.T) {
		t.Parallel()

		db := openFWSStubDB(t, fwsQueryRouter(false, nil, false, errFWSBoom), fwsExecRouter(new(int), nil, ""))
		if err := DisarmFindingWriteStop(context.Background(), db); err == nil {
			t.Fatal("DisarmFindingWriteStop() = nil error, want the wrapped probe error")
		} else if !strings.Contains(err.Error(), "probe finding write stop trigger") {
			t.Fatalf("error = %v, want it to name the trigger probe", err)
		}
	})

	t.Run("exec error names the verb", func(t *testing.T) {
		t.Parallel()

		db := openFWSStubDB(t, fwsQueryRouter(false, nil, true, nil), fwsExecRouter(new(int), errFWSBoom, ""))
		if err := DisarmFindingWriteStop(context.Background(), db); err == nil {
			t.Fatal("DisarmFindingWriteStop() = nil error, want the wrapped exec error")
		} else if !strings.Contains(err.Error(), "DISABLE finding write stop trigger") {
			t.Fatalf("error = %v, want it to name the DISABLE verb", err)
		}
		db = openFWSStubDB(t, fwsQueryRouter(false, nil, true, nil), fwsExecRouter(new(int), errFWSBoom, ""))
		if err := ArmFindingWriteStop(context.Background(), db); err == nil {
			t.Fatal("ArmFindingWriteStop() = nil error, want the wrapped exec error")
		} else if !strings.Contains(err.Error(), "ENABLE finding write stop trigger") {
			t.Fatalf("error = %v, want it to name the ENABLE verb", err)
		}
	})
}

// TestRearmFindingWriteStopFailsLoudly proves the re-arm check fails the test
// when the arm errors, when the trigger still reads disabled, and when the
// verify read itself fails.
func TestRearmFindingWriteStopFailsLoudly(t *testing.T) {
	t.Parallel()

	t.Run("clean re-arm records nothing", func(t *testing.T) {
		t.Parallel()

		db := openFWSStubDB(t, fwsQueryRouter(true, nil, true, nil), fwsExecRouter(new(int), nil, "ENABLE"))
		fake := &fwsFakeTB{}
		rearmFindingWriteStop(fake, db)
		fake.requireClean(t)
	})

	t.Run("disabled after re-arm errors", func(t *testing.T) {
		t.Parallel()

		db := openFWSStubDB(t, fwsQueryRouter(false, nil, true, nil), fwsExecRouter(new(int), nil, "ENABLE"))
		fake := &fwsFakeTB{}
		rearmFindingWriteStop(fake, db)
		if len(fake.errors) != 1 || !strings.Contains(fake.errors[0], "reads disabled") {
			t.Fatalf("errors = %q, want the reads-disabled failure", fake.errors)
		}
	})

	t.Run("arm error errors", func(t *testing.T) {
		t.Parallel()

		db := openFWSStubDB(t, fwsQueryRouter(true, nil, true, nil), fwsExecRouter(new(int), errFWSBoom, "ENABLE"))
		fake := &fwsFakeTB{}
		rearmFindingWriteStop(fake, db)
		if len(fake.errors) != 1 || !strings.Contains(fake.errors[0], "re-arm the finding write stop") {
			t.Fatalf("errors = %q, want the re-arm failure", fake.errors)
		}
	})

	t.Run("verify error errors", func(t *testing.T) {
		t.Parallel()

		db := openFWSStubDB(t, fwsQueryRouter(false, errFWSBoom, true, nil), fwsExecRouter(new(int), nil, "ENABLE"))
		fake := &fwsFakeTB{}
		rearmFindingWriteStop(fake, db)
		if len(fake.errors) != 1 || !strings.Contains(fake.errors[0], "verify the finding write stop is armed") {
			t.Fatalf("errors = %q, want the verify failure", fake.errors)
		}
	})
}

// TestSeedFindingFactsScopesTheDisarm proves the scoped plant runs the seed
// between a disarm and a re-arm, and proves a failed disarm reports without
// running the seed.
func TestSeedFindingFactsScopesTheDisarm(t *testing.T) {
	t.Parallel()

	t.Run("seed runs armed again after", func(t *testing.T) {
		t.Parallel()

		var calls int
		db := openFWSStubDB(t, fwsQueryRouter(true, nil, true, nil), fwsExecRouter(&calls, nil, ""))
		fake := &fwsFakeTB{}
		seedRan := false
		SeedFindingFacts(fake, db, func() { seedRan = true })
		if !seedRan {
			t.Fatal("seed did not run inside the scoped disarm")
		}
		// One DISABLE for the seed window plus one ENABLE for the re-arm.
		if calls != 2 {
			t.Fatalf("scoped seed issued %d exec(s), want 2", calls)
		}
		fake.requireClean(t)
	})

	t.Run("failed disarm skips the seed", func(t *testing.T) {
		t.Parallel()

		var calls int
		db := openFWSStubDB(t, fwsQueryRouter(false, nil, false, errFWSBoom), fwsExecRouter(&calls, nil, ""))
		fake := &fwsFakeTB{}
		seedRan := false
		SeedFindingFacts(fake, db, func() { seedRan = true })
		if seedRan {
			t.Fatal("seed ran after a failed disarm")
		}
		if len(fake.fatals) != 1 || !strings.Contains(fake.fatals[0], "disarm the finding write stop to seed") {
			t.Fatalf("fatals = %q, want the disarm failure", fake.fatals)
		}
		if calls != 0 {
			t.Fatalf("failed disarm issued %d exec(s), want none", calls)
		}
	})
}

// TestDisarmFindingWriteStopForTestRearmsAtCleanup proves the whole-test
// disarm re-arms at cleanup, and proves a failed disarm reports without
// registering the re-arm.
func TestDisarmFindingWriteStopForTestRearmsAtCleanup(t *testing.T) {
	t.Parallel()

	t.Run("cleanup re-arms", func(t *testing.T) {
		t.Parallel()

		var calls int
		db := openFWSStubDB(t, fwsQueryRouter(true, nil, true, nil), fwsExecRouter(&calls, nil, ""))
		fake := &fwsFakeTB{}
		DisarmFindingWriteStopForTest(fake, db)
		fake.requireClean(t)
		if len(fake.cleanups) != 1 {
			t.Fatalf("registered %d cleanup(s), want the re-arm", len(fake.cleanups))
		}
		fake.runCleanups()
		fake.requireClean(t)
		// One DISABLE for the test window plus one ENABLE at cleanup.
		if calls != 2 {
			t.Fatalf("whole-test disarm issued %d exec(s), want 2", calls)
		}
	})

	t.Run("failed disarm registers no re-arm", func(t *testing.T) {
		t.Parallel()

		var calls int
		db := openFWSStubDB(t, fwsQueryRouter(false, nil, false, errFWSBoom), fwsExecRouter(&calls, nil, ""))
		fake := &fwsFakeTB{}
		DisarmFindingWriteStopForTest(fake, db)
		if len(fake.fatals) != 1 || !strings.Contains(fake.fatals[0], "disarm the finding write stop for the test") {
			t.Fatalf("fatals = %q, want the disarm failure", fake.fatals)
		}
		if len(fake.cleanups) != 0 {
			t.Fatalf("registered %d cleanup(s), want none after a failed disarm", len(fake.cleanups))
		}
		if calls != 0 {
			t.Fatalf("failed disarm issued %d exec(s), want none", calls)
		}
	})
}
