// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// DSN-gated DB-integration proof for RepositoryFreshnessStore (issue #5148,
// follow-up from the #5143 cold review, P2 runtime-proof-gap).
//
// RepositoryFreshnessStore's composite SQL (repository_freshness_sql.go) was
// unit-tested only against a fakeQueryer that returns canned rows regardless
// of the query text. Real-DB semantics -- the latest_generations join
// resolution, the DB-level succeeded-status exclusion in
// repositoryFreshnessStageCountsQuery, the repository_id scoping of
// repositoryFreshnessSharedPendingQuery, and the identity coupling between a
// canonical repository id and fact.payload->>'repo_id' -- were proven once on
// a live Compose stack during #5143 but had no durable CI gate. In
// particular, the succeeded-leak regression that #5143 fixed was guarded only
// by a query-text substring assertion (TestRepositoryFreshnessStageCounts...
// ExcludesSucceededStatus in repository_freshness_test.go): a predicate that
// referenced the right status column but the WRONG status values would still
// pass that assertion. This file drives the real ReadRepositoryFreshness path
// against a live Postgres instance so the row-level exclusion is proven, not
// just the query text. It mirrors webhook_refresh_proof_integration_test.go's
// DSN-gated, schema-per-test-run conventions and skips cleanly when no DSN is
// configured so the hermetic unit suite stays green without Postgres.
//
// It also covers two low-confidence, under-report-only nuances flagged by the
// same #5143 review:
//
//  1. readUnobservedPush ordering: a newest queued/claimed webhook trigger
//     whose target_sha matches the observed commit must not mask an OLDER
//     trigger whose target_sha does not match -- proven here against real
//     Postgres row ordering, not a hand-ordered fakeQueryer response slice.
//  2. repository_full_name/repo_slug format alignment: RepoSlugFromRemoteURL
//     (repositoryidentity/identity.go) lower-cases the owner/repo slug it
//     derives from a git remote, but the webhook normalizers
//     (webhook/normalizer_github.go and siblings) store repository_full_name
//     verbatim from the provider payload, which preserves the repo owner's
//     actual casing. Before this change, repositoryFreshnessWebhookQuery
//     compared the two with case-sensitive SQL equality, so any repository
//     whose real GitHub/GitLab/etc. name contains uppercase characters would
//     silently fail to surface an otherwise-real unobserved push -- an
//     under-report, never a false positive. The fix folds both sides of the
//     comparison to the same case.
//
// Performance Evidence: repository_full_name carries no index in
// webhook_refresh_triggers (only (status, updated_at) and
// (status, received_at, trigger_id) are indexed); the query already applies
// this predicate as a residual Filter after the status/LIMIT-bound index scan,
// so wrapping both sides in LOWER() changes zero index usage and zero query
// shape. No-Regression Evidence: this is a pure correctness fix on an
// unindexed equality predicate; TestRepositoryFreshnessStageCountsQuery...
// ExcludesSucceededStatus and the rest of repository_freshness_test.go's
// fakeQueryer suite are unaffected since they assert Go-level dispatch, not
// SQL text equality on this predicate. Observability Evidence: no new metric,
// span, or log is added; this store already records
// eshu_dp_repository_freshness_query_duration_seconds and
// eshu_dp_repository_freshness_query_errors_total on every read
// (repository_freshness.go), which cover this query unchanged.
//
// Split across two files to stay under the repository's 500-line file cap:
// this file holds the DSN gate and TestReadRepositoryFreshnessLiveDB's
// subtests; repository_freshness_db_integration_schema_test.go holds the
// throwaway-schema bootstrap and fixture/seed helpers they share.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

const repositoryFreshnessDBIntegrationDSNEnv = "ESHU_POSTGRES_DSN"

func repositoryFreshnessDBIntegrationDSN() string {
	return strings.TrimSpace(os.Getenv(repositoryFreshnessDBIntegrationDSNEnv))
}

