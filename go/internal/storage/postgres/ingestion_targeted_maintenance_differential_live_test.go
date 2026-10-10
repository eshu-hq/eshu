// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"
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
		p.prepass()
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		p.run("direct_outgoing_reference", targetedDiffCase{
			owed:        owedPartitions("git:tgt", "tgt-2"),
			compared:    partitionSet("git:tgt", "tgt-2"),
			newEvidence: []string{"repo-tgt->repo-dep"},
			published:   partitionSet("git:tgt", "tgt-2"),
			reopened:    workIDs("tgt-2"),
			// The whole pass also rewrites the untouched dep partition's phase
			// and memo and reopens its correlation items. The stale tgt-1
			// relationship items stay succeeded: #7637 gave the relationship
			// listings the same per-scope replay floor the correlation
			// listing already had.
			wholeOutside: outsideKeys(concatIDs(
				[]string{phaseKey("git:dep", "dep-1"), memoKey("git:dep", "dep-1")},
				workKeys(correlationIDs("dep-1")),
			)...),
		})
	})

	t.Run("inbound_content_reference_from_another_repo", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.gitRepo("git:in", "in-1", "repo-in", "billing-ui")
		p.terraformRef("in-1-ref", "git:in", "in-1", "repo-in", "in.tf", "payments-service")
		p.workItems("git:in", "in-1")
		p.prepass()
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		p.run("inbound_content_reference_from_another_repo", targetedDiffCase{
			owed:        owedPartitions("git:tgt", "tgt-2"),
			compared:    partitionSet("git:tgt", "tgt-2", "git:in", "in-1"),
			newEvidence: []string{"repo-tgt->repo-dep"},
			published:   partitionSet("git:tgt", "tgt-2", "git:in", "in-1"),
			// in-1 is a same-pass memo hit: its relationship items stay
			// succeeded in both arms; its correlation items reopen in both.
			reopened: concatIDs(workIDs("tgt-2"), correlationIDs("in-1")),
			// No stale tgt-1 relationship reopen: the #7637 replay floor
			// covers the relationship listings too.
			wholeOutside: outsideKeys(concatIDs(
				[]string{phaseKey("git:dep", "dep-1"), memoKey("git:dep", "dep-1")},
				workKeys(correlationIDs("dep-1")),
			)...),
		})
	})

	t.Run("cloud_scope_gcp_relation_owed", func(t *testing.T) {
		// Negative control for a target-partition-only design: the owed
		// partition is a cloud scope with no repository fact, and its only
		// effect is evidence attached to ANOTHER scope's repository.
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.gitRepo("git:gsrc", "gsrc-1", "repo-gsrc", "order-gateway")
		p.workItems("git:gsrc", "gsrc-1")
		p.scope(targetedGCPScope)
		p.generation(targetedGCPScope, "gcp-1", 0, true)
		p.prepass()
		p.generation(targetedGCPScope, "gcp-2", 90*time.Minute, true)
		p.gcpRelation("gcp-2-edge", "gcp-2", "order-gateway", "payments-service")
		p.workItems(targetedGCPScope, "gcp-2")
		p.run("cloud_scope_gcp_relation_owed", targetedDiffCase{
			owed: owedPartitions(targetedGCPScope, "gcp-2"),
			// A cloud scope maps to no repository: no pass can publish its
			// phase, so it is inapplicable, but its evidence work still runs.
			outcomes:    map[string]TargetedMaintenanceOutcomeKind{targetedGCPScope + "/gcp-2": TargetedMaintenanceInapplicable},
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
			wholeOutside: outsideKeys(concatIDs(
				[]string{
					phaseKey("git:dep", "dep-1"), memoKey("git:dep", "dep-1"),
					phaseKey("git:tgt", "tgt-1"), memoKey("git:tgt", "tgt-1"),
				},
				workKeys(correlationIDs("dep-1")),
				workKeys(correlationIDs("tgt-1")),
				workKeys(workIDs("gcp-2")),
			)...),
		})
	})

	t.Run("cloud_scope_gcp_relation_into_owed_repo", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.gitRepo("git:gsrc", "gsrc-1", "repo-gsrc", "order-gateway")
		p.workItems("git:gsrc", "gsrc-1")
		p.scope(targetedGCPScope)
		p.generation(targetedGCPScope, "gcp-1", 0, true)
		p.gcpRelation("gcp-1-edge", "gcp-1", "order-gateway", "payments-service")
		p.prepass()
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		outcome := p.run("cloud_scope_gcp_relation_into_owed_repo", targetedDiffCase{
			owed:        owedPartitions("git:tgt", "tgt-2"),
			compared:    partitionSet("git:tgt", "tgt-2", "git:gsrc", "gsrc-1"),
			newEvidence: []string{"repo-tgt->repo-dep"},
			published:   partitionSet("git:tgt", "tgt-2", "git:gsrc", "gsrc-1"),
			reopened:    concatIDs(workIDs("tgt-2"), correlationIDs("gsrc-1")),
			// No stale tgt-1 relationship reopen: the #7637 replay floor
			// covers the relationship listings too.
			wholeOutside: outsideKeys(concatIDs(
				[]string{phaseKey("git:dep", "dep-1"), memoKey("git:dep", "dep-1")},
				workKeys(correlationIDs("dep-1")),
			)...),
		})
		if _, loaded := owedSet(outcome.result.Loaded)[scopeGenerationPartition{ScopeID: targetedGCPScope, GenerationID: "gcp-1"}]; !loaded {
			t.Fatalf("closure did not load the GCP cloud scope holding the inbound relation: %v", outcome.result.Loaded)
		}
	})

	t.Run("argocd_applicationset_external_config_repo", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		seedTargetedArgoCD(p, "payments-service")
		p.prepass()
		p.generation("git:config", "config-2", 90*time.Minute, true)
		p.repo("git:config", "config-2", "repo-config", "platform-config")
		p.argoConfig("config-2", "orders-api")
		p.workItems("git:config", "config-2")
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
		p.run("argocd_applicationset_external_config_repo", targetedDiffCase{
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
		p.gitRepo("git:mono", "mono-1", "repo-m1", "mono-alpha")
		p.repo("git:mono", "mono-1", "repo-m2", "mono-beta")
		p.workItems("git:mono", "mono-1")
		p.prepass()
		seedTargetedMonoGeneration(p)
		p.run("two_repos_sharing_one_partition_in_separate_batches", targetedDiffCase{
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
		p.prepass()
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service")
		p.run("empty_evidence", targetedDiffCase{
			owed:      owedPartitions("git:tgt", "tgt-2"),
			compared:  partitionSet("git:tgt", "tgt-2"),
			published: partitionSet("git:tgt", "tgt-2"),
			reopened:  workIDs("tgt-2"),
		})
	})

	t.Run("unrelated_quiet_scope", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.gitRepo("git:quiet", "quiet-1", "repo-quiet", "audit-log")
		p.workItems("git:quiet", "quiet-1")
		p.prepass()
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		p.quietGeneration("git:quiet", "quiet-2", "repo-quiet", "audit-log", "orders-api")
		outcome := p.run("unrelated_quiet_scope", targetedDiffCase{
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
		p.gitRepo("git:only", "only-1", "repo-only", "solo-service")
		p.workItems("git:only", "only-1")
		p.prepass()
		p.quietGeneration("git:only", "only-2", "repo-only", "solo-service")
		p.run("single_repository_quiet_generation", targetedDiffCase{
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
		p.gitRepo("git:a", "a-1", "repo-a", "alpha-svc")
		p.workItems("git:a", "a-1")
		p.gitRepo("git:b", "b-1", "repo-b", "beta-svc")
		p.workItems("git:b", "b-1")
		p.prepass()
		p.quietGeneration("git:a", "a-2", "repo-a", "alpha-svc")
		p.quietGeneration("git:b", "b-2", "repo-b", "beta-svc")
		p.run("multi_owed_both_fresh_superseded", targetedDiffCase{
			owed:      owedPartitions("git:a", "a-2", "git:b", "b-2"),
			compared:  partitionSet("git:a", "a-2", "git:b", "b-2"),
			published: partitionSet("git:a", "a-2", "git:b", "b-2"),
			reopened:  concatIDs(workIDs("a-2"), workIDs("b-2")),
		})
	})

	t.Run("memo_hit_and_miss", func(t *testing.T) {
		p := newTargetedDiffPair(t)
		seedTargetedCorpus(p)
		p.gitRepo("git:in", "in-1", "repo-in", "billing-ui")
		p.terraformRef("in-1-ref", "git:in", "in-1", "repo-in", "in.tf", "payments-service")
		p.workItems("git:in", "in-1")
		p.gitRepo("git:in2", "in2-1", "repo-in2", "ledger-ui")
		p.terraformRef("in2-1-ref", "git:in2", "in2-1", "repo-in2", "in2.tf", "payments-service")
		p.workItems("git:in2", "in2-1")
		p.prepass()
		// in2-1 keeps its committed evidence but loses its memo row: the
		// tolerated "publication withheld" state, a same-pass memo miss.
		p.exec("DELETE FROM deferred_backfill_partition_memo WHERE scope_id = 'git:in2'")
		p.quietGeneration("git:tgt", "tgt-2", "repo-tgt", "payments-service", "orders-api")
		p.run("memo_hit_and_miss", targetedDiffCase{
			owed:        owedPartitions("git:tgt", "tgt-2"),
			compared:    partitionSet("git:tgt", "tgt-2", "git:in", "in-1", "git:in2", "in2-1"),
			newEvidence: []string{"repo-tgt->repo-dep"},
			published:   partitionSet("git:tgt", "tgt-2", "git:in", "in-1", "git:in2", "in2-1"),
			reopened:    concatIDs(workIDs("tgt-2"), correlationIDs("in-1"), workIDs("in2-1")),
		})
	})
}
