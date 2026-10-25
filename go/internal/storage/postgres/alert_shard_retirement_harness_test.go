// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/alerts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/security/alerts/unread"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// The #9019 shard-retirement live proofs drive the production paths a shard
// scope goes through: the claim-fenced ingestion commit (the #7926 successor
// guard included), the projector service with the real projector runtime and
// queue, the reducer-owned reconciliation fact writer, and the query stores the
// list, count, and inventory routes read. Only the provider and the reducer's
// decision step are replaced, by literal alerts and decisions.

const (
	shardLiveBaseScopeID = "security-alert:github-org:acme"
	shardLiveProvider    = "github_dependabot"
)

// shardLiveAlert is one provider alert of one repository.
type shardLiveAlert struct {
	repository string
	number     int
}

// shardLiveClaim seeds one claimed work item for generationID on scopeID and
// returns the mutation that holds it, so the claim-fenced commit passes its
// HeartbeatClaim check.
func shardLiveClaim(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) workflow.ClaimMutation {
	t.Helper()
	item, claim := "item-"+generationID, "claim-"+generationID
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_work_items (
    work_item_id, run_id, collector_kind, collector_instance_id, source_system,
    scope_id, acceptance_unit_id, source_run_id, generation_id, status, attempt_count,
    current_claim_id, current_owner_id, current_fencing_token, lease_expires_at, created_at, updated_at
) VALUES ($1, 'run-live', 'security_alert', 'inst-live', 'security_alert', $3, $3, $2, $2,
          'claimed', 1, $4, 'owner-live', 1, now() + interval '1 hour', now(), now())`,
		item, generationID, scopeID, claim,
	); err != nil {
		t.Fatalf("seed work item %s: %v", generationID, err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_claims (
    claim_id, work_item_id, fencing_token, owner_id, status, claimed_at, heartbeat_at,
    lease_expires_at, created_at, updated_at
) VALUES ($1, $2, 1, 'owner-live', 'active', now(), now(), now() + interval '1 hour', now(), now())`,
		claim, item,
	); err != nil {
		t.Fatalf("seed claim %s: %v", generationID, err)
	}
	return workflow.ClaimMutation{
		WorkItemID: item, ClaimID: claim, FencingToken: 1, OwnerID: "owner-live",
		LeaseDuration: time.Hour, ObservedAt: time.Now().UTC(),
	}
}

// shardLiveGeneration is one generation a shard-retirement proof commits.
type shardLiveGeneration struct {
	scopeID       string
	parentScopeID string
	generationID  string
	hint          string
	alerts        []shardLiveAlert
	// repositoriesTotal is the read state's repositories_total.
	repositoriesTotal int
	trustedDeletion   bool
}

// shardLiveCommit commits one generation through the production claim-fenced
// path with its read state, exactly as the claimed service does.
func shardLiveCommit(t *testing.T, ctx context.Context, database *sql.DB, store postgres.IngestionStore, g shardLiveGeneration) error {
	t.Helper()
	mutation := shardLiveClaim(t, ctx, database, g.scopeID, g.generationID)
	now := time.Now().UTC()
	scopeValue := scope.IngestionScope{
		ScopeID:       g.scopeID,
		ParentScopeID: g.parentScopeID,
		SourceSystem:  string(scope.CollectorSecurityAlert),
		ScopeKind:     scope.KindSecurityAlert,
		CollectorKind: scope.CollectorSecurityAlert,
		PartitionKey:  shardLiveProvider,
		Metadata:      map[string]string{"provider": shardLiveProvider},
	}
	generation := scope.ScopeGeneration{
		GenerationID: g.generationID, ScopeID: g.scopeID, ObservedAt: now, IngestedAt: now,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		FreshnessHint: g.hint, TrustedDeletion: g.trustedDeletion,
	}
	stream := make(chan facts.Envelope, len(g.alerts))
	for _, alert := range g.alerts {
		env, err := securityalerts.NewGitHubDependabotAlertEnvelope(securityalerts.EnvelopeContext{
			ScopeID: g.scopeID, RepositoryID: "security-alert:github:" + alert.repository,
			GenerationID: g.generationID, CollectorInstanceID: "inst-live", FencingToken: 1, ObservedAt: now,
		}, securityalerts.GitHubDependabotAlert{
			Number: alert.number, State: "open",
			Dependency: securityalerts.GitHubDependabotDependency{
				Package:      securityalerts.GitHubDependabotPackage{Ecosystem: "npm", Name: "left-pad"},
				ManifestPath: "package.json",
			},
		})
		if err != nil {
			t.Fatalf("build alert envelope: %v", err)
		}
		stream <- env
	}
	close(stream)
	state := securityalerts.TargetReadState{
		GenerationID: g.generationID, ScopeID: g.scopeID, RepositoriesTotal: g.repositoriesTotal, ObservedAt: now,
	}
	return store.CommitClaimedScopeGenerationWithState(ctx, mutation, scopeValue, generation, stream, nil, unread.Writer(state))
}

