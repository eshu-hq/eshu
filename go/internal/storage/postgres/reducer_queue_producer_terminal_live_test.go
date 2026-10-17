// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7597 own gate bounded by its deployment_mapping producer through the real ReducerQueue claim/Fail SQL and the production Postgres lookups: a dead-lettered producer dead-letters the deployable-unit and workload consumers within MaxAttempts in the counting producer-terminal class, a producer retrying on backward evidence keeps them non-counting with attempt_count frozen; isolated schema per subtest, about 6 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
)

// The #7597 own-gate live proofs drive the real ReducerQueue claim/Fail SQL
// and the real own gate (deployable-unit handler and workload loader wired
// with the production Postgres lookups) against one isolated schema. Gated on
// ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN like the other queue
// proofs in this package.

// producerTerminalFactLoader returns one Dockerfile candidate so the own gate
// has candidates to guard and is not vacuous.
type producerTerminalFactLoader struct{}

func (producerTerminalFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	now := time.Now().UTC()
	return []facts.Envelope{
		{
			FactID: "fact-repo", FactKind: "repository", ObservedAt: now,
			Payload: map[string]any{"graph_id": "repo-edge-api", "name": "edge-api"},
		},
		{FactID: "fact-file", FactKind: "file", ObservedAt: now, Payload: map[string]any{
			"repo_id": "repo-edge-api", "language": "dockerfile", "relative_path": "Dockerfile",
			"parsed_file_data": map[string]any{"dockerfile_stages": []any{map[string]any{"name": "runtime"}}},
		}},
	}, nil
}

// producerTerminalCountingError is a counting failure: Retryable, no class.
type producerTerminalCountingError struct{}

func (producerTerminalCountingError) Error() string   { return "neo4j write timeout (counting)" }
func (producerTerminalCountingError) Retryable() bool { return true }

// ownGateConsumer runs one consumer pass through the production own gate.
type ownGateConsumer struct {
	domain reducer.Domain
	class  string
	run    func(context.Context, reducer.Intent) error
}

func ownGateConsumers(db *sql.DB) []ownGateConsumer {
	store := NewRelationshipStore(SQLDB{DB: db})
	active := NewRelationshipGenerationActiveLookup(store)
	producer := NewResolutionProducerLookup(SQLDB{DB: db})
	duc := reducer.DeployableUnitCorrelationHandler{
		FactLoader: producerTerminalFactLoader{}, ResolvedLoader: store,
		ResolutionActiveLookup: active, ResolutionProducerLookup: producer,
	}
	wm := reducer.CorrelatedWorkloadProjectionInputLoader{
		FactLoader: producerTerminalFactLoader{}, ResolvedLoader: store,
		ResolutionActiveLookup: active, ResolutionProducerLookup: producer,
	}
	return []ownGateConsumer{
		{
			domain: reducer.DomainDeployableUnitCorrelation,
			class:  crossrepo.DeployableUnitCorrelationProducerDeadLetteredFailureClass,
			run: func(ctx context.Context, intent reducer.Intent) error {
				_, err := duc.Handle(ctx, intent)
				return err
			},
		},
		{
			domain: reducer.DomainWorkloadMaterialization,
			class:  crossrepo.WorkloadMaterializationProducerDeadLetteredFailureClass,
			run: func(ctx context.Context, intent reducer.Intent) error {
				_, err := wm.LoadWorkloadProjectionScopeInputs(ctx, intent)
				return err
			},
		},
	}
}

// failDeploymentMapping claims the scope's deployment_mapping row and fails it
// with cause `times` times, advancing the clock past each retry delay.
func failDeploymentMapping(t *testing.T, ctx context.Context, db *sql.DB, clock *time.Time, cause error, times int) {
	t.Helper()
	q := newOwnGateQueue(db, clock, reducer.DomainDeploymentMapping)
	for i := 0; i < times; i++ {
		intent, ok, err := q.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("deployment_mapping claim %d: ok=%v err=%v", i, ok, err)
		}
		if err := q.Fail(ctx, intent, cause); err != nil {
			t.Fatalf("deployment_mapping fail %d: %v", i, err)
		}
		*clock = clock.Add(2 * time.Hour)
	}
}