// TestReadRepositoryFreshnessLiveDB is the issue #5148 gate. Each subtest
// seeds its own scope/generation/repository fixture in an isolated
// throwaway schema and drives the real ReadRepositoryFreshness path against
// live Postgres, so subtests are independent and order-insensitive.
func TestReadRepositoryFreshnessLiveDB(t *testing.T) {
	dsn := repositoryFreshnessDBIntegrationDSN()
	if dsn == "" {
		t.Skipf("%s is not set; skipping repository freshness Postgres DB-integration proof", repositoryFreshnessDBIntegrationDSNEnv)
	}

	ctx := context.Background()
	db := openRepositoryFreshnessDBIntegrationSchema(t, ctx, dsn)
	store := NewRepositoryFreshnessStore(SQLDB{DB: db})
	now := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	t.Run("fully drained generation reports current stages with empty outstanding", func(t *testing.T) {
		fx := freshnessScopeFixture{
			scopeID:        "scope-drained",
			generationID:   "gen-drained",
			repoID:         "repository:drained-1",
			repoSlug:       "acme/drained-repo",
			observedCommit: "drained0000000000000000000000000000000",
			now:            now,
		}
		seedRepositoryFreshnessScope(t, ctx, db, fx)
		// Every stage already succeeded; the grouped stage-counts query must
		// exclude these rows at the database, proving the #5143 succeeded-leak
		// fix live rather than asserting only the query's SQL text.
		seedRepositoryFreshnessWorkItem(t, ctx, db, fx.scopeID, fx.generationID, "wi-drained-1", "reducer", "succeeded", now)
		seedRepositoryFreshnessWorkItem(t, ctx, db, fx.scopeID, fx.generationID, "wi-drained-2", "projector", "succeeded", now)
		completedAt := now
		seedRepositoryFreshnessSharedIntent(t, ctx, db, "intent-drained-1", "deployment_mapping", fx.repoID, fx.generationID, now, &completedAt)

		snapshot, err := store.ReadRepositoryFreshness(ctx, fx.repoID)
		if err != nil {
			t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
		}
		if !snapshot.Resolved || !snapshot.HasGeneration {
			t.Fatalf("snapshot = %+v, want Resolved=true HasGeneration=true", snapshot)
		}
		if len(snapshot.Outstanding) != 0 {
			t.Fatalf("Outstanding = %+v, want empty: succeeded rows must not leak from a live Postgres read", snapshot.Outstanding)
		}
		if !snapshot.Stages.Collected || !snapshot.Stages.Reduced || !snapshot.Stages.Projected || !snapshot.Stages.Materialized {
			t.Fatalf("Stages = %+v, want all true for a fully drained generation", snapshot.Stages)
		}
		if snapshot.SharedEnrichment.Pending {
			t.Fatalf("SharedEnrichment = %+v, want Pending=false when the only intent is completed", snapshot.SharedEnrichment)
		}
	})

	t.Run("outstanding reducer rows report Reduced=false with accurate live counts", func(t *testing.T) {
		fx := freshnessScopeFixture{
			scopeID:        "scope-outstanding",
			generationID:   "gen-outstanding",
			repoID:         "repository:outstanding-1",
			repoSlug:       "acme/outstanding-repo",
			observedCommit: "outstanding000000000000000000000000000",
			now:            now,
		}
		seedRepositoryFreshnessScope(t, ctx, db, fx)
		seedRepositoryFreshnessWorkItem(t, ctx, db, fx.scopeID, fx.generationID, "wi-outstanding-1", "reducer", "pending", now)
		seedRepositoryFreshnessWorkItem(t, ctx, db, fx.scopeID, fx.generationID, "wi-outstanding-2", "reducer", "pending", now)
		// Decoy succeeded rows for both stages: they must not be counted or
		// flip Reduced/Projected, and projector must read fully drained.
		seedRepositoryFreshnessWorkItem(t, ctx, db, fx.scopeID, fx.generationID, "wi-outstanding-3", "reducer", "succeeded", now)
		seedRepositoryFreshnessWorkItem(t, ctx, db, fx.scopeID, fx.generationID, "wi-outstanding-4", "projector", "succeeded", now)

		snapshot, err := store.ReadRepositoryFreshness(ctx, fx.repoID)
		if err != nil {
			t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
		}
		if snapshot.Stages.Reduced {
			t.Fatal("Stages.Reduced = true, want false for two outstanding reducer rows")
		}
		if !snapshot.Stages.Projected {
			t.Fatal("Stages.Projected = false, want true: only a succeeded projector row exists")
		}
		if len(snapshot.Outstanding) != 1 || snapshot.Outstanding[0].Stage != "reducer" ||
			snapshot.Outstanding[0].Status != "pending" || snapshot.Outstanding[0].Count != 2 {
			t.Fatalf("Outstanding = %+v, want exactly one {reducer,pending,2} row from live Postgres", snapshot.Outstanding)
		}
	})

	t.Run("shared-intent pending is scoped by repository_id, not global", func(t *testing.T) {
		fx := freshnessScopeFixture{
			scopeID:        "scope-shared",
			generationID:   "gen-shared",
			repoID:         "repository:shared-1",
			repoSlug:       "acme/shared-repo",
			observedCommit: "shared0000000000000000000000000000000",
			now:            now,
		}
		seedRepositoryFreshnessScope(t, ctx, db, fx)
		seedRepositoryFreshnessWorkItem(t, ctx, db, fx.scopeID, fx.generationID, "wi-shared-1", "reducer", "succeeded", now)
		seedRepositoryFreshnessWorkItem(t, ctx, db, fx.scopeID, fx.generationID, "wi-shared-2", "projector", "succeeded", now)
		seedRepositoryFreshnessSharedIntent(t, ctx, db, "intent-shared-own", "deployment_mapping", fx.repoID, fx.generationID, now, nil)

		// Decoy: a pending intent for a DIFFERENT repository_id/generation must
		// not leak into this repository's SharedEnrichment.
		decoyFx := freshnessScopeFixture{
			scopeID:        "scope-shared-decoy",
			generationID:   "gen-shared-decoy",
			repoID:         "repository:shared-decoy",
			repoSlug:       "acme/shared-decoy-repo",
			observedCommit: "shareddecoy000000000000000000000000000",
			now:            now,
		}
		seedRepositoryFreshnessScope(t, ctx, db, decoyFx)
		seedRepositoryFreshnessSharedIntent(t, ctx, db, "intent-shared-decoy", "deployment_mapping", decoyFx.repoID, decoyFx.generationID, now, nil)

		snapshot, err := store.ReadRepositoryFreshness(ctx, fx.repoID)
		if err != nil {
			t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
		}
		if !snapshot.SharedEnrichment.Pending {
			t.Fatal("SharedEnrichment.Pending = false, want true for one own-repository pending intent")
		}
		if len(snapshot.SharedEnrichment.PendingDomains) != 1 ||
			snapshot.SharedEnrichment.PendingDomains[0].Domain != "deployment_mapping" ||
			snapshot.SharedEnrichment.PendingDomains[0].Count != 1 {
			t.Fatalf("PendingDomains = %+v, want exactly one {deployment_mapping,1} row (the decoy repository's intent must not leak in)", snapshot.SharedEnrichment.PendingDomains)
		}
		if snapshot.Stages.Materialized {
			t.Fatal("Stages.Materialized = true, want false while a shared domain is pending")
		}
	})

	t.Run("queued webhook trigger surfaces as unobserved push via the live selector store", func(t *testing.T) {
		fx := freshnessScopeFixture{
			scopeID:        "scope-webhook",
			generationID:   "gen-webhook",
			repoID:         "repository:webhook-1",
			repoSlug:       "acme/webhook-repo",
			observedCommit: "webhookobserved0000000000000000000000",
			now:            now,
		}
		seedRepositoryFreshnessScope(t, ctx, db, fx)
		seedRepositoryFreshnessWorkItem(t, ctx, db, fx.scopeID, fx.generationID, "wi-webhook-1", "reducer", "succeeded", now)
		seedRepositoryFreshnessWorkItem(t, ctx, db, fx.scopeID, fx.generationID, "wi-webhook-2", "projector", "succeeded", now)

		webhookStore := NewWebhookTriggerStore(SQLDB{DB: db})
		trigger := webhook.Trigger{
			Provider:             webhook.ProviderGitHub,
			EventKind:            webhook.EventKindPush,
			Decision:             webhook.DecisionAccepted,
			DeliveryID:           "delivery-webhook-1",
			RepositoryExternalID: "webhook-external-1",
			RepositoryFullName:   fx.repoSlug,
			DefaultBranch:        "main",
			Ref:                  "refs/heads/main",
			TargetSHA:            "webhookpushedsha00000000000000000000",
		}
		if _, err := webhookStore.StoreTrigger(ctx, trigger, now); err != nil {
			t.Fatalf("StoreTrigger() error = %v, want nil", err)
		}

		snapshot, err := store.ReadRepositoryFreshness(ctx, fx.repoID)
		if err != nil {
			t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
		}
		if snapshot.UnobservedPush == nil {
			t.Fatal("UnobservedPush = nil, want the live-stored queued trigger to surface")
		}
		if snapshot.UnobservedPush.TargetSHA != trigger.TargetSHA || snapshot.UnobservedPush.Ref != trigger.Ref {
			t.Fatalf("UnobservedPush = %+v, want target_sha=%q ref=%q", snapshot.UnobservedPush, trigger.TargetSHA, trigger.Ref)
		}
	})

	t.Run("canonical repository id couples to payload repo_id without cross-repository collision", func(t *testing.T) {
		idA, err := repositoryidentity.CanonicalRepositoryID("https://github.com/acme/coupling-one", "")
		if err != nil {
			t.Fatalf("CanonicalRepositoryID(coupling-one) error = %v, want nil", err)
		}
		idB, err := repositoryidentity.CanonicalRepositoryID("https://github.com/acme/coupling-two", "")
		if err != nil {
			t.Fatalf("CanonicalRepositoryID(coupling-two) error = %v, want nil", err)
		}
		if idA == idB {
			t.Fatalf("CanonicalRepositoryID collided for two distinct remotes: %q", idA)
		}

		fxA := freshnessScopeFixture{
			scopeID:        "scope-coupling-a",
			generationID:   "gen-coupling-a",
			repoID:         idA,
			repoSlug:       "acme/coupling-one",
			observedCommit: "couplingaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			now:            now,
		}
		fxB := freshnessScopeFixture{
			scopeID:        "scope-coupling-b",
			generationID:   "gen-coupling-b",
			repoID:         idB,
			repoSlug:       "acme/coupling-two",
			observedCommit: "couplingbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			now:            now,
		}
		seedRepositoryFreshnessScope(t, ctx, db, fxA)
		seedRepositoryFreshnessScope(t, ctx, db, fxB)

		snapshotA, err := store.ReadRepositoryFreshness(ctx, idA)
		if err != nil {
			t.Fatalf("ReadRepositoryFreshness(idA) error = %v, want nil", err)
		}
		if !snapshotA.Resolved || snapshotA.ScopeID != fxA.scopeID || snapshotA.ObservedCommit != fxA.observedCommit {
			t.Fatalf("snapshotA = %+v, want resolved scope %q observed commit %q", snapshotA, fxA.scopeID, fxA.observedCommit)
		}

		snapshotB, err := store.ReadRepositoryFreshness(ctx, idB)
		if err != nil {
			t.Fatalf("ReadRepositoryFreshness(idB) error = %v, want nil", err)
		}
		if !snapshotB.Resolved || snapshotB.ScopeID != fxB.scopeID || snapshotB.ObservedCommit != fxB.observedCommit {
			t.Fatalf("snapshotB = %+v, want resolved scope %q observed commit %q", snapshotB, fxB.scopeID, fxB.observedCommit)
		}
	})

	t.Run("newest webhook trigger matching the observed commit does not mask an older mismatch", func(t *testing.T) {
		fx := freshnessScopeFixture{
			scopeID:        "scope-nuance-order",
			generationID:   "gen-nuance-order",
			repoID:         "repository:nuance-order-1",
			repoSlug:       "acme/nuance-order-repo",
			observedCommit: "nuanceorderobserved00000000000000000",
			now:            now,
		}
		seedRepositoryFreshnessScope(t, ctx, db, fx)

		webhookStore := NewWebhookTriggerStore(SQLDB{DB: db})
		older := webhook.Trigger{
			Provider:             webhook.ProviderGitHub,
			EventKind:            webhook.EventKindPush,
			Decision:             webhook.DecisionAccepted,
			DeliveryID:           "delivery-nuance-order-older",
			RepositoryExternalID: "nuance-order-external",
			RepositoryFullName:   fx.repoSlug,
			DefaultBranch:        "main",
			Ref:                  "refs/heads/main",
			TargetSHA:            "olderMismatchSha00000000000000000000",
		}
		newer := webhook.Trigger{
			Provider:             webhook.ProviderGitHub,
			EventKind:            webhook.EventKindPush,
			Decision:             webhook.DecisionAccepted,
			DeliveryID:           "delivery-nuance-order-newer",
			RepositoryExternalID: "nuance-order-external",
			RepositoryFullName:   fx.repoSlug,
			DefaultBranch:        "main",
			Ref:                  "refs/heads/main",
			TargetSHA:            fx.observedCommit, // matches: already built, not unobserved
		}
		// The newest trigger by received_at matches the observed commit; an
		// older trigger, received first, does not. Store the older one with an
		// earlier received_at so real Postgres ORDER BY received_at DESC puts
		// the matching row first -- the exact shape that would mask the older
		// mismatch if readUnobservedPush stopped at the first row instead of
		// scanning until it finds a genuine mismatch.
		if _, err := webhookStore.StoreTrigger(ctx, older, now.Add(-2*time.Minute)); err != nil {
			t.Fatalf("StoreTrigger(older) error = %v, want nil", err)
		}
		if _, err := webhookStore.StoreTrigger(ctx, newer, now.Add(-1*time.Minute)); err != nil {
			t.Fatalf("StoreTrigger(newer) error = %v, want nil", err)
		}

		snapshot, err := store.ReadRepositoryFreshness(ctx, fx.repoID)
		if err != nil {
			t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
		}
		if snapshot.UnobservedPush == nil {
			t.Fatal("UnobservedPush = nil, want the older mismatching trigger to still surface")
		}
		if snapshot.UnobservedPush.TargetSHA != older.TargetSHA {
			t.Fatalf("UnobservedPush.TargetSHA = %q, want %q (the older mismatch, not masked by the newer matching trigger)", snapshot.UnobservedPush.TargetSHA, older.TargetSHA)
		}
	})

	t.Run("webhook repository_full_name case must align with the lower-cased repo_slug", func(t *testing.T) {
		// RepoSlugFromRemoteURL (repositoryidentity/identity.go) lower-cases the
		// owner/repo slug it derives from a git remote, but GitHub (and sibling
		// provider normalizers) store repository_full_name verbatim from the
		// provider payload, which preserves the repository owner's actual
		// casing. Before the LOWER()/LOWER() fix in
		// repositoryFreshnessWebhookQuery, any repository whose real name
		// contains uppercase characters would never match here, silently
		// under-reporting a genuine unobserved push.
		fx := freshnessScopeFixture{
			scopeID:        "scope-nuance-case",
			generationID:   "gen-nuance-case",
			repoID:         "repository:nuance-case-1",
			repoSlug:       "acme/mixedcase-repo", // lower-cased, as RepoSlugFromRemoteURL produces
			observedCommit: "nuancecaseobserved00000000000000000000",
			now:            now,
		}
		seedRepositoryFreshnessScope(t, ctx, db, fx)

		webhookStore := NewWebhookTriggerStore(SQLDB{DB: db})
		trigger := webhook.Trigger{
			Provider:             webhook.ProviderGitHub,
			EventKind:            webhook.EventKindPush,
			Decision:             webhook.DecisionAccepted,
			DeliveryID:           "delivery-nuance-case",
			RepositoryExternalID: "nuance-case-external",
			RepositoryFullName:   "Acme/MixedCase-Repo", // case-preserved, as GitHub's full_name reports it
			DefaultBranch:        "main",
			Ref:                  "refs/heads/main",
			TargetSHA:            "caseMismatchSha000000000000000000000",
		}
		if _, err := webhookStore.StoreTrigger(ctx, trigger, now); err != nil {
			t.Fatalf("StoreTrigger() error = %v, want nil", err)
		}

		snapshot, err := store.ReadRepositoryFreshness(ctx, fx.repoID)
		if err != nil {
			t.Fatalf("ReadRepositoryFreshness() error = %v, want nil", err)
		}
		if snapshot.UnobservedPush == nil {
			t.Fatal("UnobservedPush = nil, want the queued trigger to surface despite the repository_full_name/repo_slug case difference")
		}
		if snapshot.UnobservedPush.TargetSHA != trigger.TargetSHA {
			t.Fatalf("UnobservedPush.TargetSHA = %q, want %q", snapshot.UnobservedPush.TargetSHA, trigger.TargetSHA)
		}
	})
}