// shardLiveProject runs the projector service with the production runtime,
// queue, and fact store until no projector work is left, then returns.
func shardLiveProject(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	runCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	queue := postgres.NewProjectorQueue(postgres.SQLDB{DB: database}, "projector-live", time.Minute)
	service := projector.Service{
		WorkSource:         queue,
		FactStore:          postgres.NewFactStore(postgres.SQLDB{DB: database}),
		Runner:             runtime.Runtime{IntentWriter: postgres.NewReducerQueue(postgres.SQLDB{DB: database}, "reducer-live", time.Minute)},
		WorkSink:           queue,
		DeltaBaselineFence: queue,
		WriteMarker:        queue,
		Workers:            1,
		Wait: func(context.Context, time.Duration) error {
			cancel()
			return context.Canceled
		},
	}
	if err := service.Run(runCtx); err != nil {
		t.Fatalf("projector run: %v", err)
	}
}

// shardLiveReconcile writes the reducer-owned reconciliation facts the reducer
// derives from a generation's alerts, keyed like the reducer keys them: scope
// and generation of the provider alert fact.
func shardLiveReconcile(t *testing.T, ctx context.Context, database *sql.DB, g shardLiveGeneration) {
	t.Helper()
	decisions := make([]securityalert.SecurityAlertReconciliationDecision, 0, len(g.alerts))
	for _, alert := range g.alerts {
		repositoryID := "security-alert:github:" + alert.repository
		decisions = append(decisions, securityalert.SecurityAlertReconciliationDecision{
			ProviderAlertFactID:       fmt.Sprintf("fact-%s-%d", alert.repository, alert.number),
			ProviderAlertScopeID:      g.scopeID,
			ProviderAlertGenerationID: g.generationID,
			Provider:                  shardLiveProvider,
			ProviderAlertID:           fmt.Sprintf("%s#%d", repositoryID, alert.number),
			ProviderAlertNumber:       int64(alert.number),
			ProviderState:             "open",
			RepositoryID:              repositoryID,
			ProviderRepositoryID:      repositoryID,
			PackageID:                 "npm://left-pad",
			Status:                    securityalert.SecurityAlertReconciliationUnmatched,
		})
	}
	writer := securityalert.PostgresSecurityAlertReconciliationWriter{DB: postgres.SQLDB{DB: database}}
	if _, err := writer.WriteSecurityAlertReconciliations(ctx, securityalert.SecurityAlertReconciliationWrite{
		IntentID: "intent-" + g.generationID, ScopeID: g.scopeID, GenerationID: g.generationID,
		SourceSystem: string(scope.CollectorSecurityAlert), Cause: "live-proof", Decisions: decisions,
	}); err != nil {
		t.Fatalf("write reconciliations: %v", err)
	}
}

// shardLiveReads returns what the list, count, and inventory stores answer for
// the provider-anchored read the routes run, paging both list and inventory
// to the end.
func shardLiveReads(t *testing.T, ctx context.Context, database *sql.DB) (listed []string, counted int, inventory map[string]int) {
	t.Helper()
	store := alerts.NewPostgresStore(database)
	after := ""
	for {
		rows, err := store.ListSecurityAlertReconciliations(ctx, supplychain.SecurityAlertReconciliationFilter{
			Provider: shardLiveProvider, Limit: 200, AfterReconciliationID: after,
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, row := range rows {
			listed = append(listed, row.ProviderAlert.RepositoryID)
			after = row.ReconciliationID
		}
		if len(rows) < 200 {
			break
		}
	}
	aggregate := alerts.NewPostgresAggregateStore(database)
	filter := supplychain.SecurityAlertReconciliationAggregateFilter{Provider: shardLiveProvider}
	count, err := aggregate.CountSecurityAlertReconciliations(ctx, filter)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	inventory = map[string]int{}
	for offset := 0; ; offset += 200 {
		buckets, err := aggregate.SecurityAlertReconciliationInventory(ctx, filter, supplychain.SecurityAlertReconciliationInventoryByRepository, 200, offset)
		if err != nil {
			t.Fatalf("inventory: %v", err)
		}
		for _, bucket := range buckets {
			inventory[bucket.Value] = bucket.Count
		}
		if len(buckets) < 200 {
			break
		}
	}
	return listed, count.TotalReconciliations, inventory
}

// shardLiveActive returns the scope's active generation id ("" for none).
func shardLiveActive(t *testing.T, ctx context.Context, database *sql.DB, scopeID string) string {
	t.Helper()
	var active sql.NullString
	if err := database.QueryRowContext(ctx, `SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1`, scopeID).Scan(&active); err != nil {
		t.Fatalf("read active generation of %s: %v", scopeID, err)
	}
	return active.String
}
