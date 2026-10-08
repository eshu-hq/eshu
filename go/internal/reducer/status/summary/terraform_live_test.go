// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	statussummary "github.com/eshu-hq/eshu/go/internal/reducer/status/summary"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	statestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/terraform/state"
)

// newLiveWriterWithTerraform is newLiveWriter plus the Terraform-state
// companion row, wired like the reducer does (cmd/reducer
// statusSummaryWriterFor): the storage package's statements and digests.
func newLiveWriterWithTerraform(database *sql.DB) *statussummary.Runner {
	writer := newLiveWriter(database)
	writer.Companions = []statussummary.Statement{{
		ModelKey:     store.ModelTerraformState,
		SourceSHA256: statestore.SummarySourceSHA256(),
		Compute: func(ctx context.Context, queryer db.Queryer, _ time.Time) ([]store.Entry, error) {
			return statestore.SummaryEntries(ctx, queryer)
		},
	}}
	return writer
}

// terraformFixture seeds Terraform-state scopes and warnings. Every scope has
// three generations (serials 1-3, the last active) so the last-serial
// statement ranks them, and warnings are spread over all of them so the
// recent-warning statement's per-locator rank cap and its generation-blind
// history both matter.
type terraformFixture struct {
	stateScopes      int // state_snapshot scopes
	warningsPerScope int // warnings per state generation (3 generations)
	gitRepos         int // repository scopes with unresolved_backend_expression warnings
	gitWarnings      int // per repo generation, spread over 3 source paths
	malformedScopes  int // state scopes whose serial overflows int64: the SQL returns them and the Go decoder skips them
}

func seedTerraformState(ctx context.Context, t *testing.T, database *sql.DB, f terraformFixture) {
	t.Helper()
	mustExec(ctx, t, database, `DELETE FROM fact_records WHERE fact_kind = 'terraform_state_warning'`)
	mustExec(ctx, t, database, `DELETE FROM scope_generations WHERE scope_id LIKE 'state_snapshot:%' OR scope_id LIKE 'tfrepo-%'`)
	mustExec(ctx, t, database, `DELETE FROM ingestion_scopes WHERE scope_id LIKE 'state_snapshot:%' OR scope_id LIKE 'tfrepo-%'`)
	if f.stateScopes+f.malformedScopes > 0 {
		mustExec(ctx, t, database, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
  observed_at, ingested_at, status, payload)
SELECT 'state_snapshot:s3:tf-' || i, 'state_snapshot', 'terraform_state', 'tf-' || i, 'terraform_state', 'tf-' || i,
       now() - interval '5 hours', now() - interval '5 hours', 'active',
       jsonb_build_object('locator_hash', 'loc-' || lpad(i::text, 4, '0'), 'backend_kind', 's3')
FROM generate_series(1, $1) AS i`, f.stateScopes+f.malformedScopes)
		mustExec(ctx, t, database, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
SELECT CASE WHEN i <= $1 THEN 'terraform_state:state_snapshot:s3:tf-' || i || ':lineage-' || i || ':serial:' || g
            -- A serial that matches the statement's digits pattern but overflows
            -- int64, so the SQL returns it and the Go decoder's malformed-serial
            -- skip is the thing that drops the locator.
            ELSE 'terraform_state:state_snapshot:s3:tf-' || i || ':lineage-' || i || ':serial:9999999999999999999' || g END,
       'state_snapshot:s3:tf-' || i, 'snapshot', now() - make_interval(hours => 4 - g), now() - make_interval(hours => 4 - g),
       CASE WHEN g = 3 THEN 'active' ELSE 'superseded' END, '{}'::jsonb
FROM generate_series(1, $2) AS i CROSS JOIN generate_series(1, 3) AS g`, f.stateScopes, f.stateScopes+f.malformedScopes)
		mustExec(ctx, t, database, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
  source_fact_key, observed_at, ingested_at, payload)
SELECT 'tfw-' || gen.generation_id || '-' || n, gen.scope_id, gen.generation_id, 'terraform_state_warning',
       'wk' || n, 'terraform_state', 'wfk' || n, gen.observed_at + make_interval(secs => n), gen.ingested_at,
       jsonb_build_object('warning_kind', (ARRAY['state_missing', 'sensitive_skip', 'output_value_dropped'])[1 + n % 3],
         'reason', 'reason-' || (n % 4), 'severity', 'warning', 'actionability', 'operator',
         'source', 'terraform_state', 'source_handle', 'handle-' || n)
FROM scope_generations gen CROSS JOIN generate_series(1, $1) AS n
WHERE gen.scope_id LIKE 'state_snapshot:%'`, f.warningsPerScope)
	}
	if f.gitRepos > 0 {
		mustExec(ctx, t, database, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
  observed_at, ingested_at, status, payload)
SELECT 'tfrepo-' || i, 'repository', 'git', 'tfrepo-' || i, 'git', 'tfrepo-' || i,
       now() - interval '5 hours', now() - interval '5 hours', 'active', jsonb_build_object('repo_id', 'repo-' || i)
FROM generate_series(1, $1) AS i`, f.gitRepos)
		mustExec(ctx, t, database, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
SELECT 'gen:tfrepo-' || i || ':' || g, 'tfrepo-' || i, 'snapshot', now() - make_interval(hours => 4 - g),
       now() - make_interval(hours => 4 - g), CASE WHEN g = 3 THEN 'active' ELSE 'superseded' END, '{}'::jsonb
FROM generate_series(1, $1) AS i CROSS JOIN generate_series(1, 3) AS g`, f.gitRepos)
		mustExec(ctx, t, database, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
  source_fact_key, observed_at, ingested_at, payload)
SELECT 'tfg-' || gen.generation_id || '-' || n, gen.scope_id, gen.generation_id, 'terraform_state_warning',
       'gk' || n, 'git', 'gfk' || n, gen.observed_at + make_interval(secs => n), gen.ingested_at,
       jsonb_build_object('warning_kind', 'unresolved_backend_expression', 'reason', 'dynamic_backend',
         'severity', 'warning', 'actionability', 'author', 'source', 'git', 'repo_id', 'repo-' || substring(gen.scope_id from 8),
         'source_path', 'infra/backend' || (n % 3) || '.tf')
FROM scope_generations gen CROSS JOIN generate_series(1, $1) AS n
WHERE gen.scope_id LIKE 'tfrepo-%'`, f.gitWarnings)
	}
}

