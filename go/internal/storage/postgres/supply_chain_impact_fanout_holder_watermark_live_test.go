// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The #11613 PR1 holder-freshness watermark live proofs (red-first): the
// reopen unions into a succeeded holder's surviving stamp, the shadow
// outcome reports a would-skip while still reopening, the fail-closed
// matrix never shadows, and the pass-state watermark is generation-bound.

// shadowOrigin builds a token-bearing origin for one CVE set, the shape a
// findings pass fans out with after PR1 stamps its evidence-read token.
func shadowOrigin(token int64, cves ...string) supplychaincore.FanOutOrigin {
	return supplychaincore.FanOutOrigin{
		SourceScopeID:      "scope:shadow-origin",
		SourceGenerationID: "gen-shadow",
		SourceIntentID:     "intent-shadow",
		CVEIDs:             cves,
		FencingToken:       token,
	}
}

// shadowSucceed turns a seeded pending item into a succeeded one with no
// stamp, the post-image of a genuine full pass that consumed its origin.
func shadowSucceed(t *testing.T, ctx context.Context, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id = $1`, id); err != nil {
		t.Fatalf("succeed %s: %v", id, err)
	}
}

// shadowWatermarkWrite records a full evidence-complete pass through the
// production writer and returns its admitted fencing token.
func shadowWatermarkWrite(t *testing.T, ctx context.Context, db *sql.DB, intent, scope, generation string) int64 {
	t.Helper()
	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
		Now: func() time.Time { return replaceSetLiveNow },
	}
	if _, err := writer.WriteSupplyChainImpactFindings(ctx,
		fenceWrite(intent, scope, generation, fallbackFinding("CVE-2026-9101", "pkg:npm/a@1"))); err != nil {
		t.Fatalf("watermark write %s: %v", intent, err)
	}
	_, _, _, token := fencePassState(t, ctx, db, scope)
	return token
}

// TestReopenUnionsStampOnSucceededHolderLive: a succeeded holder whose stamp
// survived (a mid-pass widen fenced out the consume) keeps its un-derived
// set when the next origin arrives. Today the succeeded arm overwrites the
// stamp and the surviving set is lost; the fix unions it like the
// pending/flagged arms already do.
func TestReopenUnionsStampOnSucceededHolderLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := fanOutLiveStore(db)

	t.Run("union", func(t *testing.T) {
		fanOutSeedScope(t, ctx, db, "h-suc-union", "g1")
		id := fanOutSeedItem(t, ctx, db, "h-suc-union", "g1")
		widenSetStamp(t, ctx, db, id, widenStamp(t, widenOriginA))
		shadowSucceed(t, ctx, db, id)

		reopen, err := store.ReopenHolderDetailed(ctx, "h-suc-union", widenOriginB)
		if err != nil {
			t.Fatalf("ReopenHolderDetailed() error = %v, want nil", err)
		}
		if reopen.Outcome != supplychaincore.FanOutReopened {
			t.Fatalf("outcome = %q, want reopened (the row still goes pending; only the stamp is unioned)", reopen.Outcome)
		}
		after := widenRead(t, ctx, db, id)
		if want := `["` + widenCVEA + `", "` + widenCVEB + `"]`; after.cves != want {
			t.Fatalf("stamp cves = %s, want %s: the surviving set must not be dropped", after.cves, want)
		}
		if after.status != "pending" || after.attempts != 0 || after.flag {
			t.Fatalf("row = %+v, want pending, attempt 0, no replay flag", after)
		}
	})

	t.Run("over cap unions to the sentinel", func(t *testing.T) {
		stored := make([]string, 0, supplychaincore.FanOutOriginCVEIDCap)
		for i := 0; i < supplychaincore.FanOutOriginCVEIDCap; i++ {
			stored = append(stored, "CVE-2026-3"+itoa(i))
		}
		capped := widenOriginA
		capped.CVEIDs = stored
		fanOutSeedScope(t, ctx, db, "h-suc-cap", "g1")
		id := fanOutSeedItem(t, ctx, db, "h-suc-cap", "g1")
		widenSetStamp(t, ctx, db, id, widenStamp(t, capped))
		shadowSucceed(t, ctx, db, id)

		if _, err := store.ReopenHolderDetailed(ctx, "h-suc-cap", widenOriginB); err != nil {
			t.Fatalf("ReopenHolderDetailed() error = %v, want nil", err)
		}
		if row := widenRead(t, ctx, db, id); row.cves != `["`+supplychaincore.FanOutStampSentinel+`"]` {
			t.Fatalf("stamp past the cap = %s, want the sentinel", row.cves)
		}
	})

	t.Run("stored sentinel stays sticky", func(t *testing.T) {
		sentinel := widenOriginA
		sentinel.CVEIDs = []string{supplychaincore.FanOutStampSentinel}
		fanOutSeedScope(t, ctx, db, "h-suc-sentinel", "g1")
		id := fanOutSeedItem(t, ctx, db, "h-suc-sentinel", "g1")
		widenSetStamp(t, ctx, db, id, widenStamp(t, sentinel))
		shadowSucceed(t, ctx, db, id)

		if _, err := store.ReopenHolderDetailed(ctx, "h-suc-sentinel", widenOriginB); err != nil {
			t.Fatalf("ReopenHolderDetailed() error = %v, want nil", err)
		}
		if row := widenRead(t, ctx, db, id); row.cves != `["`+supplychaincore.FanOutStampSentinel+`"]` {
			t.Fatalf("stored sentinel + bounded incoming = %s, want the sentinel to stay", row.cves)
		}
	})
}

func itoa(i int) string {
	s := "0000"
	n := ""
	if i == 0 {
		n = "0"
	}
	for v := i; v > 0; v /= 10 {
		n = string(rune('0'+v%10)) + n
	}
	return s[:len(s)-len(n)] + n
}

// TestReopenShadowCountsWouldSkipButStillReopensLive: a succeeded holder
// whose generation-bound watermark postdates the origin pass reports the
// shadow outcome, but the row still goes pending exactly as today (PR1
// enables no skip).
func TestReopenShadowCountsWouldSkipButStillReopensLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := fanOutLiveStore(db)
	fanOutSeedScope(t, ctx, db, "h-shadow", "g1")
	gen := fanOutGen("h-shadow", "g1")

	// Two full evidence-complete passes: the watermark holds the second
	// token, and the origin carries the first.
	first := shadowWatermarkWrite(t, ctx, db, "intent:shadow:1", "h-shadow", gen)
	second := shadowWatermarkWrite(t, ctx, db, "intent:shadow:2", "h-shadow", gen)
	if second <= first {
		t.Fatalf("watermark tokens %d -> %d, want strictly increasing", first, second)
	}

	id := fanOutSeedItem(t, ctx, db, "h-shadow", "g1")
	shadowSucceed(t, ctx, db, id)
	before := widenRead(t, ctx, db, id)

	reopen, err := store.ReopenHolderDetailed(ctx, "h-shadow", shadowOrigin(first, widenCVEB))
	if err != nil {
		t.Fatalf("ReopenHolderDetailed() error = %v, want nil", err)
	}
	if reopen.Outcome != supplychaincore.FanOutShadowWouldSkip {
		t.Fatalf("outcome = %q, want shadow_would_skip (watermark %d > origin %d at the item generation)", reopen.Outcome, second, first)
	}
	// PR1 still reopens: the row goes pending with the incoming stamp, like
	// any reopened holder.
	after := widenRead(t, ctx, db, id)
	if after.status != "pending" || after.attempts != 0 || after.flag {
		t.Fatalf("row = %+v, want pending, attempt 0, no replay flag: shadow never skips in PR1", after)
	}
	if want := `["` + widenCVEB + `"]`; after.cves != want {
		t.Fatalf("stamp cves = %s, want %s", after.cves, want)
	}
	if !after.updatedAt.After(before.updatedAt) {
		t.Fatalf("updated_at %v -> %v, want the reopen to move it as today", before.updatedAt, after.updatedAt)
	}
	if item := fanOutItem(t, ctx, db, id); !item.reopened {
		t.Fatal("reopened_at unset: a shadow report must still reopen the row in PR1")
	}
}

// TestReopenShadowFailClosedMatrixLive runs every decision-1 negative and
// ambiguous case: each must reopen (or flag, or stay dead) exactly as today,
// never shadow. Every case gets its own scope: the queue identity is one
// item per (scope, generation), so scopes cannot share.
func TestReopenShadowFailClosedMatrixLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := fanOutLiveStore(db)

	// matrixWatermark primes a scope with two full evidence-complete passes
	// and returns the older and newer admitted tokens: the watermark holds
	// the newer, so an origin carrying the older is strictly behind it.
	matrixWatermark := func(scope string) (older, newer int64) {
		fanOutSeedScope(t, ctx, db, scope, "g1")
		gen := fanOutGen(scope, "g1")
		older = shadowWatermarkWrite(t, ctx, db, "intent:"+scope+":1", scope, gen)
		newer = shadowWatermarkWrite(t, ctx, db, "intent:"+scope+":2", scope, gen)
		if newer <= older {
			t.Fatalf("watermark tokens %d -> %d, want strictly increasing", older, newer)
		}
		return older, newer
	}

	// A scope with only a partial pass records stuck state but no watermark.
	fanOutSeedScope(t, ctx, db, "h-matrix-partial", "g1")
	pgen := fanOutGen("h-matrix-partial", "g1")
	partial := fenceWrite("intent:matrix:partial", "h-matrix-partial", pgen, fallbackFinding("CVE-2026-9101", "pkg:npm/a@1"))
	partial.PartialEvidence = true
	partial.PartialCauses = []string{"evidence_budget"}
	pwriter := supplychaincore.PostgresSupplyChainImpactWriter{
		DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
		Now: func() time.Time { return replaceSetLiveNow },
	}
	if _, err := pwriter.WriteSupplyChainImpactFindings(ctx, partial); err != nil {
		t.Fatalf("partial write: %v", err)
	}

	// A scope watermarked at gen-1 whose item sits at the active gen-2.
	fanOutSeedScope(t, ctx, db, "h-matrix-mismatch", "g1")
	mgen1 := fanOutGen("h-matrix-mismatch", "g1")
	_ = shadowWatermarkWrite(t, ctx, db, "intent:matrix:mm1", "h-matrix-mismatch", mgen1)
	advanceFenceScope(t, ctx, db, "h-matrix-mismatch", mgen1, fanOutGen("h-matrix-mismatch", "g2"), replaceSetLiveNow)

	unsafe := recovery.UnsafeReplayFailureClasses()
	if len(unsafe) == 0 {
		t.Fatal("no unsafe-replay failure classes: the dead-letter matrix needs one")
	}

	older, _ := matrixWatermark("h-matrix")
	owedOlder, _ := matrixWatermark("h-matrix-owed")
	remainingOlder, _ := matrixWatermark("h-matrix-remaining")
	_, _ = matrixWatermark("h-matrix-setless")
	_, _ = matrixWatermark("h-matrix-notoken")
	claimedOlder, _ := matrixWatermark("h-matrix-claimed")
	_, equalNewer := matrixWatermark("h-matrix-equal")
	olderOlder, _ := matrixWatermark("h-matrix-older")
	sentinelOlder, _ := matrixWatermark("h-matrix-sentinel")
	// Dead-letter scopes need no watermark: a non-succeeded status fails
	// the shadow predicate before any token is compared.
	fanOutSeedScope(t, ctx, db, "h-matrix-dead-safe", "g1")
	fanOutSeedScope(t, ctx, db, "h-matrix-dead-unsafe", "g1")

	for _, tc := range []struct {
		name          string
		scope         string
		prepare       func(id string)
		gen           string
		origin        supplychaincore.FanOutOrigin
		want          supplychaincore.FanOutOutcome
		wantStamp     bool
		wantStatus    string
		wantUntouched bool
	}{
		{
			name:       "eligible control shadows",
			scope:      "h-matrix",
			gen:        "g1",
			origin:     shadowOrigin(older, widenCVEB),
			want:       supplychaincore.FanOutShadowWouldSkip,
			wantStamp:  true,
			wantStatus: "pending",
		},
		{
			name:       "partial pass state never shadows",
			scope:      "h-matrix-partial",
			gen:        "g1",
			origin:     shadowOrigin(1, widenCVEB),
			want:       supplychaincore.FanOutReopened,
			wantStamp:  true,
			wantStatus: "pending",
		},
		{
			name:  "owed marker never shadows",
			scope: "h-matrix-owed",
			gen:   "g1",
			prepare: func(id string) {
				execSQL(t, ctx, db, `UPDATE fact_work_items SET payload = payload || '{"fanout_owed": true}'::jsonb WHERE work_item_id = $1`, id)
			},
			origin:     shadowOrigin(owedOlder, widenCVEB),
			want:       supplychaincore.FanOutReopened,
			wantStamp:  true,
			wantStatus: "pending",
		},
		{
			name:  "remaining list never shadows",
			scope: "h-matrix-remaining",
			gen:   "g1",
			prepare: func(id string) {
				execSQL(t, ctx, db, `UPDATE fact_work_items SET payload = payload || '{"fanout_remaining": ["scope:owed-a"]}'::jsonb WHERE work_item_id = $1`, id)
			},
			origin:     shadowOrigin(remainingOlder, widenCVEB),
			want:       supplychaincore.FanOutReopened,
			wantStamp:  true,
			wantStatus: "pending",
		},
		{
			name:  "safe dead letter reopens genuine without a stamp",
			scope: "h-matrix-dead-safe",
			gen:   "g1",
			prepare: func(id string) {
				deadLetterSeed(t, ctx, db, id, "")
			},
			origin:     shadowOrigin(1, widenCVEB),
			want:       supplychaincore.FanOutDeadLetterReopened,
			wantStamp:  false,
			wantStatus: "pending",
		},
		{
			name:  "unsafe dead letter stays dead",
			scope: "h-matrix-dead-unsafe",
			gen:   "g1",
			prepare: func(id string) {
				deadLetterSeed(t, ctx, db, id, unsafe[0])
			},
			origin:        shadowOrigin(1, widenCVEB),
			want:          supplychaincore.FanOutDeadLetterSkipped,
			wantStamp:     false,
			wantStatus:    "dead_letter",
			wantUntouched: true,
		},
		{
			name:       "set-less origin never shadows",
			scope:      "h-matrix-setless",
			gen:        "g1",
			origin:     fanOutLiveOrigin,
			want:       supplychaincore.FanOutReopened,
			wantStamp:  false,
			wantStatus: "pending",
		},
		{
			name:  "token-less origin never shadows",
			scope: "h-matrix-notoken",
			gen:   "g1",
			origin: supplychaincore.FanOutOrigin{
				SourceScopeID: "scope:shadow-origin", SourceGenerationID: "gen-shadow",
				SourceIntentID: "intent-shadow", CVEIDs: []string{widenCVEB},
			},
			want:       supplychaincore.FanOutReopened,
			wantStamp:  true,
			wantStatus: "pending",
		},
		{
			name:       "watermark generation mismatch never shadows",
			scope:      "h-matrix-mismatch",
			gen:        "g2",
			origin:     shadowOrigin(1, widenCVEB),
			want:       supplychaincore.FanOutReopened,
			wantStamp:  true,
			wantStatus: "pending",
		},
		{
			name:  "claimed holder takes the flag path, never shadows",
			scope: "h-matrix-claimed",
			gen:   "g1",
			prepare: func(id string) {
				execSQL(t, ctx, db, `UPDATE fact_work_items
SET status = 'claimed', lease_owner = 'matrix-worker', claim_until = now() + interval '1 hour', last_attempt_at = now()
WHERE work_item_id = $1`, id)
			},
			origin:     shadowOrigin(claimedOlder, widenCVEB),
			want:       supplychaincore.FanOutReplayFlagged,
			wantStamp:  true,
			wantStatus: "claimed",
		},
		{
			name:       "origin token at the watermark never shadows",
			scope:      "h-matrix-equal",
			gen:        "g1",
			origin:     shadowOrigin(equalNewer, widenCVEB),
			want:       supplychaincore.FanOutReopened,
			wantStamp:  true,
			wantStatus: "pending",
		},
		{
			name:       "older origin token shadows, newer does not",
			scope:      "h-matrix-older",
			gen:        "g1",
			origin:     shadowOrigin(olderOlder, widenCVEB),
			want:       supplychaincore.FanOutShadowWouldSkip,
			wantStamp:  true,
			wantStatus: "pending",
		},
		{
			name:       "sentinel origin is skip-eligible",
			scope:      "h-matrix-sentinel",
			gen:        "g1",
			origin:     shadowOrigin(sentinelOlder, supplychaincore.FanOutStampSentinel),
			want:       supplychaincore.FanOutShadowWouldSkip,
			wantStamp:  true,
			wantStatus: "pending",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := fanOutSeedItem(t, ctx, db, tc.scope, tc.gen)
			var before widenRow
			if tc.scope == "h-matrix-dead-safe" || tc.scope == "h-matrix-dead-unsafe" {
				// Dead-letter seeds set their own status and payload
				// through prepare below; the seeded item stays pending
				// until then.
			} else {
				shadowSucceed(t, ctx, db, id)
			}
			before = widenRead(t, ctx, db, id)
			if tc.prepare != nil {
				tc.prepare(id)
				before = widenRead(t, ctx, db, id)
			}
			reopen, err := store.ReopenHolderDetailed(ctx, tc.scope, tc.origin)
			if err != nil {
				t.Fatalf("ReopenHolderDetailed() error = %v, want nil", err)
			}
			if reopen.Outcome != tc.want {
				t.Fatalf("outcome = %q, want %q", reopen.Outcome, tc.want)
			}
			after := widenRead(t, ctx, db, id)
			if after.status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", after.status, tc.wantStatus)
			}
			_, present := fanOutStampCVEs(t, ctx, db, id)
			if present != tc.wantStamp {
				t.Fatalf("stamp present = %v, want %v", present, tc.wantStamp)
			}
			if tc.wantUntouched && after.version != before.version {
				t.Fatalf("row version %s -> %s: an unsafe dead letter must be locked and classified, never written", before.version, after.version)
			}
		})
	}

	// The sentinel shadow case must carry the sentinel, not a bounded set:
	// a fresh succeeded item on the sentinel scope reopens shadowed.
	fanOutSeedScope(t, ctx, db, "h-matrix-sentinel-stamp", "g1")
	sgen := fanOutGen("h-matrix-sentinel-stamp", "g1")
	stale := shadowWatermarkWrite(t, ctx, db, "intent:sentinel-stamp:1", "h-matrix-sentinel-stamp", sgen)
	fresh := shadowWatermarkWrite(t, ctx, db, "intent:sentinel-stamp:2", "h-matrix-sentinel-stamp", sgen)
	_ = fresh
	sid := fanOutSeedItem(t, ctx, db, "h-matrix-sentinel-stamp", "g1")
	shadowSucceed(t, ctx, db, sid)
	if _, err := store.ReopenHolderDetailed(ctx, "h-matrix-sentinel-stamp", shadowOrigin(stale, supplychaincore.FanOutStampSentinel)); err != nil {
		t.Fatalf("sentinel reopen: %v", err)
	}
	if got, _ := fanOutStampCVEs(t, ctx, db, sid); !slices.Equal(got, []string{supplychaincore.FanOutStampSentinel}) {
		t.Fatalf("sentinel shadow stamp = %v, want the sentinel", got)
	}
}

func execSQL(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

// TestScopePassStateWatermarkIsGenerationBoundLive proves the ruling's
// durability demand through the production writer: only a full
// evidence-complete pass moves the watermark, it names the generation the
// token describes, and a stale-generation pass cannot move it.
func TestScopePassStateWatermarkIsGenerationBoundLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	scope := "wm-scope"
	fanOutSeedScope(t, ctx, db, scope, "g1")
	gen1 := fanOutGen(scope, "g1")
	gen2 := fanOutGen(scope, "g2")
	keep := fallbackFinding("CVE-2026-9101", "pkg:npm/a@1")
	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
		Now: func() time.Time { return replaceSetLiveNow },
	}
	write := func(intent string, partial bool, scoped []string, generation string) {
		t.Helper()
		in := fenceWrite(intent, scope, generation, keep)
		in.PartialEvidence = partial
		if partial {
			in.PartialCauses = []string{"evidence_budget"}
		}
		in.ScopedCVEIDs = scoped
		if _, err := writer.WriteSupplyChainImpactFindings(ctx, in); err != nil {
			t.Fatalf("write %s: %v", intent, err)
		}
	}

	// A partial pass records stuck state but no watermark: there is no
	// proven-complete pass to compare an origin against yet.
	write("intent:wm:partial-1", true, nil, gen1)
	if token, generation, complete := fenceWatermark(t, ctx, db, scope); token.Valid || generation.Valid || complete.Valid {
		t.Fatalf("watermark after partial = (%v, %v, %v), want all NULL", token, generation, complete)
	}

	// The first full evidence-complete pass anchors the watermark at its
	// generation with its token.
	t1 := shadowWatermarkWrite(t, ctx, db, "intent:wm:full-1", scope, gen1)
	if token, generation, complete := fenceWatermark(t, ctx, db, scope); !token.Valid || token.Int64 != t1 || !generation.Valid || generation.String != gen1 || !complete.Valid || !complete.Bool {
		t.Fatalf("watermark = (%v, %v, %v), want (%d, %s, true)", token, generation, complete, t1, gen1)
	}

	// A later partial pass marks stuck state again but must not move the
	// proven watermark: fail-closed keeps comparing against the last
	// complete pass, not the latest attempt.
	write("intent:wm:partial-2", true, nil, gen1)
	if token, generation, _ := fenceWatermark(t, ctx, db, scope); !token.Valid || token.Int64 != t1 || !generation.Valid || generation.String != gen1 {
		t.Fatalf("partial moved the watermark to (%v, %v), want (%d, %s) kept", token, generation, t1, gen1)
	}

	// A scoped complete pass re-derives only its stamped subset, so it
	// leaves both the stuck state and the watermark untouched.
	write("intent:wm:scoped-1", false, []string{"CVE-2026-9101"}, gen1)
	if token, generation, _ := fenceWatermark(t, ctx, db, scope); !token.Valid || token.Int64 != t1 || !generation.Valid || generation.String != gen1 {
		t.Fatalf("scoped pass moved the watermark to (%v, %v), want (%d, %s) kept", token, generation, t1, gen1)
	}

	// The lifecycle advances past the seed clock (2026-10-31): a stale
	// gen-1 pass admitted late draws a higher token but is
	// generation-fenced, so the watermark stays.
	advanceFenceScope(t, ctx, db, scope, gen1, gen2, time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC))
	t2 := shadowWatermarkWrite(t, ctx, db, "intent:wm:full-2", scope, gen2)
	if token, generation, _ := fenceWatermark(t, ctx, db, scope); !token.Valid || token.Int64 != t2 || !generation.Valid || generation.String != gen2 {
		t.Fatalf("watermark = (%v, %v), want (%d, %s) after the gen-2 full pass", token, generation, t2, gen2)
	}
	stale := fenceWrite("intent:wm:stale-1", scope, gen1, keep)
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, stale); err != nil {
		t.Fatalf("stale gen-1 write: %v", err)
	}
	if token, generation, _ := fenceWatermark(t, ctx, db, scope); !token.Valid || token.Int64 != t2 || !generation.Valid || generation.String != gen2 {
		t.Fatalf("stale pass moved the watermark to (%v, %v), want (%d, %s) kept", token, generation, t2, gen2)
	}
}

// fenceWatermark reads the durable holder watermark: the token of the last
// full evidence-complete pass and the generation it describes. NULLs report
// no proven pass yet and fail every skip closed.
func fenceWatermark(t *testing.T, ctx context.Context, db *sql.DB, scope string) (token sql.NullInt64, generation sql.NullString, complete sql.NullBool) {
	t.Helper()
	if err := db.QueryRowContext(ctx, `SELECT watermark_token, watermark_generation_id, watermark_evidence_complete
FROM supply_chain_impact_scope_pass_state WHERE scope_id = $1`, scope).Scan(&token, &generation, &complete); err != nil {
		t.Fatalf("read watermark: %v", err)
	}
	return token, generation, complete
}

// TestPendingSentinelStaysStickyLive pins the ruling's sentinel rule on the
// pending arm: a stored unbounded stamp already covers any bounded change,
// so the reopen writes nothing (pending_noop) and the sentinel stays, and
// the next pass still runs full. The union arm that keeps the sentinel on a
// rewrite is proven on the succeeded path (stored sentinel stays sticky).
func TestPendingSentinelStaysStickyLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := fanOutLiveStore(db)
	sentinel := widenOriginA
	sentinel.CVEIDs = []string{supplychaincore.FanOutStampSentinel}
	fanOutSeedScope(t, ctx, db, "h-pending-sentinel", "g1")
	id := fanOutSeedItem(t, ctx, db, "h-pending-sentinel", "g1")
	widenSetStamp(t, ctx, db, id, widenStamp(t, sentinel))
	before := widenRead(t, ctx, db, id)

	reopen, err := store.ReopenHolderDetailed(ctx, "h-pending-sentinel", widenOriginB)
	if err != nil {
		t.Fatalf("ReopenHolderDetailed() error = %v, want nil", err)
	}
	if reopen.Outcome != supplychaincore.FanOutPendingNoop {
		t.Fatalf("outcome = %q, want pending_noop: the stored sentinel covers the change, so nothing is written", reopen.Outcome)
	}
	after := widenRead(t, ctx, db, id)
	if after.cves != `["`+supplychaincore.FanOutStampSentinel+`"]` {
		t.Fatalf("stored sentinel + bounded incoming = %s, want the sentinel to stay", after.cves)
	}
	if after.version != before.version {
		t.Fatalf("row version %s -> %s: a covered holder must cost no row version", before.version, after.version)
	}
}
