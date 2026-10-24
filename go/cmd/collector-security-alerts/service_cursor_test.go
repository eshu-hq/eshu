// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts/alertruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// recordingOrgClient answers the org read as truncated and records the
// per-repository reads in order. A repository in failing answers 503, a stop
// that repeats on every claim.
type recordingOrgClient struct {
	mu      sync.Mutex
	calls   []string
	failing map[string]bool
}

func (c *recordingOrgClient) ListRepositoryAlertsPages(
	_ context.Context, repository string, _ int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, repository)
	if c.failing[repository] {
		return securityalerts.GitHubDependabotAlertResult{}, securityalerts.GitHubDependabotError{StatusCode: 503}
	}
	return securityalerts.GitHubDependabotAlertResult{PagesFetched: 1}, nil
}

func (*recordingOrgClient) ListOrganizationAlertsPages(
	context.Context, string, int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	return securityalerts.GitHubDependabotAlertResult{PagesFetched: 1, Truncated: true}, nil
}

// cursorDB is a stateful stand-in for the Postgres rows the cursor lives in:
// the read-state upsert the StateWriter runs stores fanout_next_repository, and
// the cursor read answers what was stored last. It is what a restart sees: the
// database outlives the source.
type cursorDB struct {
	mu     sync.Mutex
	stored sql.NullString
	lap    sql.NullString
	have   bool
	writes int
}

func (d *cursorDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if strings.Contains(query, "INSERT INTO security_alert_target_read_states") {
		d.writes++
		d.have = true
		d.stored, d.lap = sql.NullString{}, sql.NullString{}
		if next, ok := args[8].(string); ok {
			d.stored = sql.NullString{String: next, Valid: true}
		}
		if lap, ok := args[9].(string); ok {
			d.lap = sql.NullString{String: lap, Valid: true}
		}
	}
	return fake.Result{}, nil
}

func (d *cursorDB) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !strings.Contains(query, "FROM scope_generations") || !strings.Contains(query, "fanout_next_repository") {
		return nil, errors.New("unexpected query in the cursor database")
	}
	if !d.have {
		return &fake.Rows{}, nil
	}
	return &fake.Rows{Data: [][]any{{d.stored, d.lap}}}, nil
}

func cursorAllowlistJSON(count int) string {
	names := make([]string, count)
	for i := range names {
		names[i] = fmt.Sprintf(`"example-org/repo-%03d"`, i)
	}
	return strings.Join(names, ",")
}

// claimBuiltService builds the production service over database, runs one org
// claim, and commits its read state through the StateWriter the claim handed
// the committer, the way CommitClaimedScopeGenerationWithState does.
func claimBuiltService(t *testing.T, database db.ExecQueryer, generation string) []string {
	t.Helper()
	client := &recordingOrgClient{failing: map[string]bool{"example-org/repo-005": true}}
	service, err := buildClaimedServiceWithClients(database, orgStateGetenvAllowing(cursorAllowlistJSON(10)), nil, nil, nil,
		func(alertruntime.TargetConfig) (alertruntime.RepositoryAlertClient, error) { return client, nil })
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	now := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	collected, ok, err := service.Source.NextClaimed(context.Background(), workflow.WorkItem{
		WorkItemID:          "work-" + generation,
		CollectorKind:       scope.CollectorSecurityAlert,
		CollectorInstanceID: "security-alert-primary",
		ScopeID:             "security-alert:github-org:example-org",
		GenerationID:        generation,
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = ok %v, err %v (cause %v), want a collected org generation", ok, err, errors.Unwrap(err))
	}
	if collected.StateWriter == nil {
		t.Fatal("org claim has no StateWriter: the cursor cannot commit with its generation")
	}
	if err := collected.StateWriter(context.Background(), database); err != nil {
		t.Fatalf("StateWriter() error = %v", err)
	}
	return client.calls
}

// The test drives the StateWriter the built source returns over a stateful fake
// of the read-state row. The real committer path, the claim-fenced transaction
// that runs that writer, is proven on PostgreSQL by
// TestSecurityAlertFanoutCursorLive/CommitsAndReadsTheCursorOfTheNewestGeneration;
// this test proves the construction wiring.
//
// TestBuildClaimedServiceResumesOrgFanoutAcrossGenerationsAndRestarts pins the
// #8946 wiring on the production construction path with two generations: the
// first claim starts at the top, stops at a failing repository, and commits its
// cursor through the writer the built source hands the committer; a fresh
// service built over the same database (a restart) reads that cursor through the
// injected reader and starts after the repository that failed. Delete the reader injection and construction fails;
// delete the cursor columns from the writer and the second claim starts at the
// top.
func TestBuildClaimedServiceResumesOrgFanoutAcrossGenerationsAndRestarts(t *testing.T) {
	t.Parallel()

	database := &cursorDB{}
	// repo-005 answers 503 on every claim, so the first claim stops there.
	first := claimBuiltService(t, database, "generation-1")
	if want := cursorRepositories(0, 5); !slices.Equal(first, want) {
		t.Fatalf("first claim read %v, want %v (from the top, stopped at repo-005)", first, want)
	}
	if got := database.stored.String; got != "example-org/repo-006" || database.writes != 1 {
		t.Fatalf("stored cursor = %q after %d writes, want example-org/repo-006 after 1", got, database.writes)
	}
	second := claimBuiltService(t, database, "generation-2") // a fresh service: a restart
	if want := append(cursorRepositories(6, 9), cursorRepositories(0, 5)...); !slices.Equal(second, want) {
		t.Fatalf("second claim read %v, want %v (resumed at repo-006, wrapped, retried repo-005 last)", second, want)
	}
	if got := database.stored.String; got != "example-org/repo-006" {
		t.Fatalf("stored cursor after the second generation = %q, want the settled example-org/repo-006", got)
	}
}

// cursorRepositories lists repo-<from> through repo-<to>, inclusive.
func cursorRepositories(from, to int) []string {
	names := make([]string, 0, to-from+1)
	for i := from; i <= to; i++ {
		names = append(names, fmt.Sprintf("example-org/repo-%03d", i))
	}
	return names
}
