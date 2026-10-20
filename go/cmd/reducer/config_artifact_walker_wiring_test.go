// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	walkartifacts "github.com/eshu-hq/eshu/go/internal/reducer/maintenance/config/artifacts"
	storageartifacts "github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// refusingWalkerDB fails every query: the start-up encoding and fence checks
// refuse, so the wired deriver answers every derive without a statement.
type refusingWalkerDB struct {
	queries int
	execs   int
}

func (f *refusingWalkerDB) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	f.queries++
	return nil, errors.New("connection refused")
}

func (f *refusingWalkerDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	f.execs++
	return fakeReducerResult{}, nil
}

func (f *refusingWalkerDB) Begin(context.Context) (db.Transaction, error) {
	return nil, errors.New("refusingWalkerDB opens no transaction")
}

// queryOnlyWalkerDB answers queries but its method set has no Begin: the
// walker cannot be built over it because the derive opens its own write
// transaction.
type queryOnlyWalkerDB struct {
	queries int
}

func (f *queryOnlyWalkerDB) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	f.queries++
	return nil, errors.New("connection refused")
}

func (f *queryOnlyWalkerDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return fakeReducerResult{}, nil
}

// TestConfigArtifactWalkerRunnerForDefaults pins the default wiring: enabled,
// 4 workers, the leaf page budget, skip limit and poll interval, over a pool
// whose start-up checks refuse (so the deriver is built but refuses).
func TestConfigArtifactWalkerRunnerForDefaults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	runner, err := configArtifactWalkerRunnerFor(ctx,
		func(string) string { return "" }, &refusingWalkerDB{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("configArtifactWalkerRunnerFor(defaults) error = %v", err)
	}
	if runner == nil {
		t.Fatal("configArtifactWalkerRunnerFor(defaults) = nil, want the walker on by default")
	}
	if runner.Config.Workers != 4 {
		t.Fatalf("Workers = %d, want 4", runner.Config.Workers)
	}
	if got := runner.Config.WithDefaults(); got.PageBudget != 500 || got.SkipAfterFailures != 5 {
		t.Fatalf("walk config = %+v, want the leaf page budget 500 and skip limit 5", got)
	}
	if got := runner.Config.WithDefaults().PollInterval; got.String() != "5m0s" {
		t.Fatalf("PollInterval = %v, want 5m", got)
	}
}

// TestConfigArtifactWalkerRunnerForDisabled pins the kill switch: disabled
// builds no walker and issues no statement.
func TestConfigArtifactWalkerRunnerForDisabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	database := &refusingWalkerDB{}
	runner, err := configArtifactWalkerRunnerFor(ctx,
		func(key string) string {
			if key == content.ConfigArtifactWalkerEnabledEnv {
				return "false"
			}
			return ""
		}, database, nil, nil, nil)
	if err != nil {
		t.Fatalf("configArtifactWalkerRunnerFor(disabled) error = %v", err)
	}
	if runner != nil {
		t.Fatal("configArtifactWalkerRunnerFor(disabled) = non-nil, want nil")
	}
	if database.queries+database.execs != 0 {
		t.Fatalf("disabled wiring issued %d statements, want none", database.queries+database.execs)
	}
}

// TestConfigArtifactWalkerRunnerForInvalidConfig pins fail-fast startup: a
// bad worker count or a bad timeout is an error, never a walker with a
// setting it did not ask for. A pool without transactions leaves the walker
// off without an error, like the sibling reducer components over a limited
// database.
func TestConfigArtifactWalkerRunnerForInvalidConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, env := range []map[string]string{
		{content.ConfigArtifactWalkerWorkersEnv: "0"},
		{content.ConfigArtifactDeriveTimeoutEnv: "forever"},
		{
			content.ConfigArtifactWalkerEnabledEnv: "false",
			content.ConfigArtifactDeriveTimeoutEnv: "forever",
		},
		{
			content.ConfigArtifactWalkerEnabledEnv: "false",
			content.ConfigArtifactWalkerWorkersEnv: "0",
		},
	} {
		if _, err := configArtifactWalkerRunnerFor(ctx,
			func(key string) string { return env[key] },
			&refusingWalkerDB{}, nil, nil, nil); err == nil {
			t.Fatalf("configArtifactWalkerRunnerFor(%v) = nil, want an error", env)
		}
	}
	runner, err := configArtifactWalkerRunnerFor(ctx,
		func(string) string { return "" }, &queryOnlyWalkerDB{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("configArtifactWalkerRunnerFor(pool without transactions) error = %v, want nil", err)
	}
	if runner != nil {
		t.Fatal("configArtifactWalkerRunnerFor(pool without transactions) = non-nil, want the walker off")
	}
}

