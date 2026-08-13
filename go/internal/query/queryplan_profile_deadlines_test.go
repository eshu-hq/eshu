// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Deadline budgets for the live query-plan PROFILE gate, which lives in
// queryplan_profile_live_test.go behind the queryplan_profile_live build tag
// and is driven by scripts/verify-query-plan-profile.sh.
//
// This file carries no build tag on purpose: the tagged live test and the
// hermetic tests below must share one set of budgets, and the hermetic tests
// have to run on every PR rather than only where a Neo4j container exists.
//
// Every phase of the gate gets its OWN deadline. The gate used to share a
// single 2-minute context across schema setup and all ~516 PROFILE subtests,
// so `CALL db.awaitIndexes(120)` could spend the whole budget by itself, and
// every remaining subtest then failed instantly with the driver's wording:
// "ConnectivityError: Timeout while waiting for connection to any of
// [[127.0.0.1:32768]] ... i/o timeout". That sends whoever reads the log
// hunting a broken Neo4j container instead of an exhausted clock.
//
// Sizing comes from measurement, not from a guess (2026-08-13):
//
//	green CI run 31671825681 (main)     54.946s for the whole test
//	local dev Mac, this worktree        32.712s
//	red CI run 31671122417 (PR #6093)   spent the full 120s and cascaded. That
//	                                    runner compiled ./internal/query in
//	                                    9m51s against the green runner's 2m22s,
//	                                    4.2x slower, so its honest runtime
//	                                    projects to 54.9s x 4.2 =~ 230s. #6093
//	                                    changed no file under go/internal/query
//	                                    or go/internal/queryplan; it went red
//	                                    purely on the shared clock.
//
// So a healthy runner used 46% of the old budget and a runner 2-3x slower than
// median blew through it. The budgets below are sized off those numbers:
const (
	// queryplanProfileConnectBudget covers one Bolt handshake to a container
	// scripts/verify-query-plan-profile.sh has already waited for ("Started."
	// in the container log), so 30s is a hang detector, not a race.
	queryplanProfileConnectBudget = 30 * time.Second

	// queryplanProfileSchemaBudget covers the CREATE statements plus
	// `CALL db.awaitIndexes(120)`. That await is allowed 120s by its own
	// argument, so any budget at or under 2m would kill the wait the gate
	// itself asked for: 180s is that 120s contract plus 60s for the CREATEs on
	// a slow runner. Index population can no longer eat the subtests' time.
	queryplanProfileSchemaBudget = 3 * time.Minute

	// queryplanProfileQueryBudget bounds ONE profiled query. 516 profiles in
	// 54.9s on green CI is ~106ms each, so 30s is ~280x the median. A single
	// PROFILE against this empty isolated graph taking 30s is a planner defect
	// worth failing on, and it now fails exactly one subtest with a message
	// that says so.
	queryplanProfileQueryBudget = 30 * time.Second

	// queryplanProfileTotalBudget is the wall-clock backstop for the whole
	// gate: 6.5x the 54.9s green-CI runtime and ~1.6x the 230s projected for
	// the slowest runner actually observed. It is a hang guard, not a per-query
	// limit, and it sits below the -timeout the gate script passes to `go test`
	// so this budget's own message wins over a test-binary panic.
	queryplanProfileTotalBudget = 6 * time.Minute
)

// queryplanProfileStepError explains a failed step of the live PROFILE gate in
// the terms that actually caused it.
//
// The Neo4j driver reports an expired context as a connection timeout, because
// from its side that is what happened: the context died while it waited for a
// connection. Reported raw, that wording accuses the graph backend. When the
// step's own deadline is what expired, this says so, and names the step, the
// elapsed time, and the budget it was measured against.
func queryplanProfileStepError(ctx context.Context, step string, elapsed, budget time.Duration, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf(
			"%s ran out of its own time: %s elapsed against a %s budget. "+
				"This is an exhausted deadline, not an unreachable graph backend -- "+
				"the driver words it as a connectivity timeout because the context died while it waited. "+
				"Driver error: %v",
			step, elapsed.Round(time.Millisecond), budget, err,
		)
	}
	return fmt.Errorf("%s: %w", step, err)
}

// queryplanProfileTotalBudgetError reports the gate outrunning its wall-clock
// backstop, and returns nil while it is still inside it. The gate checks this
// between profiles so a machine that is pathologically slow stops with one
// honest failure instead of a tail of subtests failing on a clock the run had
// already spent.
func queryplanProfileTotalBudgetError(elapsed time.Duration, completed int) error {
	if elapsed <= queryplanProfileTotalBudget {
		return nil
	}
	return fmt.Errorf(
		"live PROFILE gate ran out of wall-clock time after %d profiles: %s elapsed against its %s backstop. "+
			"Every profile up to here was checked; the gate stopped itself rather than let the rest fail on a "+
			"spent clock. If this machine is genuinely this slow, raise queryplanProfileTotalBudget and cite the "+
			"measurement that justifies it",
		completed, elapsed.Round(time.Second), queryplanProfileTotalBudget,
	)
}

