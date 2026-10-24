// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/security/alerts/unread"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Run the live proofs against a throwaway Postgres whose role may CREATE
// DATABASE:
//
//	ESHU_POSTGRES_DSN='postgres://eshu:change-me@localhost:15432/eshu?sslmode=disable' \
//	  go test -race ./internal/storage/postgres -run SecurityAlertReadStateLive -count=1 -v
//
// Each proof gets a database of its own: a retention pass walks every
// superseded generation and must not touch another proof's rows.

// readStateClaimMutation names the workflow claim mutation the live proofs hold.
type readStateClaimMutation = workflow.ClaimMutation

const readStateLiveScopeID = "security-alert:github:org:acme"

// readStateIsolatedLiveDB creates a database from the bootstrap schema.
func readStateIsolatedLiveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	raw := os.Getenv("ESHU_POSTGRES_DSN")
	if raw == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the security-alert read state live proofs")
	}
	dsn, err := url.Parse(raw)
	if err != nil || dsn.Scheme == "" {
		t.Skip("the read state live proofs need a URL-form ESHU_POSTGRES_DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	base, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatalf("open base db: %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })
	name := fmt.Sprintf("alert_read_state_%d", time.Now().UnixNano())
	if _, err := base.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Skipf("cannot create an isolated database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = base.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	dsn.Path = "/" + name
	isolated, err := sql.Open("pgx", dsn.String())
	if err != nil {
		t.Fatalf("open isolated db: %v", err)
	}
	isolated.SetMaxOpenConns(24)
	t.Cleanup(func() { _ = isolated.Close() })
	if err := postgres.ApplyBootstrapWithoutContentSearchIndexes(ctx, postgres.SQLDB{DB: isolated}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	if err := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: isolated}).EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure workflow control schema: %v", err)
	}
	if _, err := isolated.ExecContext(ctx, `
INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at)
VALUES ('run-live', 'schedule', 'running', now(), now())`); err != nil {
		t.Fatalf("seed workflow run: %v", err)
	}
	return isolated, ctx
}

// readStateClaimFor seeds one active claim for generationID so the claim-fenced commit
// passes its HeartbeatClaim check, and returns the mutation that holds it.
func readStateClaimFor(t *testing.T, ctx context.Context, database *sql.DB, generationID string) workflow.ClaimMutation {
	t.Helper()
	item, claim := "item-"+generationID, "claim-"+generationID
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_work_items (
    work_item_id, run_id, collector_kind, collector_instance_id, source_system,
    scope_id, acceptance_unit_id, source_run_id, generation_id, status, attempt_count,
    current_claim_id, current_owner_id, current_fencing_token, lease_expires_at, created_at, updated_at
) VALUES ($1, 'run-live', 'security_alert', 'inst-live', 'security_alert', $3, $3, $2, $2,
          'claimed', 1, $4, 'owner-live', 1, now() + interval '1 hour', now(), now())`,
		item, generationID, readStateLiveScopeID, claim,
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

// readStateCommitGeneration commits one generation through the production claim-fenced
// path with its read state, exactly as the claimed service does.
func readStateCommitGeneration(
	ctx context.Context,
	store postgres.IngestionStore,
	mutation workflow.ClaimMutation,
	generationID string,
	hint string,
	state securityalerts.TargetReadState,
) error {
	now := time.Now().UTC()
	scopeValue := scope.IngestionScope{
		ScopeID:       readStateLiveScopeID,
		SourceSystem:  string(scope.CollectorSecurityAlert),
		ScopeKind:     scope.KindSecurityAlert,
		CollectorKind: scope.CollectorSecurityAlert,
		PartitionKey:  "github_dependabot",
	}
	generation := scope.ScopeGeneration{
		GenerationID: generationID, ScopeID: readStateLiveScopeID, ObservedAt: now, IngestedAt: now,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot, FreshnessHint: hint,
	}
	state.GenerationID, state.ScopeID, state.ObservedAt = generationID, readStateLiveScopeID, now
	stream := make(chan facts.Envelope, 1)
	stream <- facts.Envelope{
		FactID: "fact-" + generationID, ScopeID: readStateLiveScopeID, GenerationID: generationID,
		FactKind: "security_alert.repository_alert", StableFactKey: "alert:" + generationID,
		ObservedAt: now, Payload: map[string]any{"number": float64(1)},
	}
	close(stream)
	return store.CommitClaimedScopeGenerationWithState(ctx, mutation, scopeValue, generation, stream, nil, unread.Writer(state))
}

func readStateUnreadState(total int, unread ...securityalerts.UnreadRepository) securityalerts.TargetReadState {
	return securityalerts.TargetReadState{OrgReadTruncated: true, RepositoriesTotal: total, Unread: unread}
}

func readStateCount(t *testing.T, ctx context.Context, database *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := database.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}
