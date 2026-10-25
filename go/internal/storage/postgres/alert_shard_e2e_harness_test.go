// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts/alertruntime"
	"github.com/eshu-hq/eshu/go/internal/coordinator/security/alert"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/security/alerts/retirement"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/security/alerts/unread"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// shardE2EOrgClient is the fake provider of the #9019 end-to-end proofs: the
// org endpoint returns, untruncated, one open alert per repository of the
// organization, plus one for a repository nobody allowlists.
type shardE2EOrgClient struct {
	repositories []string
}

func (c shardE2EOrgClient) ListOrganizationAlertsPages(context.Context, string, int) (securityalerts.GitHubDependabotAlertResult, error) {
	alerts := make([]securityalerts.GitHubDependabotAlert, 0, len(c.repositories)+1)
	for i, repository := range append(slices.Clone(c.repositories), "acme/not-allowlisted") {
		_, name, _ := strings.Cut(repository, "/")
		alerts = append(alerts, securityalerts.GitHubDependabotAlert{
			Number: i + 1, State: "open",
			Dependency: securityalerts.GitHubDependabotDependency{
				Package: securityalerts.GitHubDependabotPackage{Ecosystem: "npm", Name: "left-pad"}, ManifestPath: "package.json",
			},
			Repository: securityalerts.GitHubDependabotRepository{FullName: repository, Name: name},
		})
	}
	return securityalerts.GitHubDependabotAlertResult{Alerts: alerts, PagesFetched: 1}, nil
}

func (shardE2EOrgClient) ListRepositoryAlertsPages(context.Context, string, int) (securityalerts.GitHubDependabotAlertResult, error) {
	return securityalerts.GitHubDependabotAlertResult{}, fmt.Errorf("an untruncated org read never fans out")
}

// shardE2E drives one org target through the production planner, collector,
// claim-fenced commit, projector, and reducer reconciliation writer.
type shardE2E struct {
	t        *testing.T
	ctx      context.Context
	database *sql.DB
	store    postgres.IngestionStore
	planner  alert.WorkPlanner
	// organization holds every repository the provider knows, so a
	// repository removed from the allowlist still has alerts upstream.
	organization []string
	claimSeq     int
}

func newShardE2E(t *testing.T, database *sql.DB, ctx context.Context) *shardE2E {
	t.Helper()
	organization := make([]string, 0, 120)
	for i := range 120 {
		organization = append(organization, fmt.Sprintf("acme/svc-%03d", i))
	}
	return &shardE2E{
		t: t, ctx: ctx, database: database, store: postgres.NewIngestionStore(postgres.SQLDB{DB: database}),
		planner:      alert.WorkPlanner{ShardScopes: retirement.NewReader(postgres.SQLDB{DB: database})},
		organization: organization,
	}
}

func shardE2EConfig(allowed []string) string {
	raw, _ := json.Marshal(map[string]any{"targets": []any{map[string]any{
		"provider": "github_dependabot", "scope": "org", "scope_id": shardLiveBaseScopeID, "organization": "acme",
		"token_env": "GITHUB_TOKEN", "allowed_repositories": allowed, "max_pages": 1,
	}}})
	return string(raw)
}

// plan runs the production planner for allowed under planKey.
func (e *shardE2E) plan(allowed []string, planKey string) []workflow.WorkItem {
	e.t.Helper()
	observedAt := time.Now().UTC()
	_, items, err := e.planner.PlanSecurityAlertWork(e.ctx, alert.PlanRequest{
		Instance: workflow.CollectorInstance{
			InstanceID: "inst-live", CollectorKind: scope.CollectorSecurityAlert, Mode: workflow.CollectorModeContinuous,
			Enabled: true, ClaimsEnabled: true, Configuration: shardE2EConfig(allowed),
			LastObservedAt: observedAt, CreatedAt: observedAt, UpdatedAt: observedAt,
		},
		ObservedAt: observedAt,
		PlanKey:    planKey,
	})
	if err != nil {
		e.t.Fatalf("plan: %v", err)
	}
	return items
}

