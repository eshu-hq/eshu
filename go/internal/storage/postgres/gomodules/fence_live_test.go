// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10055 real PostgreSQL proof of the commit-skew fence: the amended rule on the real page read at one slot and at fleet scale, the retraction transition, the unusable-remote acceptance case, the dirty-log capture, and the open-target guard releasing a repository after a terminal failure
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/coordinator/reachability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const fenceLiveInterval = time.Hour

var fenceLiveEvidenceCommit = strings.Repeat("e", 40)

func fenceStore(database *sql.DB) Store {
	return NewStore(postgres.SQLDB{DB: database}).WithHolderReopener(postgres.ReopenSupplyChainImpactHolderInTransaction)
}

// seedStampedEvidence writes a repository's go-reachability scope holding kinds, with
// the commit a committed claim stamps and the activation its Ack writes. An empty commit
// stays NULL. It returns the scope and the active generation.
func seedStampedEvidence(
	t *testing.T,
	database *sql.DB,
	repositoryID, commit string,
	activatedAgo time.Duration,
	kinds ...string,
) (string, string) {
	t.Helper()
	seedReachabilityScope(t, database, repositoryID, kinds)
	scopeID := scannercomponent.GoReachabilityScopePrefix + repositoryID
	generationID := "reach-" + repositoryID + "-0"
	var stamped any
	if commit != "" {
		stamped = commit
	}
	if _, err := database.ExecContext(t.Context(),
		`UPDATE scope_generations SET source_commit_sha = $1, activated_at = $2 WHERE generation_id = $3`,
		stamped, time.Now().UTC().Add(-activatedAgo), generationID); err != nil {
		t.Fatalf("stamp evidence generation: %v", err)
	}
	return scopeID, generationID
}

// seedRepositoryLeft records that the repository's own scope had a generation at commit
// that was superseded leftAgo before now: the moment the repository left that commit.
func seedRepositoryLeft(t *testing.T, database *sql.DB, repositoryID, commit string, leftAgo time.Duration) {
	t.Helper()
	at := time.Now().UTC().Add(-leftAgo)
	if _, err := database.ExecContext(t.Context(), `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, source_commit_sha, superseded_at)
VALUES ($1, $2, 'snapshot', $3, $3, 'superseded', $4, $3)`,
		fmt.Sprintf("left-%s-%s-%d", repositoryID, commit[:6], leftAgo), "git-repository-scope:"+repositoryID, at, commit); err != nil {
		t.Fatalf("seed repository history: %v", err)
	}
}

// scopeState is what the fence changes for one scope.
type scopeState struct {
	pointer      sql.NullString
	status       string
	supersededAt sql.NullTime
}

func readScopeState(t *testing.T, database *sql.DB, scopeID, generationID string) scopeState {
	t.Helper()
	var state scopeState
	if err := database.QueryRowContext(t.Context(), `
SELECT scope.active_generation_id, generation.status, generation.superseded_at
FROM ingestion_scopes AS scope
JOIN scope_generations AS generation ON generation.scope_id = scope.scope_id
WHERE scope.scope_id = $1 AND generation.generation_id = $2`, scopeID, generationID,
	).Scan(&state.pointer, &state.status, &state.supersededAt); err != nil {
		t.Fatalf("read scope state: %v", err)
	}
	return state
}

// countActiveEvidence reads Go evidence the way every downstream reader does, through
// the scope's active generation pointer.
func countActiveEvidence(t *testing.T, database *sql.DB, scopeID string) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(t.Context(), `
SELECT count(*)
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id AND scope.active_generation_id = fact.generation_id
WHERE fact.scope_id = $1
  AND fact.fact_kind IN ('vulnerability.go_module_evidence', 'vulnerability.go_call_reachability')`, scopeID,
	).Scan(&count); err != nil {
		t.Fatalf("count active evidence: %v", err)
	}
	return count
}

func countScopeFacts(t *testing.T, database *sql.DB, scopeID string) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(t.Context(),
		`SELECT count(*) FROM fact_records WHERE scope_id = $1`, scopeID).Scan(&count); err != nil {
		t.Fatalf("count scope facts: %v", err)
	}
	return count
}

