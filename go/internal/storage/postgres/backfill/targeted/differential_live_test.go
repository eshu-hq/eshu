// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package targeted_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7584 disposable PostgreSQL 18 write-side differential of the partition-scoped deferred maintenance pass against the whole pass on two identically seeded bootstrapped schemas (outgoing, inbound, GCP, ArgoCD, shared partition, empty, unrelated quiet scope, memo hit and miss); enrolled in the live-postgres-readiness runner; local run passed in 15.07s of test time on the pinned postgres:18 image (2 CPUs, 1 GiB) on 2026-10-05; wall time is logged, not gated
//eshu:readiness:env ESHU_TARGETED_MAINTENANCE_PROOF_DISPOSABLE ESHU_TARGETED_MAINTENANCE_PROOF_DSN

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// TestTargetedMaintenanceMatchesWholePass is the #7584 write-side
// differential. Each subtest seeds two fully bootstrapped schemas
// identically, runs the real corpus-wide pre-pass to reach steady state,
// activates the owed generation without a maintenance pass (the quiet Ack),
// then runs RunDeferredRelationshipMaintenance in one schema and
// RunDeferredRelationshipMaintenanceForPartitions in the other and compares
// the committed rows (see targetedDiffPair.run).
func TestTargetedMaintenanceMatchesWholePass(t *testing.T) {
	t.Run("direct_outgoing_reference", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.Prepass()
		p.QuietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		runTargetedDiff(p, "direct_outgoing_reference", targetedDiffCase{
			owed:        owedPartitions("git:tgt", "tgt-2"),
			compared:    partitionSet("git:tgt", "tgt-2"),
			newEvidence: []string{"repo-tgt->repo-dep"},
			published:   partitionSet("git:tgt", "tgt-2"),
			reopened:    workIDs("tgt-2"),
			// dep-1 is a memo hit with no inserted evidence row (the new
			// repo-tgt->repo-dep row lands in tgt-2), so the whole pass leaves
			// its phase and memo as they were (#8082) and only reopens its
			// correlation items. The stale tgt-1 relationship items stay
			// succeeded: #7637 gave the relationship listings the same
			// per-scope replay floor the correlation listing already had.
			unchanged:    partitionSet("git:dep", "dep-1"),
			wholeOutside: outsideKeys(workKeys(correlationIDs("dep-1"))...),
		})
	})

	t.Run("inbound_content_reference_from_another_repo", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.GitRepo("git:in", "in-1", "repo-in", "billing-ui")
		p.TerraformRef("in-1-ref", "git:in", "in-1", "repo-in", "in.tf", "payments-service")
		p.WorkItems("git:in", "in-1")
		p.Prepass()
		p.QuietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		runTargetedDiff(p, "inbound_content_reference_from_another_repo", targetedDiffCase{
			owed:        owedPartitions("git:tgt", "tgt-2"),
			compared:    partitionSet("git:tgt", "tgt-2", "git:in", "in-1"),
			newEvidence: []string{"repo-tgt->repo-dep"},
			published:   partitionSet("git:tgt", "tgt-2", "git:in", "in-1"),
			// in-1 is a same-pass memo hit: its relationship items stay
			// succeeded in both arms; its correlation items reopen in both.
			reopened: concatIDs(workIDs("tgt-2"), correlationIDs("in-1")),
			// in-1 and dep-1 are memo hits with no inserted evidence row, so
			// the whole pass leaves their phase and memo as they were (#8082);
			// the targeted arm republishes in-1 with new timestamps only. No
			// stale tgt-1 relationship reopen: the #7637 replay floor covers
			// the relationship listings too.
			unchanged:    partitionSet("git:in", "in-1", "git:dep", "dep-1"),
			wholeOutside: outsideKeys(workKeys(correlationIDs("dep-1"))...),
		})
	})

	t.Run("cloud_scope_gcp_relation_owed", func(t *testing.T) {
		// Negative control for a target-partition-only design: the owed
		// partition is a cloud scope with no repository fact, and its only
		// effect is evidence attached to ANOTHER scope's repository.
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.GitRepo("git:gsrc", "gsrc-1", "repo-gsrc", "order-gateway")
		p.WorkItems("git:gsrc", "gsrc-1")
		p.Scope(targetedGCPScope)
		p.Generation(targetedGCPScope, "gcp-1", 0, true)
		p.Prepass()
		p.Generation(targetedGCPScope, "gcp-2", 90*time.Minute, true)
		p.GCPRelation("gcp-2-edge", "gcp-2", "order-gateway", "payments-service")
		p.WorkItems(targetedGCPScope, "gcp-2")
		runTargetedDiff(p, "cloud_scope_gcp_relation_owed", targetedDiffCase{
			owed: owedPartitions(targetedGCPScope, "gcp-2"),
			// A cloud scope maps to no repository: no pass can publish its
			// phase, so it is inapplicable, but its evidence work still runs.
			outcomes:    map[string]postgres.TargetedMaintenanceOutcomeKind{targetedGCPScope + "/gcp-2": postgres.TargetedMaintenanceInapplicable},
			compared:    partitionSet("git:gsrc", "gsrc-1"),
			newEvidence: []string{"repo-gsrc->repo-tgt"},
			published:   partitionSet("git:gsrc", "gsrc-1"),
			// gsrc-1 is a memo hit (its own facts did not change), but the pass
			// wrote genuinely new evidence into it (repo-gsrc->repo-tgt from
			// the cloud fact), so BOTH arms reopen its relationship items:
			// the skip set is revised with the rows the backfill actually
			// inserted (issue #7636). Before the fix both arms wrongly left
			// them succeeded.
			reopened: workIDs("gsrc-1"),
			// dep-1 and tgt-1 are memo hits with no inserted evidence row (the
			// new row lands in gsrc-1), so the whole pass leaves their phase
			// and memo as they were (#8082).
			unchanged: partitionSet("git:dep", "dep-1", "git:tgt", "tgt-1"),
			wholeOutside: outsideKeys(concatIDs(
				workKeys(correlationIDs("dep-1")),
				workKeys(correlationIDs("tgt-1")),
				workKeys(workIDs("gcp-2")),
			)...),
		})
	})

	t.Run("cloud_scope_gcp_relation_into_owed_repo", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.GitRepo("git:gsrc", "gsrc-1", "repo-gsrc", "order-gateway")
		p.WorkItems("git:gsrc", "gsrc-1")
		p.Scope(targetedGCPScope)
		p.Generation(targetedGCPScope, "gcp-1", 0, true)
		p.GCPRelation("gcp-1-edge", "gcp-1", "order-gateway", "payments-service")
		p.Prepass()
		p.QuietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		outcome := runTargetedDiff(p, "cloud_scope_gcp_relation_into_owed_repo", targetedDiffCase{
			owed:        owedPartitions("git:tgt", "tgt-2"),
			compared:    partitionSet("git:tgt", "tgt-2", "git:gsrc", "gsrc-1"),
			newEvidence: []string{"repo-tgt->repo-dep"},
			published:   partitionSet("git:tgt", "tgt-2", "git:gsrc", "gsrc-1"),
			reopened:    concatIDs(workIDs("tgt-2"), correlationIDs("gsrc-1")),
			// gsrc-1 (its gcp-1 evidence already committed in the pre-pass)
			// and dep-1 are memo hits with no inserted evidence row, so the
			// whole pass leaves their phase and memo as they were (#8082). No
			// stale tgt-1 relationship reopen: the #7637 replay floor covers
			// the relationship listings too.
			unchanged:    partitionSet("git:gsrc", "gsrc-1", "git:dep", "dep-1"),
			wholeOutside: outsideKeys(workKeys(correlationIDs("dep-1"))...),
		})
		if _, loaded := owedSet(outcome.result.Loaded)[backfill.ScopeGenerationPartition{ScopeID: targetedGCPScope, GenerationID: "gcp-1"}]; !loaded {
			t.Fatalf("closure did not load the GCP cloud scope holding the inbound relation: %v", outcome.result.Loaded)
		}
	})

	t.Run("argocd_applicationset_external_config_repo", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		seedTargetedArgoCD(p, "payments-service")
		p.Prepass()
		p.Generation("git:config", "config-2", 90*time.Minute, true)
		p.Repo("git:config", "config-2", "repo-config", "platform-config")
		p.ArgoConfig("config-2", "orders-api")
		p.WorkItems("git:config", "config-2")
		// The new config names orders-api, so the deploy-source evidence
		// repo-dep -> repo-config (source = deployed repo) is new and lands in
		// dep-1. The ApplicationSet discovery evidence repo-gitops ->
		// repo-config (yaml_iac_evidence.go appendDiscoveryEvidence) touches
		// the owed config repo, so the control partition gitops-1 is affected
		// too. gitops-1 is ArgoCD-bearing and never memoized, so its
		// relationship items reopen; dep-1 is a memo hit, but it received
		// genuinely new evidence this pass, so its relationship items
		// reopen too (issue #7636: the skip set is revised with the rows
		// the backfill actually inserted).
		runTargetedDiff(p, "argocd_applicationset_external_config_repo", targetedDiffCase{
			owed:        owedPartitions("git:config", "config-2"),
			compared:    partitionSet("git:config", "config-2", "git:dep", "dep-1", "git:gitops", "gitops-1"),
			newEvidence: []string{"repo-dep->repo-config"},
			published:   partitionSet("git:config", "config-2", "git:dep", "dep-1", "git:gitops", "gitops-1"),
			reopened:    concatIDs(workIDs("config-2"), workIDs("dep-1"), workIDs("gitops-1")),
		})
	})

	t.Run("two_repos_sharing_one_partition_in_separate_batches", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.GitRepo("git:mono", "mono-1", "repo-m1", "mono-alpha")
		p.Repo("git:mono", "mono-1", "repo-m2", "mono-beta")
		p.WorkItems("git:mono", "mono-1")
		p.Prepass()
		seedTargetedMonoGeneration(p)
		runTargetedDiff(p, "two_repos_sharing_one_partition_in_separate_batches", targetedDiffCase{
			owed:        owedPartitions("git:mono", "mono-2"),
			compared:    partitionSet("git:mono", "mono-2"),
			newEvidence: []string{"repo-m1->repo-dep", "repo-m2->repo-dep"},
			published:   partitionSet("git:mono", "mono-2"),
			reopened:    workIDs("mono-2"),
			batchSize:   1,
		})
	})

	t.Run("empty_evidence", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.Prepass()
		p.QuietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service")
		runTargetedDiff(p, "empty_evidence", targetedDiffCase{
			owed:      owedPartitions("git:tgt", "tgt-2"),
			compared:  partitionSet("git:tgt", "tgt-2"),
			published: partitionSet("git:tgt", "tgt-2"),
			reopened:  workIDs("tgt-2"),
		})
	})

	t.Run("unrelated_quiet_scope", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.GitRepo("git:quiet", "quiet-1", "repo-quiet", "audit-log")
		p.WorkItems("git:quiet", "quiet-1")
		p.Prepass()
		p.QuietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		p.QuietGeneration("git:quiet", "quiet-2", "repo-quiet", "audit-log", "orders-api")
		outcome := runTargetedDiff(p, "unrelated_quiet_scope", targetedDiffCase{
			owed:        owedPartitions("git:tgt", "tgt-2"),
			compared:    partitionSet("git:tgt", "tgt-2"),
			newEvidence: []string{"repo-tgt->repo-dep"},
			published:   partitionSet("git:tgt", "tgt-2"),
			reopened:    workIDs("tgt-2"),
		})
		// The unrelated quiet scope is the whole pass's work only: it owes
		// its own obligation, and the targeted arm must leave it untouched.
		for _, key := range []string{phaseKey("git:quiet", "quiet-2"), memoKey("git:quiet", "quiet-2")} {
			if _, ok := outcome.whole[key]; !ok {
				t.Fatalf("whole arm did not publish %s", key)
			}
			if _, ok := outcome.targeted[key]; ok {
				t.Fatalf("targeted arm published the unrelated quiet scope: %s", key)
			}
		}
	})

	t.Run("single_repository_quiet_generation", func(t *testing.T) {
		// Issue #7638 item 9: the only repository's generation advanced
		// past its memo, so no ACTIVE partition holds a memo row. RED for
		// the wider baseline: the targeted arm must publish the owed
		// partition within one pass once the owed scope's own most recent
		// memo (any generation, matching fingerprint) is accepted. Today
		// the catalog guard refuses no_memo_baseline and the gap holds
		// until a later commit runs the epoch whole pass.
		p := newTargetedDiffPair(t)
		p.GitRepo("git:only", "only-1", "repo-only", "solo-service")
		p.WorkItems("git:only", "only-1")
		p.Prepass()
		p.QuietGeneration("git:only", "only-2", "repo-only", "solo-service")
		runTargetedDiff(p, "single_repository_quiet_generation", targetedDiffCase{
			owed:      owedPartitions("git:only", "only-2"),
			compared:  partitionSet("git:only", "only-2"),
			published: partitionSet("git:only", "only-2"),
			reopened:  workIDs("only-2"),
		})
	})

	t.Run("multi_owed_both_fresh_superseded", func(t *testing.T) {
		// #7638 positive multi-scope: two owed scopes, both superseded
		// past their memos, no active memo anywhere. The wider baseline
		// admits both and the targeted arm must match the whole arm.
		p := newTargetedDiffPair(t)
		p.GitRepo("git:a", "a-1", "repo-a", "alpha-svc")
		p.WorkItems("git:a", "a-1")
		p.GitRepo("git:b", "b-1", "repo-b", "beta-svc")
		p.WorkItems("git:b", "b-1")
		p.Prepass()
		p.QuietGeneration("git:a", "a-2", "repo-a", "alpha-svc")
		p.QuietGeneration("git:b", "b-2", "repo-b", "beta-svc")
		runTargetedDiff(p, "multi_owed_both_fresh_superseded", targetedDiffCase{
			owed:      owedPartitions("git:a", "a-2", "git:b", "b-2"),
			compared:  partitionSet("git:a", "a-2", "git:b", "b-2"),
			published: partitionSet("git:a", "a-2", "git:b", "b-2"),
			reopened:  concatIDs(workIDs("a-2"), workIDs("b-2")),
		})
	})

	t.Run("memo_hit_and_miss", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.GitRepo("git:in", "in-1", "repo-in", "billing-ui")
		p.TerraformRef("in-1-ref", "git:in", "in-1", "repo-in", "in.tf", "payments-service")
		p.WorkItems("git:in", "in-1")
		p.GitRepo("git:in2", "in2-1", "repo-in2", "ledger-ui")
		p.TerraformRef("in2-1-ref", "git:in2", "in2-1", "repo-in2", "in2.tf", "payments-service")
		p.WorkItems("git:in2", "in2-1")
		p.Prepass()
		// in2-1 keeps its committed evidence but loses its memo row: the
		// tolerated "publication withheld" state, a same-pass memo miss.
		p.Exec("DELETE FROM deferred_backfill_partition_memo WHERE scope_id = 'git:in2'")
		p.QuietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		runTargetedDiff(p, "memo_hit_and_miss", targetedDiffCase{
			owed:        owedPartitions("git:tgt", "tgt-2"),
			compared:    partitionSet("git:tgt", "tgt-2", "git:in", "in-1", "git:in2", "in2-1"),
			newEvidence: []string{"repo-tgt->repo-dep"},
			published:   partitionSet("git:tgt", "tgt-2", "git:in", "in-1", "git:in2", "in2-1"),
			reopened:    concatIDs(workIDs("tgt-2"), correlationIDs("in-1"), workIDs("in2-1")),
			// in-1 is a memo hit with no inserted evidence row, so the whole
			// pass leaves its phase and memo as they were (#8082); in2-1 is a
			// memo miss and publishes in both arms.
			unchanged: partitionSet("git:in", "in-1"),
		})
	})
}