// readProducerTerminalRow reads the queue state the proofs assert.
func readProducerTerminalRow(t *testing.T, ctx context.Context, db *sql.DB, id string) (string, string, string, int, time.Time) {
	t.Helper()
	var status string
	var class, message sql.NullString
	var attempts int
	var visible sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT status, failure_class, failure_message, attempt_count, visible_at
FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&status, &class, &message, &attempts, &visible); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return status, class.String, message.String, attempts, visible.Time
}

// TestOwnGateProducerDeadLetteredConsumerDeadLettersLive is RED on main: there
// the consumer retries forever on its non-counting deferral with
// attempt_count frozen at 1. With the producer lookup wired, the consumer fails
// in the counting producer-terminal class and dead-letters within MaxAttempts.
func TestOwnGateProducerDeadLetteredConsumerDeadLettersLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #7597 own-gate proof")
	}
	ctx := context.Background()
	for _, domain := range ownGateConsumerDomains {
		t.Run(string(domain), func(t *testing.T) {
			db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
			consumer := consumerFor(db, domain)
			clock := time.Now().UTC()
			const scope, gen = "git-repository-scope:acme/app", "gen-1"
			seedOwnGateScope(t, ctx, db, scope, gen, clock.Add(-time.Hour))
			addOwnGateItem(t, ctx, db, "dm-1", scope, gen, reducer.DomainDeploymentMapping, clock.Add(-time.Minute))
			addOwnGateItem(t, ctx, db, "consumer-1", scope, gen, consumer.domain, clock.Add(-time.Minute))
			failDeploymentMapping(t, ctx, db, &clock, producerTerminalCountingError{}, 3)
			if st, _, _, _, _ := readProducerTerminalRow(t, ctx, db, "dm-1"); st != "dead_letter" {
				t.Fatalf("deployment_mapping status = %s, want dead_letter", st)
			}

			q := newOwnGateQueue(db, &clock, consumer.domain)
			for cycle := 1; cycle <= q.MaxAttempts+2; cycle++ {
				intent, ok, err := q.Claim(ctx)
				if err != nil {
					t.Fatalf("cycle %d claim: %v", cycle, err)
				}
				if !ok {
					break
				}
				if err := q.Fail(ctx, intent, consumer.run(ctx, intent)); err != nil {
					t.Fatalf("cycle %d fail: %v", cycle, err)
				}
				st, cls, msg, att, vis := readProducerTerminalRow(t, ctx, db, "consumer-1")
				t.Logf("cycle %d: status=%s class=%s attempts=%d", cycle, st, cls, att)
				if st == "dead_letter" {
					if cls != consumer.class {
						t.Fatalf("dead-letter class = %q, want %q", cls, consumer.class)
					}
					if !strings.Contains(msg, "dm-1") {
						t.Fatalf("failure_message = %q, want the producer work item dm-1", msg)
					}
					if att != q.MaxAttempts {
						t.Fatalf("attempt_count = %d, want %d (the budget must count)", att, q.MaxAttempts)
					}
					return
				}
				if att != cycle {
					t.Fatalf("cycle %d attempt_count = %d, want it to advance with each counted failure", cycle, att)
				}
				clock = vis.Add(time.Second)
			}
			st, cls, _, att, _ := readProducerTerminalRow(t, ctx, db, "consumer-1")
			t.Fatalf("consumer never dead-lettered: status=%s class=%s attempts=%d", st, cls, att)
		})
	}
}