// TestCommitSkewRuleOnThePageReadLive drives the real page read and the real fence over
// Amendment 1's cases on a one-slot fleet, where the bound is two intervals. Only the
// row the rule names changes, the next page reads no evidence for it, and the facts stay
// in place because only retirement deletes them.
func TestCommitSkewRuleOnThePageReadLive(t *testing.T) {
	database := openStoreProof(t)
	store := fenceStore(database)
	const module = "vulnerability.go_module_evidence"
	month := 30 * 24 * time.Hour
	past, inside := 3*fenceLiveInterval, fenceLiveInterval
	evidence := fenceLiveEvidenceCommit

	// (a) Skew older than the bound.
	stale := seedRepository(t, database, 1, "go.mod")
	staleScope, staleGeneration := seedStampedEvidence(t, database, stale, evidence, month, module, "vulnerability.go_call_reachability")
	seedRepositoryLeft(t, database, stale, evidence, past)

	// (c) A quiet repository: old evidence, no skew.
	quiet := seedRepository(t, database, 2, "go.mod")
	quietScope, quietGeneration := seedStampedEvidence(t, database, quiet, fmt.Sprintf("%040x", 2), month, module)

	// (d) Old evidence and a push one hour ago. The evidence's age must not fence it.
	pushed := seedRepository(t, database, 3, "go.mod")
	pushedScope, pushedGeneration := seedStampedEvidence(t, database, pushed, evidence, month, module)
	seedRepositoryLeft(t, database, pushed, evidence, inside)

	// (e) History pruned. Another commit's row must not count as the evidence commit's.
	pruned := seedRepository(t, database, 4, "go.mod")
	prunedScope, prunedGeneration := seedStampedEvidence(t, database, pruned, evidence, month, module)
	seedRepositoryLeft(t, database, pruned, strings.Repeat("a", 40), inside)

	// (f) Push between plan and Ack: the evidence activated after the repository left its
	// commit, so a measure of "generations after the evidence" would never see the skew.
	ahead := seedRepository(t, database, 5, "go.mod")
	aheadScope, aheadGeneration := seedStampedEvidence(t, database, ahead, evidence, 10*time.Minute, module)
	seedRepositoryLeft(t, database, ahead, evidence, past)

	// (g) No stamped commit.
	unstamped := seedRepository(t, database, 6, "go.mod")
	unstampedScope, unstampedGeneration := seedStampedEvidence(t, database, unstamped, "", month, module)

	// A warning-only generation is not a candidate, however skewed.
	warning := seedRepository(t, database, 7, "go.mod")
	warningScope, warningGeneration := seedStampedEvidence(t, database, warning, evidence, month, "vulnerability.warning")
	seedRepositoryLeft(t, database, warning, evidence, past)

	// A forced re-ingest at the evidence commit: max(superseded_at) picks the newest row.
	reingested := seedRepository(t, database, 8, "go.mod")
	reingestedScope, reingestedGeneration := seedStampedEvidence(t, database, reingested, evidence, month, module)
	seedRepositoryLeft(t, database, reingested, evidence, 10*fenceLiveInterval)
	seedRepositoryLeft(t, database, reingested, evidence, inside)

	page, _ := readGoPage(t, database)
	decision := reachability.StaleEvidence(page.Repositories, time.Now().UTC(), fenceLiveInterval, page.SlotCount)
	want := map[scannercomponent.StaleEvidenceFence]bool{
		{ScopeID: staleScope, GenerationID: staleGeneration}:   true,
		{ScopeID: prunedScope, GenerationID: prunedGeneration}: true,
		{ScopeID: aheadScope, GenerationID: aheadGeneration}:   true,
	}
	if len(decision.Fences) != len(want) {
		t.Fatalf("fences = %+v, want exactly %+v", decision.Fences, want)
	}
	for _, fence := range decision.Fences {
		if !want[fence] {
			t.Fatalf("fenced %+v, want only %+v", fence, want)
		}
	}
	if decision.SkippedNoCommit != 1 || decision.HistoryPruned != 1 {
		t.Fatalf("decision = %+v, want one skipped for no commit and one pruned", decision)
	}
	factsBefore := countScopeFacts(t, database, staleScope)

	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	result, err := store.FenceStaleGoEvidence(t.Context(), decision.Fences, observedAt)
	if err != nil || result.Fenced != len(want) {
		t.Fatalf("result = %+v, err = %v, want %d fenced", result, err, len(want))
	}

	state := readScopeState(t, database, staleScope, staleGeneration)
	if state.pointer.Valid || state.status != "superseded" || !state.supersededAt.Time.Equal(observedAt) {
		t.Fatalf("fenced scope state = %+v, want a NULL pointer and superseded at %v", state, observedAt)
	}
	if got := countActiveEvidence(t, database, staleScope); got != 0 {
		t.Fatalf("active evidence after the fence = %d, want 0", got)
	}
	if got := countScopeFacts(t, database, staleScope); got != factsBefore || got == 0 {
		t.Fatalf("facts after the fence = %d, want the %d retirement deletes", got, factsBefore)
	}
	for name, kept := range map[string][2]string{
		"no skew":          {quietScope, quietGeneration},
		"push an hour ago": {pushedScope, pushedGeneration},
		"no commit":        {unstampedScope, unstampedGeneration},
		"warning only":     {warningScope, warningGeneration},
		"re-ingested at E": {reingestedScope, reingestedGeneration},
	} {
		state := readScopeState(t, database, kept[0], kept[1])
		if state.pointer.String != kept[1] || state.status != "active" {
			t.Errorf("%s: state = %+v, want the generation still active", name, state)
		}
	}

	next, planned := readGoPage(t, database)
	if !planned[stale] {
		t.Fatalf("page = %+v, want a repository with a go.mod still planned after its fence", next)
	}
	for _, repository := range next.Repositories {
		if repository.RepositoryID == stale && (repository.HasGoEvidence || repository.EvidenceGenerationID != "") {
			t.Fatalf("repository = %+v, want no evidence after the fence", repository)
		}
	}
	if again := reachability.StaleEvidence(next.Repositories, time.Now().UTC(), fenceLiveInterval, next.SlotCount); len(again.Fences) != 0 {
		t.Fatalf("second pass fences = %+v, want none", again.Fences)
	}
}

