// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	reachabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/reachability"
)

// coverageLiveRepo seeds one repository scope for the consumer-coverage proof.
type coverageLiveRepo struct {
	repo      string
	suffix    string
	hasGen    bool
	intent    string // "completed", "pending", "other", "refresh", or "" for none
	watermark string // "complete", "truncated", "old-epoch", "truncated-old-epoch", or "" for none
	staleOnly bool   // the only code intents belong to a superseded generation
	// work seeds one fact_work_items row per entry for the active generation,
	// keyed by reducer domain with the item status as value (#7602).
	work map[string]string
}

// seedCoverageLiveRepo writes the rows a repository scope, its generation, its
// acceptance row, an optional intent and an optional watermark leave behind.
func seedCoverageLiveRepo(ctx context.Context, t *testing.T, db *sql.DB, r coverageLiveRepo) {
	t.Helper()

	scopeID := "git-repository-scope:" + r.repo + r.suffix
	generationID := "gen-" + r.repo + r.suffix
	oldGenerationID := "old-" + r.repo + r.suffix
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %s: %v\n%s", r.repo, err, query)
		}
	}
	const intentSQL = `INSERT INTO shared_projection_intents(intent_id, projection_domain, partition_key,
		scope_id, acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
		VALUES ($1, $2, 'k', $3, $4, $4, $5, $6, '{}', now(), $7)`
	const refreshIntentSQL = `INSERT INTO shared_projection_intents(intent_id, projection_domain, partition_key,
		scope_id, acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
		VALUES ($1, $2, 'k', $3, $4, $4, $5, $6, '{"action":"refresh","intent_type":"repo_refresh"}', now(), $7)`
	exec(`INSERT INTO ingestion_scopes(scope_id, scope_kind, source_system, source_key, collector_kind,
		partition_key, observed_at, ingested_at, status)
		VALUES ($1, 'repository', 'git', $2, 'git', 'p', now(), now(), 'active')`, scopeID, r.repo)
	if r.staleOnly {
		exec(`INSERT INTO scope_generations(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
			VALUES ($1, $2, 'x', now(), now(), 'superseded')`, oldGenerationID, scopeID)
		exec(`INSERT INTO shared_projection_acceptance(scope_id, acceptance_unit_id, source_run_id, generation_id,
			accepted_at, updated_at) VALUES ($1, $2, 'run-old', $3, now(), now())`, scopeID, r.repo, oldGenerationID)
		exec(intentSQL, "i-old-"+r.repo+r.suffix, "code_calls", scopeID, r.repo, "run-old", oldGenerationID, time.Now())
	}
	if !r.hasGen {
		return
	}
	exec(`INSERT INTO scope_generations(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		VALUES ($1, $2, 'x', now(), now(), 'active', now())`, generationID, scopeID)
	exec(`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, generationID, scopeID)
	exec(`INSERT INTO shared_projection_acceptance(scope_id, acceptance_unit_id, source_run_id, generation_id,
		accepted_at, updated_at) VALUES ($1, $2, 'run-new', $3, now(), now())`, scopeID, r.repo, generationID)
	switch r.intent {
	case "completed":
		exec(intentSQL, "i-"+r.repo+r.suffix, "code_calls", scopeID, r.repo, "run-new", generationID, time.Now())
	case "pending":
		exec(intentSQL, "i-"+r.repo+r.suffix, "inheritance_edges", scopeID, r.repo, "run-new", generationID, nil)
	case "other":
		exec(intentSQL, "i-"+r.repo+r.suffix, "platform_infra", scopeID, r.repo, "run-new", generationID, time.Now())
	case "refresh":
		exec(refreshIntentSQL, "i-"+r.repo+r.suffix, "code_calls", scopeID, r.repo, "run-new", generationID, nil)
	}
	switch r.watermark {
	case "complete", "truncated", "old-epoch", "truncated-old-epoch":
		epoch := reachabilitystore.CodeReachabilityVerdictSchemaEpoch
		if strings.HasSuffix(r.watermark, "old-epoch") {
			epoch--
		}
		exec(`INSERT INTO code_reachability_repository_watermarks(scope_id, generation_id, repository_id, truncated,
			updated_at, verdict_schema_epoch) VALUES ($1, $2, $3, $4, now(), $5)`,
			scopeID, generationID, r.repo, strings.HasPrefix(r.watermark, "truncated"), epoch)
	}
	// Fail on an unknown work key: the seed loop below only reads the two
	// known domains, so a typo'd domain would seed nothing and a
	// complete-expecting case would pass vacuously.
	for key := range r.work {
		if key != string(reducercontract.DomainCodeCallMaterialization) &&
			key != string(reducercontract.DomainInheritanceMaterialization) {
			t.Fatalf("unknown work domain %q", key)
		}
	}
	// Seed in domain order so the fixture is deterministic.
	for _, domain := range []reducercontract.Domain{
		reducercontract.DomainCodeCallMaterialization,
		reducercontract.DomainInheritanceMaterialization,
	} {
		status, ok := r.work[string(domain)]
		if !ok {
			continue
		}
		exec(`INSERT INTO fact_work_items(work_item_id, scope_id, generation_id, stage,
			domain, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
			"w-"+r.repo+r.suffix+"-"+string(domain), scopeID, generationID,
			string(recovery.StageReducer), string(domain), status, time.Now())
	}
}

