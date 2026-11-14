// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/security/alert"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/query"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/alerts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/security/alerts/retirement"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/security/alerts/unread"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// shardE2EAlert is one open alert of the fake provider endpoint: the org
// endpoint returns, untruncated, one open alert per repository of the
// organization, plus one for a repository nobody allowlists.
type shardE2EAlert struct {
	fullName string
	name     string
	number   int
}

// shardE2EOrgClient is the fake provider of the #9019 end-to-end proofs.
type shardE2EOrgClient struct {
	repositories []string
}

func (c shardE2EOrgClient) listOrganizationAlerts() []shardE2EAlert {
	alerts := make([]shardE2EAlert, 0, len(c.repositories)+1)
	for i, repository := range append(slices.Clone(c.repositories), "acme/not-allowlisted") {
		_, name, _ := strings.Cut(repository, "/")
		alerts = append(alerts, shardE2EAlert{fullName: repository, name: name, number: i + 1})
	}
	return alerts
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

// shardE2ESource replicates the retired in-tree collect orchestration for
// the untruncated single-read org path: the fake provider never truncates,
// so no fan-out runs and the cursor stays at the top. The satellite owns the
// real collect loop since #10620; this harness proves sharding through the
// production planner, claim-fenced commit, projector, and reads, so it builds
// the same scope, generation, envelopes, and read state the source built.
type shardE2ESource struct {
	allowed []string
}

// source builds one test source replica for allowed, normalizing the
// allowlist exactly as the retired target validation did.
func (e *shardE2E) source(allowed []string) *shardE2ESource {
	e.t.Helper()
	seen := map[string]struct{}{}
	cleaned := make([]string, 0, len(allowed))
	for _, repository := range allowed {
		normalized := shard.NormalizeRepository(repository)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		cleaned = append(cleaned, normalized)
	}
	return &shardE2ESource{allowed: cleaned}
}

// shardE2ETarget resolves item's scope to its shard target: the base scope
// when the partition has one leaf, otherwise the leaf the item names, with
// the base as parent and the leaf's repositories as its allowlist. It mirrors
// the retired target expansion, which the planner matches by construction.
func (e *shardE2E) target(item workflow.WorkItem, allowed []string) (scopeID, parentScopeID string, repositories []string, err error) {
	leaves := shard.Partition(shardLiveBaseScopeID, "acme", allowed)
	if len(leaves) == 1 {
		if item.ScopeID != shardLiveBaseScopeID {
			return "", "", nil, fmt.Errorf("collect %s: scope is not the configured org target", item.ScopeID)
		}
		return shardLiveBaseScopeID, "", leaves[0].Repositories, nil
	}
	for _, leaf := range leaves {
		if leaf.ScopeID == item.ScopeID {
			return leaf.ScopeID, shardLiveBaseScopeID, leaf.Repositories, nil
		}
	}
	return "", "", nil, fmt.Errorf("collect %s: scope is not a shard of the configured org target", item.ScopeID)
}

// shardE2EFreshnessHint is the test-local freshness digest for a complete
// single-read collection. It keeps the retired digest's contract —
// deterministic, non-empty, and sensitive to the alert set and the target
// set — without reimplementing the satellite's snapshot grammar; hint values
// never leave the test database.
func shardE2EFreshnessHint(alerts []shardLiveAlert, targetSetHash string) string {
	type snapshot struct {
		Repository string `json:"repository"`
		Number     int    `json:"number"`
	}
	snapshots := make([]snapshot, 0, len(alerts))
	for _, alert := range alerts {
		snapshots = append(snapshots, snapshot{Repository: alert.repository, Number: alert.number})
	}
	slices.SortFunc(snapshots, func(a, b snapshot) int {
		if c := strings.Compare(a.Repository, b.Repository); c != 0 {
			return c
		}
		return a.Number - b.Number
	})
	raw, err := json.Marshal(map[string]any{
		"alerts":         snapshots,
		"pages_fetched":  1,
		"truncated":      false,
		"org_read_state": 1,
		"target_set":     targetSetHash,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
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

// collect answers item and commits it through the claim-fenced path, exactly
// as the claimed service did after NextClaimed: retirements commit an empty
// trusted-deletion generation, collections commit one envelope per
// allowlisted alert with a full-coverage read state.
func (e *shardE2E) collect(source *shardE2ESource, item workflow.WorkItem, mutation workflow.ClaimMutation) error {
	item.CollectorInstanceID = "inst-live"
	now := time.Now().UTC()
	scopeID := strings.TrimSpace(item.ScopeID)
	if shard.IsRetirement(scopeID, item.AcceptanceUnitID) {
		parent := ""
		if parsed, _, ok := shard.ParseScopeID(scopeID); ok {
			parent = parsed
		}
		stream := make(chan facts.Envelope)
		close(stream)
		state := unread.TargetReadState{GenerationID: item.GenerationID, ScopeID: scopeID, ObservedAt: now}
		return e.store.CommitClaimedScopeGenerationWithState(e.ctx, mutation,
			shardE2EScope(scopeID, parent), shardE2ERetirementGeneration(item, scopeID, now),
			stream, nil, unread.Writer(state))
	}
	return e.collectAlerts(source, item, mutation, now)
}

// collectAlerts commits one envelope per allowlisted alert of item's shard
// with a full-coverage read state, mirroring the retired untruncated
// single-read collect: the fake provider never truncates, so no fan-out runs
// and the cursor stays at the top.
func (e *shardE2E) collectAlerts(source *shardE2ESource, item workflow.WorkItem, mutation workflow.ClaimMutation, now time.Time) error {
	scopeID, parentScopeID, repositories, err := e.target(item, source.allowed)
	if err != nil {
		return err
	}
	allowed := make(map[string]struct{}, len(repositories))
	for _, repository := range repositories {
		allowed[repository] = struct{}{}
	}
	var alerts []shardLiveAlert
	for _, providerAlert := range (shardE2EOrgClient{repositories: e.organization}).listOrganizationAlerts() {
		fullName := strings.ToLower(strings.Trim(strings.TrimSpace(providerAlert.fullName), "/"))
		if _, ok := allowed[fullName]; !ok {
			continue
		}
		alerts = append(alerts, shardLiveAlert{repository: fullName, number: providerAlert.number})
	}
	orgRepositories := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		if strings.HasPrefix(repository, "acme/") {
			orgRepositories = append(orgRepositories, repository)
		}
	}
	targetSet := scope.NewSecurityAlertTargetSet(orgRepositories)
	stream := make(chan facts.Envelope, len(alerts))
	for _, alert := range alerts {
		stream <- shardLiveAlertEnvelope(scopeID, item.GenerationID, alert, now)
	}
	close(stream)
	state := unread.TargetReadState{
		GenerationID: item.GenerationID, ScopeID: scopeID,
		RepositoriesTotal: len(orgRepositories), ObservedAt: now,
	}
	generation := scope.ScopeGeneration{
		GenerationID: item.GenerationID, ScopeID: scopeID, ObservedAt: now, IngestedAt: now,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		FreshnessHint:          shardE2EFreshnessHint(alerts, targetSet.Hash()),
		SecurityAlertTargetSet: targetSet,
	}
	return e.store.CommitClaimedScopeGenerationWithState(e.ctx, mutation,
		shardE2EScope(scopeID, parentScopeID), generation, stream, nil, unread.Writer(state))
}

// shardE2EScope is the ingestion scope a shard claim commits, identical for
// collection and retirement generations.
func shardE2EScope(scopeID, parentScopeID string) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID: scopeID, ParentScopeID: parentScopeID, SourceSystem: string(scope.CollectorSecurityAlert),
		ScopeKind: scope.KindSecurityAlert, CollectorKind: scope.CollectorSecurityAlert,
		PartitionKey: shardLiveProvider, Metadata: map[string]string{"provider": shardLiveProvider},
	}
}

// shardE2ERetirementGeneration is the empty trusted-deletion retirement
// generation: it replaces the scope's last generation past the successor
// guard and records an explicit empty target set.
func shardE2ERetirementGeneration(item workflow.WorkItem, scopeID string, now time.Time) scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: item.GenerationID, ScopeID: scopeID, ObservedAt: now, IngestedAt: now,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		FreshnessHint:          shard.RetirementFreshnessHint,
		TrustedDeletion:        true,
		SecurityAlertTargetSet: scope.NewSecurityAlertTargetSet(nil),
	}
}