// TestSkewInsideTwoRotationsIsNotFencedOnALargeFleetLive is Amendment 1 case (b). With
// over 768 repository scopes one repository is visited once per four intervals, so skew of
// three intervals is healthy rotation. The slot count comes from the real page read.
func TestSkewInsideTwoRotationsIsNotFencedOnALargeFleetLive(t *testing.T) {
	database := openStoreProof(t)
	for _, statement := range []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		    observed_at, ingested_at, status, payload)
		 SELECT 'git-repository-scope:repository:r_' || lpad(to_hex(i), 8, '0'), 'repository', 'git',
		        'repository:r_' || lpad(to_hex(i), 8, '0'), 'git', 'p', now(), now(), 'active', '{}'::jsonb
		 FROM generate_series(4096, 5095) AS i`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, source_commit_sha)
		 SELECT 'fleet-' || i, 'git-repository-scope:repository:r_' || lpad(to_hex(i), 8, '0'), 'snapshot', now(), now(), 'active', repeat('f', 40)
		 FROM generate_series(4096, 5095) AS i`,
		`UPDATE ingestion_scopes SET active_generation_id = 'fleet-' || ('x' || right(source_key, 8))::bit(32)::int
		 WHERE scope_id LIKE 'git-repository-scope:repository:r_%'`,
	} {
		if _, err := database.ExecContext(t.Context(), statement); err != nil {
			t.Fatalf("seed fleet: %v", err)
		}
	}
	const module = "vulnerability.go_module_evidence"
	young := seedRepository(t, database, 1, "go.mod")
	youngScope, youngGeneration := seedStampedEvidence(t, database, young, fenceLiveEvidenceCommit, fenceLiveInterval, module)
	seedRepositoryLeft(t, database, young, fenceLiveEvidenceCommit, 3*fenceLiveInterval)
	old := seedRepository(t, database, 2, "go.mod")
	oldScope, oldGeneration := seedStampedEvidence(t, database, old, fenceLiveEvidenceCommit, fenceLiveInterval, module)
	seedRepositoryLeft(t, database, old, fenceLiveEvidenceCommit, 9*fenceLiveInterval)

	page, _ := readGoPage(t, database)
	if page.TotalCandidates != 1002 || page.SlotCount != 4 {
		t.Fatalf("page = total %d, slots %d, want 1002 and 4", page.TotalCandidates, page.SlotCount)
	}
	decision := reachability.StaleEvidence(page.Repositories, time.Now().UTC(), fenceLiveInterval, page.SlotCount)
	want := scannercomponent.StaleEvidenceFence{ScopeID: oldScope, GenerationID: oldGeneration}
	if len(decision.Fences) != 1 || decision.Fences[0] != want {
		t.Fatalf("fences = %+v, want only [%+v], not %s/%s", decision.Fences, want, youngScope, youngGeneration)
	}
}