// TestConfigArtifactWalkerStoreRefusesWithoutAStatement pins the refused
// deriver behind the adapter: with failed start-up checks the walker's Derive
// answers fence_missing or encoding_unsupported, counted by the runner, and
// never touches the database.
func TestConfigArtifactWalkerStoreRefusesWithoutAStatement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	database := &refusingWalkerDB{}
	runner, err := configArtifactWalkerRunnerFor(ctx,
		func(string) string { return "" }, database, nil, nil, nil)
	if err != nil {
		t.Fatalf("configArtifactWalkerRunnerFor() error = %v", err)
	}
	queriesAfterBuild := database.queries
	outcome, err := runner.Deriver.Derive(ctx, "repository:r_000", walkartifacts.TriggerRepair)
	if err != nil {
		t.Fatalf("Derive() error = %v, want the refusal as an outcome", err)
	}
	if outcome != "fence_missing" && outcome != "encoding_unsupported" {
		t.Fatalf("Derive() = %q, want a read-model-off refusal", outcome)
	}
	if database.queries != queriesAfterBuild {
		t.Fatalf("refused derive issued a statement, want none")
	}
}

// TestConfigArtifactWalkerTriggerAndReasonParity pins the leaf's duplicated
// strings to the storage constants: the leaf cannot import storage, so the
// adapter forwards these exact values into DeriveRepository and the stale
// gauge. A renamed storage value must move the leaf copy in the same PR.
func TestConfigArtifactWalkerTriggerAndReasonParity(t *testing.T) {
	t.Parallel()

	if string(storageartifacts.TriggerRepair) != walkartifacts.TriggerRepair {
		t.Fatalf("repair trigger: storage %q != leaf %q",
			string(storageartifacts.TriggerRepair), walkartifacts.TriggerRepair)
	}
	if string(storageartifacts.TriggerBackfill) != walkartifacts.TriggerBackfill {
		t.Fatalf("backfill trigger: storage %q != leaf %q",
			string(storageartifacts.TriggerBackfill), walkartifacts.TriggerBackfill)
	}
	storageReasons := []string{
		string(storageartifacts.ReasonMissing),
		string(storageartifacts.ReasonFailed),
		string(storageartifacts.ReasonStaleVersion),
		string(storageartifacts.ReasonStaleExtractor),
		string(storageartifacts.ReasonRetry),
	}
	if len(walkartifacts.StaleReasons) != len(storageReasons) {
		t.Fatalf("leaf stale reasons = %v, storage reasons = %v", walkartifacts.StaleReasons, storageReasons)
	}
	for i, reason := range storageReasons {
		if walkartifacts.StaleReasons[i] != reason {
			t.Fatalf("leaf stale reasons = %v, storage reasons = %v", walkartifacts.StaleReasons, storageReasons)
		}
	}
	if contentartifacts.ExtractorVersion < 1 {
		t.Fatalf("ExtractorVersion = %d, want the guard-pinned positive version", contentartifacts.ExtractorVersion)
	}
}

// TestConfigArtifactWalkerOutcomeParity pins the derive outcomes the leaf
// reconcile mapping branches on to the storage Outcome constants: ok,
// superseded, and the two read-model-off refusals. Any other outcome maps
// to reconcile error by default, so a new outcome degrades to error loudly
// rather than matching a wrong branch silently. A renamed storage value must
// move the leaf copy in the same PR.
func TestConfigArtifactWalkerOutcomeParity(t *testing.T) {
	t.Parallel()

	if string(storageartifacts.OutcomeOK) != walkartifacts.DeriveOutcomeOK {
		t.Fatalf("ok: storage %q != leaf %q",
			string(storageartifacts.OutcomeOK), walkartifacts.DeriveOutcomeOK)
	}
	if string(storageartifacts.OutcomeSuperseded) != walkartifacts.DeriveOutcomeSuperseded {
		t.Fatalf("superseded: storage %q != leaf %q",
			string(storageartifacts.OutcomeSuperseded), walkartifacts.DeriveOutcomeSuperseded)
	}
	if string(storageartifacts.OutcomeEncodingUnsupported) != walkartifacts.DeriveOutcomeEncodingUnsupported {
		t.Fatalf("encoding_unsupported: storage %q != leaf %q",
			string(storageartifacts.OutcomeEncodingUnsupported), walkartifacts.DeriveOutcomeEncodingUnsupported)
	}
	if string(storageartifacts.OutcomeFenceMissing) != walkartifacts.DeriveOutcomeFenceMissing {
		t.Fatalf("fence_missing: storage %q != leaf %q",
			string(storageartifacts.OutcomeFenceMissing), walkartifacts.DeriveOutcomeFenceMissing)
	}
}

// TestConfigArtifactWalkerStoreSkippedReposReportsQueryFailure pins the
// adapter's error path: a refused skip check fails the cycle instead of
// deriving blind.
func TestConfigArtifactWalkerStoreSkippedReposReportsQueryFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	database := &refusingWalkerDB{}
	runner, err := configArtifactWalkerRunnerFor(ctx,
		func(string) string { return "" }, database, nil, nil, nil)
	if err != nil {
		t.Fatalf("configArtifactWalkerRunnerFor() error = %v", err)
	}
	if _, err := runner.Lister.(configArtifactWalkerStore).SkippedRepos(ctx, []string{"repo-a"}); err == nil {
		t.Fatal("SkippedRepos(refused checks) = nil, want the query error")
	}
}