// coverageLiveRepos is the repository scopes TestCrossRepoDeadCodeConsumerCoverageLive
// seeds. r-multi2 is the multi-scope case where "lowest generation id" and "a
// truncated scope first" disagree (its -a scope has the lower generation id and
// no watermark, its -b scope is truncated), and r-trunc-old is one watermark that
// is both truncated and below the current epoch. The r-window scopes are the
// activation-to-first-intent window (#7602): refresh-only intents on a full
// generation with reducer work in various states.
func coverageLiveRepos() []coverageLiveRepo {
	codeCalls := string(reducercontract.DomainCodeCallMaterialization)
	inheritance := string(reducercontract.DomainInheritanceMaterialization)
	return []coverageLiveRepo{
		{repo: "r-ok", hasGen: true, intent: "completed", watermark: "complete"},
		{repo: "r-nowm", hasGen: true, intent: "completed"},
		{repo: "r-trunc", hasGen: true, intent: "completed", watermark: "truncated"},
		{repo: "r-trunc-old", hasGen: true, intent: "completed", watermark: "truncated-old-epoch"},
		{repo: "r-pending", hasGen: true, intent: "pending"},
		{repo: "r-docs", hasGen: true},
		{repo: "r-docs-trunc", hasGen: true, watermark: "truncated"},
		{repo: "r-otherdomain", hasGen: true, intent: "other"},
		{repo: "r-nogen"},
		{repo: "r-stale", hasGen: true, staleOnly: true},
		{repo: "r-oldepoch", hasGen: true, intent: "completed", watermark: "old-epoch"},
		{repo: "r-oldepoch-docs", hasGen: true, watermark: "old-epoch"},
		{repo: "r-multi", suffix: "-a", hasGen: true, intent: "completed", watermark: "complete"},
		{repo: "r-multi", suffix: "-b", hasGen: true, intent: "completed"},
		{repo: "r-multi2", suffix: "-a", hasGen: true, intent: "completed"},
		{repo: "r-multi2", suffix: "-b", hasGen: true, intent: "completed", watermark: "truncated"},
		{repo: "r-window", hasGen: true, intent: "refresh", work: map[string]string{
			codeCalls: "pending", inheritance: "succeeded",
		}},
		{repo: "r-window-done", hasGen: true, intent: "refresh", work: map[string]string{
			codeCalls: "succeeded", inheritance: "succeeded",
		}},
		{repo: "r-window-nowork", hasGen: true, intent: "refresh"},
		{repo: "r-window-dead", hasGen: true, intent: "refresh", work: map[string]string{
			codeCalls: "dead_letter", inheritance: "succeeded",
		}},
	}
}

// coverageLiveCase is one coverage request and the gaps it must report.
type coverageLiveCase struct {
	name    string
	request code.CrossRepoDeadCodeCoverageRequest
	want    []code.CrossRepoDeadCodeCoverageGap
}

func coverageLiveGap(repo, state, generation string) code.CrossRepoDeadCodeCoverageGap {
	return code.CrossRepoDeadCodeCoverageGap{
		RepositoryID: repo,
		State:        state,
		GenerationID: generation,
		Retryable:    code.CrossRepoDeadCodeCoverageGapRetryable(state),
	}
}