// TestOwnGateProducerRetryingConsumerKeepsDeferringLive pins the other side:
// a producer that is still live (retrying on backward evidence) keeps the
// consumer in its non-counting class with attempt_count frozen, and the
// deferral message names the producer's status and class.
func TestOwnGateProducerRetryingConsumerKeepsDeferringLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #7597 own-gate proof")
	}
	ctx := context.Background()
	wantClass := map[reducer.Domain]string{
		reducer.DomainDeployableUnitCorrelation: reducer.DeployableUnitCorrelationResolutionNotReadyFailureClass,
		reducer.DomainWorkloadMaterialization:   reducer.WorkloadMaterializationResolutionNotReadyFailureClass,
	}
	for _, domain := range ownGateConsumerDomains {
		t.Run(string(domain), func(t *testing.T) {
			db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
			consumer := consumerFor(db, domain)
			clock := time.Now().UTC()
			const scope, gen = "git-repository-scope:acme/app", "gen-1"
			seedOwnGateScope(t, ctx, db, scope, gen, clock.Add(-time.Hour))
			addOwnGateItem(t, ctx, db, "dm-1", scope, gen, reducer.DomainDeploymentMapping, clock.Add(-time.Minute))
			addOwnGateItem(t, ctx, db, "consumer-1", scope, gen, consumer.domain, clock.Add(-time.Minute))
			failDeploymentMapping(t, ctx, db, &clock,
				crossrepo.BackwardEvidenceNotReadyError{ScopeID: scope, GenerationID: gen}, 5)

			q := newOwnGateQueue(db, &clock, consumer.domain)
			const cycles = 12
			for cycle := 1; cycle <= cycles; cycle++ {
				intent, ok, err := q.Claim(ctx)
				if err != nil || !ok {
					t.Fatalf("cycle %d claim: ok=%v err=%v", cycle, ok, err)
				}
				if err := q.Fail(ctx, intent, consumer.run(ctx, intent)); err != nil {
					t.Fatalf("cycle %d fail: %v", cycle, err)
				}
				st, cls, msg, att, vis := readProducerTerminalRow(t, ctx, db, "consumer-1")
				if st != "retrying" || cls != wantClass[consumer.domain] || att != 1 {
					t.Fatalf("cycle %d: status=%s class=%s attempts=%d, want retrying/%s/1",
						cycle, st, cls, att, wantClass[consumer.domain])
				}
				for _, want := range []string{"dm-1", "status=retrying", crossrepo.CrossRepoBackwardEvidenceNotReadyFailureClass} {
					if !strings.Contains(msg, want) {
						t.Fatalf("cycle %d failure_message = %q, want it to name %q", cycle, msg, want)
					}
				}
				clock = vis.Add(time.Second)
			}
		})
	}
}

// ownGateConsumerDomains are the two domains whose own gate reads the
// deployment_mapping producer.
var ownGateConsumerDomains = []reducer.Domain{
	reducer.DomainDeployableUnitCorrelation,
	reducer.DomainWorkloadMaterialization,
}

// seedOwnGateScope inserts an active repository scope whose active generation
// is genID.
func seedOwnGateScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, genID string, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, NULL, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`, scopeID, at, genID); err != nil {
		t.Fatalf("seed scope %s: %v", scopeID, err)
	}
	addOwnGateGeneration(t, ctx, db, scopeID, genID, at, "active")
}

// addOwnGateGeneration inserts one scope generation row.
func addOwnGateGeneration(t *testing.T, ctx context.Context, db *sql.DB, scopeID, genID string, at time.Time, status string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload)
VALUES ($1, $2, 'snapshot', 'x', $3, $3, $4, $3, NULL, '{}'::jsonb)`, genID, scopeID, at, status); err != nil {
		t.Fatalf("seed generation %s: %v", genID, err)
	}
}

// addOwnGateItem inserts one pending reducer work item.
func addOwnGateItem(t *testing.T, ctx context.Context, db *sql.DB, id, scopeID, genID string, domain reducer.Domain, at time.Time) {
	t.Helper()
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID: id, scopeID: scopeID, generationID: genID, domain: string(domain),
		conflictDomain: "scope", conflictKey: id, sourceSystem: "git", updatedAt: at,
	})
}

// newOwnGateQueue is a real ReducerQueue on db with a test clock.
func newOwnGateQueue(db *sql.DB, clock *time.Time, domains ...reducer.Domain) ReducerQueue {
	return ReducerQueue{
		database: SQLDB{DB: db}, LeaseOwner: "own-gate-7597", LeaseDuration: 5 * time.Minute,
		RetryDelay: 30 * time.Second, MaxRetryDelay: time.Hour, JitterFraction: 0.1, MaxAttempts: 3,
		ClaimDomains: domains, Now: func() time.Time { return *clock },
	}
}

// consumerFor rebinds a consumer's run function to db.
func consumerFor(db *sql.DB, domain reducer.Domain) ownGateConsumer {
	for _, consumer := range ownGateConsumers(db) {
		if consumer.domain == domain {
			return consumer
		}
	}
	panic("unknown own-gate consumer " + string(domain))
}
