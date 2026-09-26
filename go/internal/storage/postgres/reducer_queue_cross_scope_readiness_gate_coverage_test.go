// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestReducerContentionPostgresProofsRunInTheReducerContentionGate is the
// hermetic enrollment guard for the live PostgreSQL proofs, and it exists
// because a DSN-gated test that quietly skips in CI proves nothing there.
//
// The live proofs need real PostgreSQL, so they skip on a developer machine
// with no DSN. CI does provide one -- the reducer contention gate runs a
// PostgreSQL service and passes ESHU_POSTGRES_DSN -- but only for the tests its
// -run filter selects. That coupling is a test NAME matching a regex in a YAML
// file, which nothing else checks: rename either side and the proofs stop
// running in CI without a single failure.
//
// This test runs everywhere, needs no database, and reads the real workflow.
func TestReducerContentionPostgresProofsRunInTheReducerContentionGate(t *testing.T) {
	t.Parallel()

	workflowPath := filepath.Join("..", "..", "..", "..", ".github", "workflows", "reducer-contention-gate.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	if !bytes.Contains(workflow, []byte("ESHU_POSTGRES_DSN:")) {
		t.Fatalf("%s no longer passes a PostgreSQL DSN: the live proofs would skip in CI", workflowPath)
	}
	if !bytes.Contains(workflow, []byte("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN:")) ||
		!bytes.Contains(workflow, []byte("ESHU_GENERATION_LIVENESS_PROOF_DSN:")) {
		t.Fatalf("%s must pass both projector and generation-liveness proof DSNs", workflowPath)
	}
	if !bytes.Contains(workflow, []byte("ESHU_REDUCER_ACK_RECLAIM_PROOF_DSN:")) {
		t.Fatalf("%s must pass the reducer ack reclaim proof DSN (#6162)", workflowPath)
	}
	if !bytes.Contains(workflow, []byte("ESHU_SUPERSEDED_GENERATION_PROOF_DSN:")) {
		t.Fatalf("%s must pass the superseded-generation proof DSN (#7121)", workflowPath)
	}
	// #6679: a skip is a failure in this lane for the acceptance monotonic
	// proofs, so a renamed DSN variable cannot silently disable them.
	if !bytes.Contains(workflow, []byte(acceptanceMonotonicRequiredEnv+": \"1\"")) {
		t.Fatalf("%s must set %s=1 so the acceptance monotonic proofs cannot skip in CI", workflowPath, acceptanceMonotonicRequiredEnv)
	}
	// #6475: a skip is a failure in this lane for the service lineage scope
	// proofs, so a renamed DSN variable cannot silently disable them.
	if !bytes.Contains(workflow, []byte(serviceLineageScopeRequiredEnv+": \"1\"")) {
		t.Fatalf("%s must set %s=1 so the service lineage scope proofs cannot skip in CI", workflowPath, serviceLineageScopeRequiredEnv)
	}
	// #6809: a skip is a failure in this lane for the migrated-schema retention
	// proof, so a renamed DSN variable cannot silently disable it.
	if !bytes.Contains(workflow, []byte(generationRetentionMigratedSchemaRequiredEnv+": \"1\"")) {
		t.Fatalf("%s must set %s=1 so the migrated-schema retention proof cannot skip in CI", workflowPath, generationRetentionMigratedSchemaRequiredEnv)
	}
	if !bytes.Contains(workflow, []byte("TestReducerContentionPostgresProofsRunInTheReducerContentionGate")) {
		t.Fatalf("%s no longer names this live-proof enrollment guard; update the guard reference in lockstep", workflowPath)
	}

	// #6794: the story target-support semantics proof lives in internal/query
	// and runs as its own step of this gate, against the same Postgres.
	if !bytes.Contains(workflow, []byte("go test ./internal/query/ -run '^TestServiceStoryTargetSupportSQLSemanticsLive$'")) {
		t.Fatalf("%s no longer runs TestServiceStoryTargetSupportSQLSemanticsLive against Postgres", workflowPath)
	}

	runFilter := reducerContentionGateRunFilter(t, string(workflow))
	selects, err := regexp.Compile(runFilter)
	if err != nil {
		t.Fatalf("compile the gate's -run filter %q: %v", runFilter, err)
	}
	for _, name := range []string{
		"TestReducerContentionGateActiveCodeCallSymbolLoaderCrossRepository",
		"TestReducerContentionGateCrossScopeReadinessDeferralKeepsItsAttemptBudget",
		"TestReducerContentionGateCrossScopeReadinessConvergesAtTheElapsedBound",
		"TestDeferredBackfillSharedScopeGenerationPublishesOneRowPerPartition",
		"TestDeferredBackfillDistinctScopesPublishOneRowEach",
		"TestDeferredBackfillWithholdsPublicationWhenSiblingBatchFails",
		"TestDeferredBackfillWithholdsPublicationWhenSiblingBatchCanceled",
		"TestDeferredBackfillPublishesOncePerPartitionAcrossBatches",
		"TestDeferredBackfillFanInSkipsPartitionWhoseGenerationAdvanced",
		"TestDeferredBackfillFanInFailureLeavesEvidenceRecoverable",
		"TestDeferredBackfillCrashBetweenBatchesAndFanInConverges",
		"TestFanInActiveGenerationMatchesCorpusLoader",
		// #6794: the status snapshot's single active-work statement must decode
		// to exactly what the pre-change standalone reads return, and the
		// stale-generation/backlog contract is pinned on real Postgres.
		"TestActiveWorkSummaryMatchesStandaloneReads",
		"TestStatusActiveWorkQueriesPreserveSemantics",
		"TestActiveFactWorkItemsFormsSelectTheSameRows",
		"TestProjectorHeartbeatSupersessionPreservesActivePointer",
		"TestProjectorHeartbeatRenewsLeaseWhileIngestionHoldsScope",
		"TestProjectorAckDefersWhileIngestionHoldsScope",
		"TestProjectorQueueRejectsReclaimedSameOwnerAttempt",
		"TestProjectorQueueRejectsAttemptReclaimedDuringLockWait",
		"TestProjectorCompletionDoesNotDeadlockSameGenerationCommit",
		"TestPublishedGenerationRecommitIsIdempotentLive",
		"TestFinalizedGenerationRecommitSkipsFactsLive",
		"TestGenerationLivenessIntegration",
		"TestSharedIntentGenerationPendingIndexLifecycleLive",
		// #6794: the status blockage filter must match the pre-change join over
		// every lease state and must hash the lease set, running no eligible or
		// lease scan more than once, whatever the statistics say.
		"TestReducerConflictBlockageLeaseMixMatchesPreChangeJoin",
		"TestActiveWorkSummaryBlockageHashesLeasesOnce",
		// #6162: a lease that expires mid-handler puts two claims of one work
		// item in the same ack batch. The batch must ack the surviving claim
		// and fence the superseded one instead of failing and stopping the
		// reducer. Only real Postgres expires a lease on the wall clock and
		// applies the ack statement's last_attempt_at fence.
		"TestReducerQueueAckBatchFencesSupersededClaimLive",
		// #7121: the superseded-generation drain lookup must defer to in-flight
		// producers and durable phase-repair rows. Only real Postgres executes
		// the NOT EXISTS safety predicate, so a skipped proof leaves it untested.
		"TestSupersededGenerationIDsAgainstPostgres",
		"TestSupersededGenerationIDsDefersToInFlightProducersAgainstPostgres",
		"TestSupersededGenerationIDsDefersToPhaseRepairRowsAgainstPostgres",
		// #6679: shared-projection acceptance must never move to an older
		// generation, whatever the commit order or snapshot timing.
		"TestSharedProjectionAcceptanceSequentialStaleWriteLive",
		"TestSharedProjectionAcceptanceConcurrentOutOfOrderLive",
		"TestSharedProjectionAcceptancePostSnapshotStoredGenerationLive",
		"TestSharedProjectionAcceptanceOrdersByIngestedAtLive",
		"TestSharedProjectionAcceptanceLegacyNullKeyRejectsStaleLive",
		"TestSharedProjectionAcceptanceLegacyNullKeyAdvancesLive",
		"TestSharedProjectionAcceptanceLegacyNullKeyInvisibleGenerationAdvancesLive",
		"TestSharedIntentAcceptanceWriterReversedBatchesDoNotDeadlockLive",
		"TestSharedIntentAcceptanceWriterStaleWriteCounterLive",
		// #6475: the service lineage is keyed by (scope_id, service_id); the
		// migrations, writer, and changed-since resolve pick need real Postgres.
		"TestServiceMaterializationActiveIndexReplayConvergesLive",
		"TestServiceMaterializationWriterKeepsScopedLineagesLive",
		"TestServiceChangedSinceResolvePicksAttributedNewestActiveLive",
		// #6809: the generation retention statements must prepare and run
		// against the real migrated schema, not a hand-written fixture.
		"TestGenerationRetentionStatementsPrepareAgainstMigratedSchemaLive",
		"TestGenerationRetentionPrunesMigratedSchemaLive",
		"TestGenerationRetentionContentPrunesDeleteExactRowsLive",
		"TestGenerationRetentionContentPrunesFinishWithoutPlannerStatisticsLive",
	} {
		if !selects.MatchString(name) {
			t.Fatalf("the reducer contention gate's -run filter %q does not select %s", runFilter, name)
		}
	}
}

// reducerContentionGateRunFilter extracts the single-quoted -run pattern from
// the workflow's test command, so this test reads the value CI actually uses
// rather than a copy of it.
func reducerContentionGateRunFilter(t *testing.T, workflow string) string {
	t.Helper()

	matches := regexp.MustCompile(`-run '([^']+)'`).FindStringSubmatch(workflow)
	if len(matches) != 2 {
		t.Fatal("the reducer contention gate no longer runs a single-quoted -run filter; this guard cannot read it")
	}
	return matches[1]
}