// coverageLiveCases is every request mode with the gaps the seeded fixture must
// produce: the same repository, state, generation id and retryable flag from the
// named statement (DISTINCT ON in SQL) and the all-repositories statement (the
// pick in Go).
func coverageLiveCases() []coverageLiveCase {
	const (
		noSnapshot = code.CrossRepoDeadCodeCoverageStateNoSnapshotYet
		truncated  = code.CrossRepoDeadCodeCoverageStateTruncated
		olderEpoch = code.CrossRepoDeadCodeCoverageStateOlderEpoch
		noScope    = code.CrossRepoDeadCodeCoverageStateNoActiveScope
	)
	consumers := []code.CrossRepoDeadCodeCoverageGap{
		coverageLiveGap("r-multi", noSnapshot, "gen-r-multi-b"),
		coverageLiveGap("r-multi2", truncated, "gen-r-multi2-b"),
		coverageLiveGap("r-nowm", noSnapshot, "gen-r-nowm"),
		coverageLiveGap("r-oldepoch", olderEpoch, "gen-r-oldepoch"),
		coverageLiveGap("r-pending", noSnapshot, "gen-r-pending"),
		coverageLiveGap("r-trunc", truncated, "gen-r-trunc"),
		coverageLiveGap("r-trunc-old", truncated, "gen-r-trunc-old"),
	}
	everyRepo := []string{
		"r-ok", "r-nowm", "r-trunc", "r-trunc-old", "r-pending", "r-docs", "r-docs-trunc",
		"r-otherdomain", "r-nogen", "r-stale", "r-multi", "r-multi2", "r-oldepoch", "r-oldepoch-docs", "r-missing",
	}
	// Named adds the two listed repositories with no active scope.
	named := []code.CrossRepoDeadCodeCoverageGap{
		coverageLiveGap("r-missing", noScope, ""),
		coverageLiveGap("r-multi", noSnapshot, "gen-r-multi-b"),
		coverageLiveGap("r-multi2", truncated, "gen-r-multi2-b"),
		coverageLiveGap("r-nogen", noScope, ""),
		coverageLiveGap("r-nowm", noSnapshot, "gen-r-nowm"),
		coverageLiveGap("r-oldepoch", olderEpoch, "gen-r-oldepoch"),
		coverageLiveGap("r-pending", noSnapshot, "gen-r-pending"),
		coverageLiveGap("r-trunc", truncated, "gen-r-trunc"),
		coverageLiveGap("r-trunc-old", truncated, "gen-r-trunc-old"),
	}
	return []coverageLiveCase{
		{
			name:    "named: a repository with no active scope is a gap",
			request: code.CrossRepoDeadCodeCoverageRequest{RepositoryIDs: everyRepo, RequireActiveScope: true},
			want:    named,
		},
		{
			name:    "grant: a granted repository nobody ingested is not a gap",
			request: code.CrossRepoDeadCodeCoverageRequest{RepositoryIDs: everyRepo},
			want:    consumers,
		},
		{
			name: "named repositories that cannot be consumers are complete",
			request: code.CrossRepoDeadCodeCoverageRequest{
				RepositoryIDs:      []string{"r-ok", "r-docs", "r-docs-trunc", "r-otherdomain", "r-stale", "r-oldepoch-docs", "r-window-done", "r-window-nowork"},
				RequireActiveScope: true,
			},
			want: nil,
		},
		{
			// A refresh-only generation with unfinished reducer work is inside
			// the activation-to-first-intent window (#7602): its edges are not
			// drained yet, so it is a gap. A dead-lettered item counts too --
			// its edges will never arrive without intervention.
			name: "window: unfinished reducer work on a refresh-only generation is a gap",
			request: code.CrossRepoDeadCodeCoverageRequest{
				RepositoryIDs:      []string{"r-window", "r-window-done", "r-window-nowork", "r-window-dead"},
				RequireActiveScope: true,
			},
			want: []code.CrossRepoDeadCodeCoverageGap{
				coverageLiveGap("r-window", noSnapshot, "gen-r-window"),
				coverageLiveGap("r-window-dead", noSnapshot, "gen-r-window-dead"),
			},
		},
		{
			name:    "every repository",
			request: code.CrossRepoDeadCodeCoverageRequest{AllRepositories: true},
			want: append(slices.Clone(consumers),
				coverageLiveGap("r-window", noSnapshot, "gen-r-window"),
				coverageLiveGap("r-window-dead", noSnapshot, "gen-r-window-dead"),
			),
		},
	}
}