// source builds one collector replica for allowed.
func (e *shardE2E) source(allowed []string) *alertruntime.ClaimedSource {
	e.t.Helper()
	source, err := alertruntime.NewClaimedSource(alertruntime.SourceConfig{
		CollectorInstanceID: "inst-live",
		Targets: []alertruntime.TargetConfig{{
			Provider: alertruntime.ProviderGitHubDependabot, Scope: alertruntime.TargetScopeOrganization,
			ScopeID: shardLiveBaseScopeID, Organization: "acme", Token: "token", MaxPages: 1, AllowedRepositories: allowed,
		}},
		ClientFactory: func(alertruntime.TargetConfig) (alertruntime.RepositoryAlertClient, error) {
			return shardE2EOrgClient{repositories: e.organization}, nil
		},
		ReadStateWriter: func(state securityalerts.TargetReadState) collector.GenerationStateWriter {
			return unread.Writer(state)
		},
		FanoutCursorReader: unread.NewCursorReader(postgres.SQLDB{DB: e.database}),
	})
	if err != nil {
		e.t.Fatalf("NewClaimedSource: %v", err)
	}
	return source
}

// claim seeds the claimed work-item row a real claim leaves, so the commit's
// claim fence holds. Each call uses its own work item id.
func (e *shardE2E) claim(item workflow.WorkItem) workflow.ClaimMutation {
	e.t.Helper()
	e.claimSeq++
	id := fmt.Sprintf("e2e-%d", e.claimSeq)
	if _, err := e.database.ExecContext(e.ctx, `
INSERT INTO workflow_work_items (
    work_item_id, run_id, collector_kind, collector_instance_id, source_system,
    scope_id, acceptance_unit_id, source_run_id, generation_id, status, attempt_count,
    current_claim_id, current_owner_id, current_fencing_token, lease_expires_at, created_at, updated_at
) VALUES ($1, 'run-live', 'security_alert', 'inst-live', 'security_alert', $2, $3, $4, $4,
          'claimed', 1, $5, 'owner-live', 1, now() + interval '1 hour', now(), now())`,
		id, item.ScopeID, item.AcceptanceUnitID, item.GenerationID, "claim-"+id); err != nil {
		e.t.Fatalf("seed work item: %v", err)
	}
	if _, err := e.database.ExecContext(e.ctx, `
INSERT INTO workflow_claims (claim_id, work_item_id, fencing_token, owner_id, status, claimed_at, heartbeat_at,
    lease_expires_at, created_at, updated_at)
VALUES ($1, $2, 1, 'owner-live', 'active', now(), now(), now() + interval '1 hour', now(), now())`, "claim-"+id, id); err != nil {
		e.t.Fatalf("seed claim: %v", err)
	}
	return workflow.ClaimMutation{
		WorkItemID: id, ClaimID: "claim-" + id, FencingToken: 1, OwnerID: "owner-live",
		LeaseDuration: time.Hour, ObservedAt: time.Now().UTC(),
	}
}

// collect answers item with source and commits it through the claim-fenced
// path, exactly as the claimed service does after NextClaimed.
func (e *shardE2E) collect(source *alertruntime.ClaimedSource, item workflow.WorkItem, mutation workflow.ClaimMutation) error {
	item.CollectorInstanceID = "inst-live"
	collected, ok, err := source.NextClaimed(e.ctx, item)
	if err != nil || !ok {
		return fmt.Errorf("collect %s: ok=%v err=%w", item.ScopeID, ok, err)
	}
	return e.store.CommitClaimedScopeGenerationWithState(e.ctx, mutation, collected.Scope, collected.Generation,
		collected.Facts, nil, collected.StateWriter)
}