// scan plans allowed under planKey, then runs the plan in two phases with
// replicas collector replicas in parallel (item i goes to replica i mod
// replicas): first every retirement item, projected, then every collection
// item, projected, and the reconciliation facts of each newly active
// collection generation. Retiring first is the worst order for read truth: a
// merged parent's old retirement stays active until its collection lands.
// betweenPhases, when set, runs after the retirements are active.
func (e *shardE2E) scan(allowed []string, planKey string, replicas int, betweenPhases ...func()) []workflow.WorkItem {
	e.t.Helper()
	items := e.plan(allowed, planKey)
	var retirements, collections []workflow.WorkItem
	for _, item := range items {
		if shard.IsRetirement(item.ScopeID, item.AcceptanceUnitID) {
			retirements = append(retirements, item)
		} else {
			collections = append(collections, item)
		}
	}
	sources := make([]*shardE2ESource, replicas)
	for r := range sources {
		sources[r] = e.source(allowed)
	}
	e.runPhase(sources, retirements)
	for _, hook := range betweenPhases {
		hook()
	}
	e.runPhase(sources, collections)
	for _, item := range collections {
		e.reconcileActive(item.ScopeID, item.GenerationID)
	}
	return items
}

// runPhase claims and commits items across the replicas, then projects.
func (e *shardE2E) runPhase(sources []*shardE2ESource, items []workflow.WorkItem) {
	e.t.Helper()
	mutations := make([]workflow.ClaimMutation, len(items))
	for i, item := range items {
		mutations[i] = e.claim(item)
	}
	errs := make([]error, len(items))
	var wg sync.WaitGroup
	for r := range sources {
		wg.Go(func() {
			for i := r; i < len(items); i += len(sources) {
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
}

// grantedReads returns what list and count answer for a scoped token whose
// grant set is granted, on the provider-anchored read.
func (e *shardE2E) grantedReads(granted ...string) (int, int) {
	e.t.Helper()
	rows, err := alerts.NewPostgresStore(e.database).ListSecurityAlertReconciliations(e.ctx, supplychain.SecurityAlertReconciliationFilter{
		Provider: shardLiveProvider, Limit: 200, AllowedSourceRepositoryIDs: granted,
	})
	if err != nil {
		e.t.Fatalf("granted list: %v", err)
	}
	count, err := alerts.NewPostgresAggregateStore(e.database).CountSecurityAlertReconciliations(e.ctx,
		supplychain.SecurityAlertReconciliationAggregateFilter{Provider: shardLiveProvider, AllowedSourceRepositoryIDs: granted})
	if err != nil {
		e.t.Fatalf("granted count: %v", err)
	}
	return len(rows), count.TotalReconciliations
}

// collectionState reads one scope's empty-answer collection state through the
// production query store, anchored on repository.
func (e *shardE2E) collectionState(scopeID, repository string) supplychain.SecurityAlertTargetState {
	e.t.Helper()
	states, err := query.NewPostgresSecurityAlertCollectionStateStoreWithReadStore(postgres.SQLDB{DB: e.database}).
		SecurityAlertTargetStates(e.ctx, []string{scopeID}, []string{repository})
	if err != nil || len(states) != 1 {
		e.t.Fatalf("collection state of %s: %v (%d rows)", scopeID, err, len(states))
	}
	return states[0]
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