func TestQueryplanProfileStepErrorNamesTheExhaustedBudget(t *testing.T) {
	t.Parallel()
	// The verbatim shape the driver produced when the shared 2-minute context
	// expired mid-run, which is what this wrapper exists to reinterpret.
	driverErr := errors.New(
		"ConnectivityError: Timeout while waiting for connection to any of [[127.0.0.1:32768]]: " +
			"dial tcp 127.0.0.1:32768: i/o timeout",
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()

	got := queryplanProfileStepError(ctx, "PROFILE QP-GRAPH-ENTITY-COUNT", 30*time.Second, queryplanProfileQueryBudget, driverErr)
	if got == nil {
		t.Fatal("queryplanProfileStepError() = nil, want an error")
	}
	message := got.Error()
	for _, want := range []string{
		"PROFILE QP-GRAPH-ENTITY-COUNT",
		"30s elapsed",
		"against a 30s budget",
		"exhausted deadline, not an unreachable graph backend",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("queryplanProfileStepError() = %q, want it to contain %q", message, want)
		}
	}
	if strings.HasPrefix(message, "ConnectivityError") {
		t.Errorf("queryplanProfileStepError() = %q, want it to lead with the exhausted deadline, not the driver wording", message)
	}
}

func TestQueryplanProfileStepErrorPreservesRealFailures(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("Neo.ClientError.Statement.SyntaxError")
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	got := queryplanProfileStepError(ctx, "PROFILE QP-RELATIONSHIPS-EDGES", 12*time.Millisecond, queryplanProfileQueryBudget, sentinel)
	if !errors.Is(got, sentinel) {
		t.Fatalf("queryplanProfileStepError() = %v, want it to wrap the driver error", got)
	}
	if strings.Contains(got.Error(), "exhausted deadline") {
		t.Errorf("queryplanProfileStepError() = %q, want no deadline claim when the context is still live", got)
	}
}

func TestQueryplanProfileTotalBudgetError(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		elapsed time.Duration
		wantErr bool
	}{
		"inside the backstop": {elapsed: queryplanProfileTotalBudget - time.Second, wantErr: false},
		"exactly at it":       {elapsed: queryplanProfileTotalBudget, wantErr: false},
		"past it":             {elapsed: queryplanProfileTotalBudget + time.Second, wantErr: true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := queryplanProfileTotalBudgetError(test.elapsed, 412)
			if test.wantErr != (err != nil) {
				t.Fatalf("queryplanProfileTotalBudgetError(%s) = %v, wantErr %t", test.elapsed, err, test.wantErr)
			}
			if err == nil {
				return
			}
			for _, want := range []string{"after 412 profiles", "6m0s backstop"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("queryplanProfileTotalBudgetError() = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

// TestQueryplanProfileBudgetsAreOrdered pins the relationships the budgets only
// make sense under: the await the schema phase issues is 120s by contract, so a
// schema budget at or under that would kill the gate's own wait, and a per-query
// budget at or above the total backstop would make the backstop unreachable.
func TestQueryplanProfileBudgetsAreOrdered(t *testing.T) {
	t.Parallel()
	const awaitIndexesContract = 120 * time.Second
	if queryplanProfileSchemaBudget <= awaitIndexesContract {
		t.Errorf("queryplanProfileSchemaBudget = %s, want more than the %s db.awaitIndexes() argument the schema phase issues",
			queryplanProfileSchemaBudget, awaitIndexesContract)
	}
	if queryplanProfileQueryBudget >= queryplanProfileTotalBudget {
		t.Errorf("queryplanProfileQueryBudget = %s, want less than the %s total backstop",
			queryplanProfileQueryBudget, queryplanProfileTotalBudget)
	}
	if queryplanProfileSchemaBudget >= queryplanProfileTotalBudget {
		t.Errorf("queryplanProfileSchemaBudget = %s, want less than the %s total backstop",
			queryplanProfileSchemaBudget, queryplanProfileTotalBudget)
	}
	if queryplanProfileConnectBudget <= 0 || queryplanProfileConnectBudget >= queryplanProfileTotalBudget {
		t.Errorf("queryplanProfileConnectBudget = %s, want a positive budget under the %s total backstop",
			queryplanProfileConnectBudget, queryplanProfileTotalBudget)
	}
}
