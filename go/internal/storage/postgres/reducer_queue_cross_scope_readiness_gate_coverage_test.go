// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
	if !activeWorkProjectionProofEnv(t, string(workflow)) {
		t.Fatalf("%s must pass an actual PostgreSQL DSN and require the active-work projection proof in its Postgres test step", workflowPath)
	}
	if !bytes.Contains(workflow, []byte("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN:")) {
		t.Fatalf("%s must pass the projector supersession proof DSN", workflowPath)
	}
	if !bytes.Contains(workflow, []byte("ESHU_GENERATION_LIVENESS_PROOF_DSN:")) {
		t.Fatalf("%s must pass the generation-liveness proof DSN (a selected proof without it skips)", workflowPath)
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
	// #7437: the DSN-substring checks above only prove the workflow sets the
	// variable; they cannot catch a proof the -run filter never selects, which
	// skips just as quietly. Every test that reads
	// ESHU_GENERATION_LIVENESS_PROOF_DSN must be selected by the filter, so a
	// new env-gated proof cannot silently sit out the gate.
	for _, name := range generationLivenessGatedTests(t) {
		if !selects.MatchString(name) {
			t.Fatalf("the reducer contention gate's -run filter %q does not select env-gated %s (reads ESHU_GENERATION_LIVENESS_PROOF_DSN)", runFilter, name)
		}
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
		"TestActiveWorkSummaryDropsTerminalTextFromMaterializedRows",
		// #7009: the gated summary's oracle differential, its mutation
		// controls, the gate's regclass proof, and the plan guard in both
		// gate branches.
		"TestActiveWorkSummaryMatchesPreHistoryGroupsOracle",
		"TestActiveWorkSummaryOracleCatchesSeededHistoryMutations",
		"TestActiveWorkSummaryGateMutationsAgainstOracle",
		"TestActiveWorkSummaryGateResolvesTableByRegclass",
		"TestStatusActiveFactWorkItemsCTEUsesGenerationIndex",
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
		// #7437: the env-gated liveness proofs below read
		// ESHU_GENERATION_LIVENESS_PROOF_DSN and skip without it; the
		// discovery check above also requires each one, so this pinned
		// list and the workflow filter stay in lockstep.
		"TestGenerationLivenessProgressWindow",
		"TestGenerationLivenessRefinalizeDuplicateProjectorRow",
		"TestGenerationLivenessRepoDependencyOwnership",
		"TestRecoverWedgedActiveGenerationsQueryDoesNotClobberConcurrentlyRenewedLease",
		"TestProjectorStrandedRetryRecovery",
		"TestProjectorStrandedRetryRecoveryLeaveLiveLease",
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
		// #7323: the intent upsert overwrites created_at and payload while
		// completed_at only advances; only real Postgres evaluates the
		// ON CONFLICT COALESCE.
		"TestSharedIntentUpsertCreatedAtLastWriterWinsLive",
		// #6475: the service lineage is keyed by (scope_id, service_id); the
		// migrations, writer, and changed-since resolve pick need real Postgres.
		"TestServiceMaterializationActiveIndexReplayConvergesLive",
		"TestServiceMaterializationWriterKeepsScopedLineagesLive",
		"TestServiceChangedSinceResolvePicksAttributedNewestActiveLive",
		// #7258: the service catalog handler must defer on the relationship
		// corpus fence against the real RelationshipStore and service writer,
		// and the unfenced negative control must reproduce the spurious
		// changed-since removal. Only real Postgres evaluates the fused fence.
		"TestServiceCatalogCorpusFenceDefersAndPreservesEvidenceLive",
		"TestServiceCatalogCorpusFenceUnfencedReadReportsSpuriousRemovalLive",
		// #6809: the generation retention statements must prepare and run
		// against the real migrated schema, not a hand-written fixture.
		"TestGenerationRetentionStatementsPrepareAgainstMigratedSchemaLive",
		"TestGenerationRetentionPrunesMigratedSchemaLive",
		"TestGenerationRetentionContentPrunesDeleteExactRowsLive",
		"TestGenerationRetentionContentPrunesFinishWithoutPlannerStatisticsLive",
		"TestGenerationRetentionRowCountsAttributeSharedRowsOnceLive",
		"TestGenerationRetentionProbeMatchesGroupedPassLive",
		"TestGenerationRetentionProbePlansStayOnKeyIndexesLive",
		"TestGenerationRetentionRefusesWithoutValidKeyIndexLive",
		"TestGenerationRetentionScopeMismatchedFactNeverOverDeletesLive",
		// #7334 section 1: the all-scope eligibility statement that replaced
		// the per-scope candidate query, and its EvalPlanQual/plan-shape
		// proofs; only real Postgres row locks, snapshots, and query plans
		// reproduce them.
		"TestGenerationRetentionSelectsEligibleGenerationsAcrossScopesLive",
		"TestGenerationRetentionLockSetMatchesEligibleScopesLive",
		"TestGenerationRetentionFairnessAcrossScopesLive",
		"TestGenerationRetentionSkipsHeldScopeAndReplacesLive",
		"TestGenerationRetentionEvalPlanQualDropsRacedCandidatesLive",
		"TestGenerationRetentionCandidatePlanNeverLoopsFactWorkItemsLive",
		// #7127 PR-3d (arbiter ruling arb-7127-3d-c): an over-limit batch of
		// one that another session takes after its savepoint rollback prunes
		// nothing and writes no event; only real row locks reproduce it.
		"TestGenerationRetentionNarrowedCandidateTakenElsewhereLive",
		// #6475 part B: the changed-since resolve binds the caller's grant on
		// the lineage row's scope_id; only real Postgres runs that predicate.
		"TestServiceChangedSinceBindsGrantToLineageScopeLive",
		// #7389: the write-start marker and the heartbeat supersede must never
		// both win; only real row locks and EvalPlanQual rechecks show it.
		"TestProjectorHeartbeatWriteMarkerInterleave",
		"TestProjectorWriteMarkerHeartbeatOrderingLive",
		"TestProjectorHeartbeatWriteGateRowsLive",
		"TestUncoveredProjectionWritersLive",
		"TestReplayedFailedGenerationWritesAndActivatesLive",
		"TestReplayAfterFullRecordsLatestWriteStartLive",
		"TestProjectorHeartbeatNeverDeadlocksWithBaselineRefusal",
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

// generationLivenessProofDSN gates the generation-liveness proofs: without it
// they skip, so a selected-but-unset proof and an unselected proof both prove
// nothing in CI.
const generationLivenessProofDSN = "ESHU_GENERATION_LIVENESS_PROOF_DSN"

// generationLivenessGatedTests discovers every Test function declared in a
// _test.go file of this package that references generationLivenessProofDSN.
// The enrollment guard itself mentions the variable, so its own file is
// excluded: the guard stays hermetic and unselected.
func generationLivenessGatedTests(t *testing.T) []string {
	t.Helper()

	const guardFile = "reducer_queue_cross_scope_readiness_gate_coverage_test.go"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("list package test files: %v", err)
	}
	testFunc := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == guardFile || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Contains(body, []byte(generationLivenessProofDSN)) {
			continue
		}
		for _, m := range testFunc.FindAllSubmatch(body, -1) {
			names = append(names, string(m[1]))
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatalf("no test references %s: the discovery scan is broken", generationLivenessProofDSN)
	}
	return names
}

// activeWorkProjectionProofEnv requires live settings in the blocking job's
// PostgreSQL proof step. YAML decoding prevents text in comments or scalar
// values from posing as environment keys or executable steps.
func activeWorkProjectionProofEnv(t *testing.T, workflow string) bool {
	t.Helper()
	var parsed struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string            `yaml:"run"`
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(workflow), &parsed); err != nil {
		return false
	}
	job, ok := parsed.Jobs["contention-gate"]
	if !ok {
		return false
	}
	const marker = "go test ./internal/storage/postgres/ -run"
	proofSteps := 0
	validEnv := false
	for _, step := range job.Steps {
		if !strings.Contains(step.Run, marker) {
			continue
		}
		proofSteps++
		validEnv = strings.TrimSpace(step.Env["ESHU_POSTGRES_DSN"]) != "" &&
			step.Env["ESHU_REQUIRE_ACTIVE_WORK_PROJECTION_PROOF"] == "1"
	}
	return proofSteps == 1 && validEnv
}

