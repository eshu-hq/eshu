// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// waitingWakeCensusRegistry is the #11228 census of every generation_active
// and producer_domain_terminal wake call site (ruling decision 1, condition
// 3). An in-tx site re-wakes after its commit; every other site states why
// the park/provider window cannot occur there. A new site, or a site moved
// onto an open transaction, fails TestWaitingWakeCensus.
var waitingWakeCensusRegistry = map[string]wakeCensusEntry{
	// The projector Ack activates the generation and wakes inside its
	// transaction, then re-runs the same wakes after Commit (#11228).
	"projector_queue_ack.go:Ack": {class: wakeCensusInTxRewoken, rewake: "wakeAckWaitsAfterCommit"},
	"reducer_waiting_generation_wake.go:wakeAckWaitsAfterCommitWithin": {
		class:  wakeCensusPostCommit,
		reason: "the Ack's post-commit re-wake itself: it runs on q.database after tx.Commit returned nil",
	},
	"projector_queue.go:Fail": {
		class: wakeCensusPostCommit,
		reason: "the terminal Fail marks the generation failed inside failProjectorWorkQuery, one autocommit " +
			"statement on q.database; the wake starts after that statement returned, so a park re-check that " +
			"starts later sees the failed generation and a wait row committed earlier is visible to the wake",
	},
	"projector_queue_scan.go:claimOnce": {
		class: wakeCensusPostCommit,
		reason: "the claim sweep supersedes generations inside claimProjectorWorkQuery, one autocommit statement " +
			"on q.database; the claimed probe starts after rows.Close, and the empty probe after rows.Next " +
			"returned false, which in pgx v5 closes the result through ReadyForQuery, sent only after the " +
			"server committed the implicit transaction",
	},
	"projector_queue_scan.go:supersedeRunningWork": {
		class: wakeCensusPostCommit,
		reason: "the heartbeat supersede turns the generation terminal inside supersedeRunningProjectorWorkQuery, " +
			"one autocommit statement on q.database; the wake starts after rows.Close completed it",
	},
	"reducer_queue.go:Ack": {
		class: wakeCensusPostCommit,
		reason: "the reducer Ack is one autocommit statement on q.database; the producer-terminal wake starts " +
			"after it returned",
	},
	"reducer_queue_batch.go:AckBatch": {
		class: wakeCensusPostCommit,
		reason: "the batch Ack statements are autocommit on q.database; the producer-terminal wake starts after " +
			"the last one returned",
	},
	// Forwarders pass their own database parameter on; their callers are
	// censused above.
	"reducer_waiting_store.go:wakeGenerationActiveWaits":                             {class: wakeCensusForwarder},
	"reducer_waiting_generation_wake.go:wakeTerminalGenerationActiveWaits":           {class: wakeCensusForwarder},
	"reducer_waiting_generation_wake.go:wakeTerminalGenerationActiveWaitsBestEffort": {class: wakeCensusForwarder},
	"reducer_waiting_generation_wake.go:wakeGenerationActiveWaitsBestEffort":         {class: wakeCensusForwarder},
	"reducer_waiting_generation_wake.go:wakeAckActivationWaits":                      {class: wakeCensusForwarder},
	"reducer_waiting_producer_terminal.go:wakeProducerTerminalAfterAck":              {class: wakeCensusForwarder},
	"reducer_waiting_producer_terminal.go:wakeProducerTerminalAfterAckWithPlan":      {class: wakeCensusForwarder},
	"reducer_waiting_producer_terminal.go:parkProducerTerminalWaits": {
		class: wakeCensusParker,
		reason: "the parker reopens its own key after its re-check saw the provider: the re-check is the " +
			"parker's read half, not a provider wake",
	},
	"reducer_waiting_narrow.go:setAsideWaitingForKey": {
		class:  wakeCensusParker,
		reason: "the parker reopens its own key after its re-check saw the provider",
	},
	"reducer_waiting_audit.go:AuditWaitingItems": {
		class:  wakeCensusAudit,
		reason: "the slow audit reads committed provider state on the pool and is itself the fallback",
	},
}

// TestWaitingWakeCensus fails when a generation_active or
// producer_domain_terminal wake site is added, moved onto an open
// transaction, or loses its post-commit re-wake without a registered reason
// (#11228, ruling decision 1).
func TestWaitingWakeCensus(t *testing.T) {
	t.Parallel()
	for _, violation := range auditWakeCensus(wakeCensusParseSources(t, "."), waitingWakeCensusRegistry) {
		t.Error(violation)
	}
}