// TestFencedRepositoryWithNoGoModDropsOutLive is state S2 of the ruling: with no go.mod
// and no evidence left, nothing plans the repository again.
func TestFencedRepositoryWithNoGoModDropsOutLive(t *testing.T) {
	database := openStoreProof(t)
	repository := seedRepository(t, database, 1, "")
	seedStampedEvidence(t, database, repository, fenceLiveEvidenceCommit, 3*fenceLiveInterval, "vulnerability.go_module_evidence")
	seedRepositoryLeft(t, database, repository, fenceLiveEvidenceCommit, 3*fenceLiveInterval)

	page, planned := readGoPage(t, database)
	if !planned[repository] {
		t.Fatalf("page = %+v, want the repository planned while it holds evidence", page)
	}
	decision := reachability.StaleEvidence(page.Repositories, time.Now().UTC(), fenceLiveInterval, page.SlotCount)
	if _, err := fenceStore(database).FenceStaleGoEvidence(t.Context(), decision.Fences, time.Now().UTC()); err != nil {
		t.Fatalf("fence: %v", err)
	}
	if next, planned := readGoPage(t, database); planned[repository] {
		t.Fatalf("page = %+v, want the repository dropped after its fence", next)
	}
}

// TestUnusableRemoteEvidenceRetractsAtTheBoundLive is the acceptance case of decision
// 3.2, with the remote emptied by the seed: the planner skips the repository by name,
// inside the bound the evidence stays, and past it the fence retracts the evidence though
// no claim can run.
func TestUnusableRemoteEvidenceRetractsAtTheBoundLive(t *testing.T) {
	database := openStoreProof(t)
	repository := seedRepository(t, database, 1, "go.mod")
	scopeID, generationID := seedStampedEvidence(t, database, repository, fenceLiveEvidenceCommit, 30*fenceLiveInterval, "vulnerability.go_module_evidence")
	seedRepositoryLeft(t, database, repository, fenceLiveEvidenceCommit, fenceLiveInterval)
	if _, err := database.ExecContext(t.Context(),
		`UPDATE ingestion_scopes SET payload = payload - 'remote_url' WHERE source_key = $1`, repository); err != nil {
		t.Fatalf("empty the remote: %v", err)
	}
	now := time.Now().UTC()
	instance := workflow.CollectorInstance{
		InstanceID: "scanner-go-reachability", CollectorKind: scope.CollectorScannerWorker,
		Mode: workflow.CollectorModeScheduled, Enabled: true, ClaimsEnabled: true, Configuration: `{}`,
		CreatedAt: now, UpdatedAt: now, LastObservedAt: now,
	}

	page, _ := readGoPage(t, database)
	_, items, skipped, err := reachability.Plan(reachability.PlanRequest{
		Instance: instance, ObservedAt: now, PlanKey: "go-modules-scheduled-20261105T120000Z", Repositories: page.Repositories,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(items) != 0 || skipped[reachability.SkipUnsupportedRemoteWithEvidence] != 1 {
		t.Fatalf("items = %d, skipped = %v, want none planned and the named skip", len(items), skipped)
	}
	if inside := reachability.StaleEvidence(page.Repositories, now, fenceLiveInterval, page.SlotCount); len(inside.Fences) != 0 {
		t.Fatalf("fences inside the bound = %+v, want none", inside.Fences)
	}

	later := now.Add(2 * fenceLiveInterval)
	past := reachability.StaleEvidence(page.Repositories, later, fenceLiveInterval, page.SlotCount)
	result, err := fenceStore(database).FenceStaleGoEvidence(t.Context(), past.Fences, later)
	if err != nil || result.Fenced != 1 {
		t.Fatalf("result = %+v, err = %v, want 1 fenced", result, err)
	}
	if state := readScopeState(t, database, scopeID, generationID); state.pointer.Valid || state.status != "superseded" {
		t.Fatalf("state = %+v, want the evidence retracted", state)
	}
}

// TestFenceLogsTheGenerationAndALaterTickConsumesItLive is the dirty-log proof. The
// pointer change writes one scope row naming the retracted generation, and a keyed tick
// consumes it. This asserts capture and consumption only: the tick re-picks among stored
// findings and re-derives nothing, which is why the fence reopens holders.
func TestFenceLogsTheGenerationAndALaterTickConsumesItLive(t *testing.T) {
	database := openStoreProof(t)
	repository := seedRepository(t, database, 1, "go.mod")
	scopeID, generationID := seedStampedEvidence(t, database, repository, fenceLiveEvidenceCommit, 3*fenceLiveInterval, "vulnerability.go_module_evidence")
	winners := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: database})
	winners.MaxKeyFraction = 1000
	if _, err := winners.ResweepWinnersKeyed(t.Context(), time.Now().UTC()); err != nil {
		t.Fatalf("drain the seed's capture: %v", err)
	}
	const logged = `SELECT count(*) FROM supply_chain_impact_winners_dirty_log
		WHERE kind = 'scope' AND source = 'activation' AND scope_id = $1 AND generation_id = $2`
	readLogged := func() int {
		t.Helper()
		var count int
		if err := database.QueryRowContext(t.Context(), logged, scopeID, generationID).Scan(&count); err != nil {
			t.Fatalf("read dirty log: %v", err)
		}
		return count
	}
	if got := readLogged(); got != 0 {
		t.Fatalf("dirty rows before the fence = %d, want 0", got)
	}

	fence := []scannercomponent.StaleEvidenceFence{{ScopeID: scopeID, GenerationID: generationID}}
	if _, err := fenceStore(database).FenceStaleGoEvidence(t.Context(), fence, time.Now().UTC()); err != nil {
		t.Fatalf("fence: %v", err)
	}
	if got := readLogged(); got != 1 {
		t.Fatalf("dirty rows after the fence = %d, want 1", got)
	}
	if _, err := winners.ResweepWinnersKeyed(t.Context(), time.Now().UTC()); err != nil {
		t.Fatalf("keyed tick: %v", err)
	}
	if got := readLogged(); got != 0 {
		t.Fatalf("dirty rows after the tick = %d, want 0", got)
	}
}

