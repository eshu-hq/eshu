// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// fakeContainerImageIdentityAdmissionTable is a hermetic, in-memory
// simulation of container_image_identity_write_admission's real CAS
// semantics: one row per (scope_id, generation_id), admitted only if the
// incoming token is >= the currently stored one (or no row exists yet). It
// recognizes ONLY containerImageIdentityAdmissionQuery -- this is not a
// general-purpose fake database, it exists to prove the admission mechanism
// itself converges the way the real `INSERT ... ON CONFLICT ... WHERE
// existing.fencing_token <= EXCLUDED.fencing_token` statement guarantees,
// without requiring a live Postgres for this specific property.
type fakeContainerImageIdentityAdmissionTable struct {
	tokens map[[2]string]int64
	calls  int
}

func (f *fakeContainerImageIdentityAdmissionTable) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.calls++
	if query != containerImageIdentityAdmissionQuery {
		return fakeAdmissionResult(0), nil
	}
	if f.tokens == nil {
		f.tokens = make(map[[2]string]int64)
	}
	scopeID := args[0].(string)
	generationID := args[1].(string)
	token := args[2].(int64)
	key := [2]string{scopeID, generationID}
	stored, exists := f.tokens[key]
	if exists && stored > token {
		// Stale: the stored watermark is strictly ahead of this pass's token.
		// Mirrors the real WHERE clause rejecting the UPDATE, so ON CONFLICT
		// DO UPDATE affects zero rows.
		return fakeAdmissionResult(0), nil
	}
	f.tokens[key] = token
	return fakeAdmissionResult(1), nil
}

type fakeAdmissionResult int64

func (fakeAdmissionResult) LastInsertId() (int64, error)   { return 0, nil }
func (r fakeAdmissionResult) RowsAffected() (int64, error) { return int64(r), nil }

// TestContainerImageIdentityWriterAdmissionConvergence is the TDD-first
// hermetic proof #5874 requires: it asserts CONVERGENCE, not single-shot
// evidence-freshness ordering, because no token issued before the evidence
// reads CAN provide single-shot ordering (the irreducible nextval()-to-first
// -SELECT window the issue itself argues from). The scenario:
//
//  1. A pass with a numerically HIGHER token (representing "issued later, but
//     carrying evidence that turned out to be stale relative to what a
//     concurrent reader saw") commits FIRST. It is the first writer for this
//     (scope, generation), so it is trivially admitted and raises the
//     watermark.
//  2. A pass with a numerically LOWER token (representing genuinely fresher
//     content that happened to draw its token earlier and is now retrying
//     after a stall) is REJECTED: its token cannot outrank the higher
//     watermark already stored. This is the documented residual -- token
//     issuance order does not track evidence-freshness order within the
//     read window -- not a bug.
//  3. The SAME logical pass RETRIES, drawing a NEW token from a monotonic
//     source (nextval() never returns a stale or repeated value), which is
//     therefore guaranteed higher than the stored watermark. This retry is
//     ADMITTED. This is the convergence property: a superseded pass is
//     rejected before mutating anything and wins on retry with a fresh
//     token, so stale truth is transient and bounded by the retry/reopen
//     cadence rather than durable and unsupersedable.
func TestContainerImageIdentityWriterAdmissionConvergence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	table := &fakeContainerImageIdentityAdmissionTable{}
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	const scopeID = "repo:team-api"
	const generationID = "generation-git"

	// Step 1: the stale-content, higher-token pass commits first.
	admitted, err := tryAdmitContainerImageIdentityWrite(ctx, table, scopeID, generationID, 5, now)
	if err != nil {
		t.Fatalf("step 1: tryAdmitContainerImageIdentityWrite() error = %v", err)
	}
	if !admitted {
		t.Fatal("step 1: first pass for a (scope, generation) pair must always be admitted")
	}

	// Step 2: the genuinely fresher pass, holding a LOWER token, is rejected.
	admitted, err = tryAdmitContainerImageIdentityWrite(ctx, table, scopeID, generationID, 3, now)
	if err != nil {
		t.Fatalf("step 2: tryAdmitContainerImageIdentityWrite() error = %v", err)
	}
	if admitted {
		t.Fatal(
			"step 2: a pass whose token is older than the stored watermark must be rejected " +
				"even though its evidence is fresher -- no token issued before the reads can " +
				"provide single-shot freshness ordering; this is the documented residual",
		)
	}

	// Step 3: the SAME pass retries with a fresh, strictly higher token (as a
	// real Postgres sequence guarantees on the next nextval() call) and wins.
	admitted, err = tryAdmitContainerImageIdentityWrite(ctx, table, scopeID, generationID, 7, now)
	if err != nil {
		t.Fatalf("step 3: tryAdmitContainerImageIdentityWrite() error = %v", err)
	}
	if !admitted {
		t.Fatal(
			"step 3: a retry with a fresh, higher token must be admitted -- this is the " +
				"convergence guarantee: a superseded pass is rejected before mutating anything " +
				"and wins on retry, so stale truth is transient rather than durable",
		)
	}

	// Equal-token retry (a redelivery of the exact same write attempt) must
	// also be admitted, per the `<=` comparison -- not treated as stale.
	admitted, err = tryAdmitContainerImageIdentityWrite(ctx, table, scopeID, generationID, 7, now)
	if err != nil {
		t.Fatalf("equal-token retry: tryAdmitContainerImageIdentityWrite() error = %v", err)
	}
	if !admitted {
		t.Fatal("equal-token retry must be admitted (<=, not <): a redelivery of the same write attempt reuses the same token")
	}

	if table.calls != 4 {
		t.Fatalf("admission CAS calls = %d, want 4", table.calls)
	}
}

// TestContainerImageIdentityWriterAdmissionIsolatesDifferentGenerations
// proves the admission watermark is scoped per (scope, generation): a
// rejection for one generation must never affect a DIFFERENT generation of
// the same scope, or a reopen replay targeting a new generation could be
// wrongly blocked by an unrelated older generation's watermark.
func TestContainerImageIdentityWriterAdmissionIsolatesDifferentGenerations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	table := &fakeContainerImageIdentityAdmissionTable{}
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)

	if admitted, err := tryAdmitContainerImageIdentityWrite(ctx, table, "repo:team-api", "generation-1", 100, now); err != nil || !admitted {
		t.Fatalf("generation-1 admission: admitted=%v err=%v, want true/nil", admitted, err)
	}
	// A low token for a DIFFERENT generation of the same scope must be
	// admitted independently -- it is not competing against generation-1's
	// high watermark at all.
	if admitted, err := tryAdmitContainerImageIdentityWrite(ctx, table, "repo:team-api", "generation-2", 1, now); err != nil || !admitted {
		t.Fatalf("generation-2 admission: admitted=%v err=%v, want true/nil", admitted, err)
	}
}