// scan plans allowed under planKey, collects every item with replicas
// collector replicas in parallel (item i goes to replica i mod replicas),
// projects, and writes the reconciliation facts of each newly active
// collection generation.
func (e *shardE2E) scan(allowed []string, planKey string, replicas int) []workflow.WorkItem {
	e.t.Helper()
	items := e.plan(allowed, planKey)
	mutations := make([]workflow.ClaimMutation, len(items))
	for i, item := range items {
		mutations[i] = e.claim(item)
	}
	sources := make([]*alertruntime.ClaimedSource, replicas)
	for r := range sources {
		sources[r] = e.source(allowed)
	}
	errs := make([]error, len(items))
	var wg sync.WaitGroup
	for r := range replicas {
		wg.Go(func() {
			for i := r; i < len(items); i += replicas {
				errs[i] = e.collect(sources[r], items[i], mutations[i])
			}
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			e.t.Fatal(err)
		}
	}
	shardLiveProject(e.t, e.ctx, e.database)
	for _, item := range items {
		if !shard.IsRetirement(item.ScopeID, item.AcceptanceUnitID) {
			e.reconcileActive(item.ScopeID, item.GenerationID)
		}
	}
	return items
}

// reconcileActive writes the reducer reconciliation facts of a committed
// collection generation from its stored alert facts, keyed as the reducer
// keys them (scope and generation of the provider alert fact).
func (e *shardE2E) reconcileActive(scopeID, generationID string) {
	e.t.Helper()
	rows, err := e.database.QueryContext(e.ctx, `
SELECT payload->>'repository_id', (payload->>'provider_alert_number')::int
FROM fact_records WHERE scope_id = $1 AND generation_id = $2 AND fact_kind = 'security_alert.repository_alert'`, scopeID, generationID)
	if err != nil {
		e.t.Fatalf("read alert facts: %v", err)
	}
	g := shardLiveGeneration{scopeID: scopeID, generationID: generationID}
	for rows.Next() {
		var repositoryID string
		var number int
		if err := rows.Scan(&repositoryID, &number); err != nil {
			e.t.Fatalf("scan alert fact: %v", err)
		}
		g.alerts = append(g.alerts, shardLiveAlert{repository: strings.TrimPrefix(repositoryID, "security-alert:github:"), number: number})
	}
	_ = rows.Close()
	if len(g.alerts) > 0 {
		shardLiveReconcile(e.t, e.ctx, e.database, g)
	}
}

// assertReads checks that list, count, and inventory return exactly one row
// per repository of allowed, and that only planned scopes contribute rows.
func (e *shardE2E) assertReads(step string, allowed []string) {
	e.t.Helper()
	want := make([]string, 0, len(allowed))
	for _, repository := range allowed {
		want = append(want, "security-alert:github:"+repository)
	}
	slices.Sort(want)
	listed, counted, inventory := shardLiveReads(e.t, e.ctx, e.database)
	slices.Sort(listed)
	if !slices.Equal(listed, want) {
		e.t.Fatalf("%s: list returned %d repositories, want exactly the %d allowlisted", step, len(listed), len(want))
	}
	if counted != len(want) || len(inventory) != len(want) {
		e.t.Fatalf("%s: count = %d, inventory buckets = %d, want %d", step, counted, len(inventory), len(want))
	}
	planned := map[string]bool{}
	for _, leaf := range shard.Partition(shardLiveBaseScopeID, "acme", allowed) {
		planned[leaf.ScopeID] = true
	}
	rows, err := e.database.QueryContext(e.ctx, `
SELECT fact.scope_id, count(*)
FROM fact_records AS fact
JOIN ingestion_scopes AS s ON s.scope_id = fact.scope_id AND s.active_generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_security_alert_reconciliation'
GROUP BY fact.scope_id`)
	if err != nil {
		e.t.Fatalf("%s: per-scope rows: %v", step, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var scopeID string
		var n int
		if err := rows.Scan(&scopeID, &n); err != nil {
			e.t.Fatalf("%s: scan: %v", step, err)
		}
		if !planned[scopeID] {
			e.t.Fatalf("%s: unplanned scope %s still contributes %d rows", step, scopeID, n)
		}
	}
}