// TestTerminalFailureReleasesTheNextPlanLive settles the open question in the ruling: a
// terminal claim must not stop planning. While the run is open the guard holds the
// repository's next item. Once reconciliation closes the run as failed, the next rotation
// admits it, so a dead remote keeps producing a visible claim.
func TestTerminalFailureReleasesTheNextPlanLive(t *testing.T) {
	database := openStoreProof(t)
	seedRepository(t, database, 1, "go.mod")
	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID: "scanner-go-reachability", CollectorKind: scope.CollectorScannerWorker,
		Mode: workflow.CollectorModeScheduled, Enabled: true, ClaimsEnabled: true, Configuration: `{}`,
		CreatedAt: now, UpdatedAt: now, LastObservedAt: now,
	}
	control := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: database})
	if err := control.ReconcileCollectorInstances(t.Context(), now, []workflow.DesiredCollectorInstance{{
		InstanceID: instance.InstanceID, CollectorKind: instance.CollectorKind, Mode: instance.Mode,
		Enabled: true, ClaimsEnabled: true, Configuration: instance.Configuration,
	}}); err != nil {
		t.Fatal(err)
	}
	admit := func(planKey string, observedAt time.Time) (workflow.Run, int) {
		t.Helper()
		page, _ := readGoPage(t, database)
		run, items, _, err := reachability.Plan(reachability.PlanRequest{
			Instance: instance, ObservedAt: observedAt, PlanKey: planKey, Repositories: page.Repositories,
		})
		if err != nil {
			t.Fatalf("plan %s: %v", planKey, err)
		}
		admission, err := control.CreateRunWithWorkItemsIfNoOpenTargets(t.Context(), run, items)
		if err != nil {
			t.Fatalf("admit %s: %v", planKey, err)
		}
		return run, admission.InsertedWorkItems
	}

	first, inserted := admit("go-modules-scheduled-20261105T120000Z", now)
	if inserted != 1 {
		t.Fatalf("first admission inserted %d, want 1", inserted)
	}
	if _, held := admit("go-modules-scheduled-20261105T130000Z", now.Add(time.Hour)); held != 0 {
		t.Fatalf("admission behind an open run inserted %d, want 0", held)
	}
	if _, err := database.ExecContext(t.Context(),
		`UPDATE workflow_work_items SET status = 'failed_terminal' WHERE run_id = $1`, first.RunID); err != nil {
		t.Fatalf("fail the claim terminal: %v", err)
	}
	if _, err := control.ReconcileWorkflowRuns(t.Context(), now.Add(2*time.Hour)); err != nil {
		t.Fatalf("reconcile runs: %v", err)
	}
	var status string
	if err := database.QueryRowContext(t.Context(),
		`SELECT status FROM workflow_runs WHERE run_id = $1`, first.RunID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(workflow.RunStatusFailed) {
		t.Fatalf("run status = %q, want %q", status, workflow.RunStatusFailed)
	}
	if _, released := admit("go-modules-scheduled-20261105T140000Z", now.Add(2*time.Hour)); released != 1 {
		t.Fatalf("admission after the terminal failure inserted %d, want 1: planning stopped", released)
	}
}