func TestActiveWorkProjectionProofEnvIsInProofStep(t *testing.T) {
	t.Parallel()
	const job = "jobs:\n  contention-gate:\n    steps:\n"
	const run = "        run: go test ./internal/storage/postgres/ -run 'X'\n"
	const dsn = "          ESHU_POSTGRES_DSN: postgres://example.invalid/eshu\n"
	const required = "          ESHU_REQUIRE_ACTIVE_WORK_PROJECTION_PROOF: \"1\"\n"
	const nested = "          UNUSED: |\n            ESHU_POSTGRES_DSN: postgres://example.invalid/eshu\n            ESHU_REQUIRE_ACTIVE_WORK_PROJECTION_PROOF: \"1\"\n"
	for _, tc := range []struct {
		name, workflow string
	}{
		{"DSN in other step", job + "      - name: Other\n        env:\n" + dsn + "      - name: Run\n        env:\n" + required + run},
		{"required flag in other step", job + "      - name: Other\n        env:\n" + required + "      - name: Run\n        env:\n" + dsn + run},
		{"DSN in other job", job + "      - name: Run\n        env:\n" + required + run + "  other-job:\n    steps:\n      - name: Other\n        env:\n" + dsn},
		{"commented DSN", job + "      - name: Run\n        env:\n          # ESHU_POSTGRES_DSN: postgres://example.invalid/eshu\n" + required + run},
		{"commented required flag", job + "      - name: Run\n        env:\n" + dsn + "          # ESHU_REQUIRE_ACTIVE_WORK_PROJECTION_PROOF: \"1\"\n" + run},
		{"keys in block scalar", job + "      - name: Run\n        env:\n" + nested + run},
		{"blank DSN", job + "      - name: Run\n        env:\n          ESHU_POSTGRES_DSN: \"\"\n" + required + run},
		{"blank required flag", job + "      - name: Run\n        env:\n" + dsn + "          ESHU_REQUIRE_ACTIVE_WORK_PROJECTION_PROOF: \"\"\n" + run},
		{"missing DSN", job + "      - name: Run\n        env:\n" + required + run},
		{"missing required flag", job + "      - name: Run\n        env:\n" + dsn + run},
		{"run marker in scalar only", job + "      - name: Run\n        env:\n" + dsn + required + "          UNUSED: |\n            " + run},
		{"duplicate proof step", job + "      - name: First\n        env:\n" + dsn + required + run + "      - name: Second\n        env:\n" + dsn + required + run},
		{"malformed YAML", job + "      - name: Run\n        env: [\n" + run},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if activeWorkProjectionProofEnv(t, tc.workflow) {
				t.Fatal("an unbound, absent, or malformed setting satisfied the Postgres proof guard")
			}
		})
	}
	green := job + "      - name: Run\n        env:\n" + dsn + required + run
	if !activeWorkProjectionProofEnv(t, green) {
		t.Fatal("the Postgres proof step's live DSN and required flag were rejected")
	}
}