// terraformSnapshotPair reads the Terraform-state sections twice inside one
// REPEATABLE READ READ ONLY snapshot: once through the stored-summary reader
// and once live.
func terraformSnapshotPair(ctx context.Context, t *testing.T, database *sql.DB) (model, live statuspkg.RawSnapshot) {
	t.Helper()
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatalf("begin snapshot: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	queryer := txQueryer{tx}
	selection := statuspkg.SnapshotSelection{}
	on := postgres.NewStatusStore(queryer).WithSummaryReader(postgres.NewStatusSummaryReaderWithConfig(store.ReadConfig{Enabled: true, StaleAfter: time.Hour}))
	if model, err = on.ReadStatusSnapshotFiltered(ctx, time.Now(), selection); err != nil {
		t.Fatalf("read through the stored summary: %v", err)
	}
	off := postgres.NewStatusStore(queryer).WithSummaryReader(postgres.NewStatusSummaryReaderWithConfig(store.ReadConfig{}))
	if live, err = off.ReadStatusSnapshotFiltered(ctx, time.Now(), selection); err != nil {
		t.Fatalf("read live: %v", err)
	}
	return model, live
}

func assertTerraformEqual(t *testing.T, model, live statuspkg.RawSnapshot) {
	t.Helper()
	if got := model.TerraformStateSource; got.Source != statuspkg.ActiveWorkSourceModel || got.Reason != statuspkg.ActiveWorkReasonFresh {
		t.Fatalf("terraform source = %+v, want model/fresh (the row must be served for the comparison to mean anything)", got)
	}
	if got := live.TerraformStateSource; got.Source != statuspkg.ActiveWorkSourceLive {
		t.Fatalf("the comparison snapshot was not live: %+v", got)
	}
	if !reflect.DeepEqual(model.TerraformStateLastSerials, live.TerraformStateLastSerials) {
		t.Fatalf("stored last serials differ from the live read:\nstored: %+v\nlive:   %+v", model.TerraformStateLastSerials, live.TerraformStateLastSerials)
	}
	if !reflect.DeepEqual(model.TerraformStateRecentWarnings, live.TerraformStateRecentWarnings) {
		t.Fatalf("stored recent warnings differ from the live read (%d stored, %d live)", len(model.TerraformStateRecentWarnings), len(live.TerraformStateRecentWarnings))
	}
}

// TestTerraformModelServedEqualToLiveLive is the accuracy proof for the
// terraform_state model: a row written by the production writer pass (the same
// pass that writes the active-work row) and read back by the production reader
// in a snapshot equals what the two live statements return, at an empty
// database, a small one, one past the per-locator rank cap, and one with Git
// backend warnings and a state scope whose generation ids carry no serial.
// The last case shows the comparison can differ.
func TestTerraformModelServedEqualToLiveLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	writer := newLiveWriterWithTerraform(database)
	for _, state := range []struct {
		name        string
		fixture     terraformFixture
		wantSerials int
		wantMinWarn int
	}{
		{"empty", terraformFixture{}, 0, 0},
		{"serials only", terraformFixture{stateScopes: 3}, 3, 0},
		{"a few states", terraformFixture{stateScopes: 3, warningsPerScope: 4}, 3, 12},
		{"past the per-locator rank cap", terraformFixture{stateScopes: 2, warningsPerScope: 60}, 2, 100},
		{"states, git backends and malformed generations", terraformFixture{stateScopes: 4, warningsPerScope: 5, gitRepos: 6, gitWarnings: 4, malformedScopes: 2}, 4, 30},
	} {
		t.Run(state.name, func(t *testing.T) {
			seedTerraformState(ctx, t, database, state.fixture)
			pass := writer.RunOnce(ctx)
			if pass.Outcome != statussummary.OutcomeOK || len(pass.Models) != 2 {
				t.Fatalf("RunOnce() = %+v, want ok with two models", pass)
			}
			time.Sleep(20 * time.Millisecond)
			model, live := terraformSnapshotPair(ctx, t, database)
			assertTerraformEqual(t, model, live)
			if len(live.TerraformStateLastSerials) != state.wantSerials || len(live.TerraformStateRecentWarnings) < state.wantMinWarn {
				t.Fatalf("fixture produced %d serials and %d warnings, want %d and at least %d",
					len(live.TerraformStateLastSerials), len(live.TerraformStateRecentWarnings), state.wantSerials, state.wantMinWarn)
			}
			if state.fixture.malformedScopes > 0 {
				// The overflowing serials reach the Go decoder: the SQL
				// statement returns a row for every state scope, so the
				// skip of the malformed ones is the decoder's.
				var sqlRows int
				if err := database.QueryRowContext(ctx, `SELECT count(DISTINCT scope_id) FROM scope_generations WHERE generation_id LIKE 'terraform_state:%:serial:%'`).Scan(&sqlRows); err != nil {
					t.Fatal(err)
				}
				if want := state.fixture.stateScopes + state.fixture.malformedScopes; sqlRows != want || len(live.TerraformStateLastSerials) != state.fixture.stateScopes {
					t.Fatalf("SQL-visible state scopes = %d (want %d), decoded serials = %d (want %d): the malformed serials must pass the SQL and be dropped by the Go decoder",
						sqlRows, want, len(live.TerraformStateLastSerials), state.fixture.stateScopes)
				}
			}
			if state.name == "past the per-locator rank cap" {
				perLocator := map[string]int{}
				for _, w := range live.TerraformStateRecentWarnings {
					perLocator[w.SafeLocatorHash]++
				}
				for locator, n := range perLocator {
					if n > statuspkg.MaxTerraformStateRecentWarnings {
						t.Fatalf("locator %s has %d warnings, want at most the rank cap %d", locator, n, statuspkg.MaxTerraformStateRecentWarnings)
					}
				}
				if len(perLocator) != 2 || len(live.TerraformStateRecentWarnings) != 2*statuspkg.MaxTerraformStateRecentWarnings {
					t.Fatalf("rank-cap fixture = %v, want 2 locators at exactly %d rows each", perLocator, statuspkg.MaxTerraformStateRecentWarnings)
				}
			}
			// Each row reads its own database clock in its own transaction,
			// the first model first, and has its own digest.
			var activeAsOf, terraformAsOf time.Time
			var activeSHA, terraformSHA string
			if err := database.QueryRowContext(ctx, `SELECT as_of, source_sha256 FROM status_summary_snapshots WHERE model_key = $1`, store.ModelActiveWorkSummary).Scan(&activeAsOf, &activeSHA); err != nil {
				t.Fatal(err)
			}
			if err := database.QueryRowContext(ctx, `SELECT as_of, source_sha256 FROM status_summary_snapshots WHERE model_key = $1`, store.ModelTerraformState).Scan(&terraformAsOf, &terraformSHA); err != nil {
				t.Fatal(err)
			}
			if terraformAsOf.Before(activeAsOf) || terraformAsOf.Sub(activeAsOf) > 5*time.Second ||
				activeSHA == terraformSHA || terraformSHA != statestore.SummarySourceSHA256() {
				t.Fatalf("rows: as_of %v / %v, digests %q / %q; want the companion's own later as_of and each model's own digest", activeAsOf, terraformAsOf, activeSHA, terraformSHA)
			}
		})
	}
	t.Run("the comparison can differ", func(t *testing.T) {
		seedTerraformState(ctx, t, database, terraformFixture{stateScopes: 2, warningsPerScope: 3})
		if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
			t.Fatalf("RunOnce() = %+v", pass)
		}
		mustExec(ctx, t, database, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'late-warning', gen.scope_id, gen.generation_id, 'terraform_state_warning', 'late', 'terraform_state', 'late', now(), now(),
       jsonb_build_object('warning_kind', 'state_missing', 'reason', 'late', 'severity', 'warning', 'actionability', 'operator', 'source', 'terraform_state', 'source_handle', 'late')
FROM scope_generations gen WHERE gen.status = 'active' AND gen.scope_id LIKE 'state_snapshot:%' LIMIT 1`)
		model, live := terraformSnapshotPair(ctx, t, database)
		if reflect.DeepEqual(model.TerraformStateRecentWarnings, live.TerraformStateRecentWarnings) {
			t.Fatal("the equality check could not see a warning written after the pass")
		}
	})
}

// modelAsOf returns one model row's stored as_of.
func modelAsOf(ctx context.Context, t *testing.T, database *sql.DB, modelKey string) time.Time {
	t.Helper()
	var asOf time.Time
	if err := database.QueryRowContext(ctx,
		`SELECT as_of FROM status_summary_snapshots WHERE model_key = $1`, modelKey).Scan(&asOf); err != nil {
		t.Fatalf("read %s as_of: %v", modelKey, err)
	}
	return asOf
}

// TestWriterKilledMidCompanionKeepsThePrimaryRowLive: each model row is its own
// transaction, so a companion whose backend dies between its statements and its
// upsert leaves the terraform_state row at its old as_of, while the
// active-work row of the same pass is already committed with a new one.
func TestWriterKilledMidCompanionKeepsThePrimaryRowLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	seedWork(ctx, t, database, 300, 120)
	seedTerraformState(ctx, t, database, terraformFixture{stateScopes: 3, warningsPerScope: 4})
	writer := newLiveWriterWithTerraform(database)
	if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK || len(pass.Models) != 2 || pass.Models[1].Outcome != statussummary.OutcomeOK {
		t.Fatalf("first pass = %+v, want ok for both models", pass)
	}
	primaryBefore, tfBefore := modelAsOf(ctx, t, database, store.ModelActiveWorkSummary), modelAsOf(ctx, t, database, store.ModelTerraformState)

	killed := newLiveWriterWithTerraform(database)
	killed.Companions[0].Compute = func(ctx context.Context, q db.Queryer, asOf time.Time) ([]store.Entry, error) {
		entries, err := statestore.SummaryEntries(ctx, q)
		if err != nil {
			return nil, err
		}
		rows, err := q.QueryContext(ctx, `SELECT pg_backend_pid()`)
		if err != nil {
			return nil, err
		}
		var pid int
		for rows.Next() {
			if err := rows.Scan(&pid); err != nil {
				return nil, err
			}
		}
		_ = rows.Close()
		mustExec(context.Background(), t, database, `SELECT pg_terminate_backend($1)`, pid)
		return entries, nil
	}
	pass := killed.RunOnce(ctx)
	if pass.Outcome != statussummary.OutcomeOK || pass.Models[1].Outcome != statussummary.OutcomeError {
		t.Fatalf("killed pass = %+v, want the first model ok and the companion an error", pass)
	}
	if got := modelAsOf(ctx, t, database, store.ModelActiveWorkSummary); !got.After(primaryBefore) {
		t.Fatalf("active-work as_of = %s, want it advanced past %s by the killed pass's committed first model", got, primaryBefore)
	}
	if got := modelAsOf(ctx, t, database, store.ModelTerraformState); !got.Equal(tfBefore) {
		t.Fatalf("terraform_state as_of = %s, want the old %s", got, tfBefore)
	}
	// The old terraform row is still whole and still serves equal to live.
	model, live := terraformSnapshotPair(ctx, t, database)
	assertTerraformEqual(t, model, live)
}