// wakeCensusSeededSource is a fixture package with one in-tx provider wake
// that has no post-commit re-wake, the shape of #11228 before its fix.
const wakeCensusSeededSource = `package postgres

func (q ProjectorQueue) SeededActivate(ctx context.Context) error {
	tx, err := q.beginner.Begin(ctx)
	if err != nil {
		return err
	}
	if err := wakeGenerationActiveWaits(ctx, tx, nil, "in_tx", "scope", "gen", now); err != nil {
		return err
	}
	return tx.Commit()
}

func (q ProjectorQueue) SeededRewoken(ctx context.Context) error {
	tx, err := q.beginner.Begin(ctx)
	if err != nil {
		return err
	}
	if err := wakeGenerationActiveWaits(ctx, tx, nil, "in_tx", "scope", "gen", now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	q.seededRewake(ctx)
	return nil
}

func (q ProjectorQueue) SeededPoolBeforeCommit(ctx context.Context) error {
	tx, err := q.beginner.Begin(ctx)
	if err != nil {
		return err
	}
	wakeGenerationActiveWaitsBestEffort(ctx, q.database, nil, "op", "scope", "gen", now)
	return tx.Commit()
}

func (q ProjectorQueue) SeededPhase(ctx context.Context, tx db.Transaction) error {
	_, _, err := wakeWaitingByKey(ctx, tx, nil, "in_tx", WaitingKindPhaseCommitted, "{}", now)
	return err
}
`

// TestWaitingWakeCensusFlagsSeededSites is the census's seeded RED/GREEN
// pair: the fixture's unregistered in-tx wake is flagged, an in-tx wake
// registered as post_commit is flagged, an in_tx_rewoken site whose re-wake
// is missing is flagged, a pool wake before the function's own commit is
// flagged, and an out-of-scope kind is ignored.
func TestWaitingWakeCensusFlagsSeededSites(t *testing.T) {
	t.Parallel()
	file, err := parser.ParseFile(token.NewFileSet(), "seeded.go", wakeCensusSeededSource, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	files := map[string]*ast.File{"seeded.go": file}
	tests := []struct {
		name     string
		registry map[string]wakeCensusEntry
		want     []string
	}{
		{
			name:     "unregistered",
			registry: map[string]wakeCensusEntry{},
			want: []string{
				"seeded.go:SeededActivate: unregistered", "seeded.go:SeededPoolBeforeCommit: unregistered",
				"seeded.go:SeededRewoken: unregistered",
			},
		},
		{
			name: "misclassified_and_missing_rewake",
			registry: map[string]wakeCensusEntry{
				"seeded.go:SeededActivate":         {class: wakeCensusPostCommit, reason: "wrong"},
				"seeded.go:SeededRewoken":          {class: wakeCensusInTxRewoken, rewake: "seededMissing"},
				"seeded.go:SeededPoolBeforeCommit": {class: wakeCensusPostCommit, reason: "wrong"},
			},
			want: []string{
				"seeded.go:SeededActivate: wakeGenerationActiveWaits wakes on open transaction tx",
				"seeded.go:SeededPoolBeforeCommit: registered post_commit but",
				"seeded.go:SeededRewoken: in-tx wake without a call to seededMissing",
			},
		},
		{
			// The re-woken site passes; only the site without a re-wake
			// after its Commit stays flagged.
			name: "rewoken_site_passes",
			registry: map[string]wakeCensusEntry{
				"seeded.go:SeededActivate":         {class: wakeCensusInTxRewoken, rewake: "seededRewake"},
				"seeded.go:SeededRewoken":          {class: wakeCensusInTxRewoken, rewake: "seededRewake"},
				"seeded.go:SeededPoolBeforeCommit": {class: wakeCensusAudit, reason: "fixture"},
			},
			want: []string{"seeded.go:SeededActivate: in-tx wake without a call to seededRewake"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := auditWakeCensus(files, tt.registry)
			if len(got) != len(tt.want) {
				t.Fatalf("violations = %q, want %d matching %q", got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if !strings.HasPrefix(got[i], tt.want[i]) {
					t.Fatalf("violation %d = %q, want prefix %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
