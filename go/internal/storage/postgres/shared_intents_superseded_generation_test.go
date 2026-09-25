// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The shared intent store opts into the superseded-generation drain (#7121).
var _ worker.SupersededGenerationReader = (*SharedIntentStore)(nil)

// TestSupersededGenerationIDsSQLShape locks the bounded lookup: one statement
// over the primary key, terminal status only. It must key on the terminal
// 'superseded' status and never on active_generation_id, which would race with
// activation and drop a pending generation's live intents.
func TestSupersededGenerationIDsSQLShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"FROM scope_generations",
		"generation_id = ANY($1",
		"status = 'superseded'",
	} {
		if !strings.Contains(supersededGenerationIDsSQL, want) {
			t.Fatalf("supersededGenerationIDsSQL missing %q:\n%s", want, supersededGenerationIDsSQL)
		}
	}
	if strings.Contains(supersededGenerationIDsSQL, "active_generation_id") {
		t.Fatalf("lookup must not key on active_generation_id:\n%s", supersededGenerationIDsSQL)
	}
}

type supersededLookupDB struct {
	queries int
	args    []any
	ids     []string
	err     error
}

func (d *supersededLookupDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected exec")
}

func (d *supersededLookupDB) QueryContext(_ context.Context, _ string, args ...any) (db.Rows, error) {
	d.queries++
	d.args = append([]any(nil), args...)
	if d.err != nil {
		return nil, d.err
	}
	return &supersededLookupRows{ids: d.ids}, nil
}

type supersededLookupRows struct {
	ids []string
	pos int
}

func (r *supersededLookupRows) Next() bool {
	if r.pos >= len(r.ids) {
		return false
	}
	r.pos++
	return true
}

func (r *supersededLookupRows) Scan(dest ...any) error {
	*dest[0].(*string) = r.ids[r.pos-1]
	return nil
}
func (r *supersededLookupRows) Err() error   { return nil }
func (r *supersededLookupRows) Close() error { return nil }

func TestSupersededGenerationIDsOneRoundTripAndResultSet(t *testing.T) {
	t.Parallel()

	database := &supersededLookupDB{ids: []string{"gen-old"}}
	store := NewSharedIntentStore(database)

	got, err := store.SupersededGenerationIDs(context.Background(), []string{"gen-old", "gen-new"})
	if err != nil {
		t.Fatalf("SupersededGenerationIDs() error = %v", err)
	}
	if database.queries != 1 {
		t.Fatalf("queries = %d, want exactly 1 round trip", database.queries)
	}
	if _, ok := got["gen-old"]; !ok || len(got) != 1 {
		t.Fatalf("result = %v, want only gen-old", got)
	}
	if arg, _ := database.args[0].([]string); !slices.Equal(arg, []string{"gen-old", "gen-new"}) {
		t.Fatalf("args = %v, want the id slice", database.args)
	}
}

func TestSupersededGenerationIDsEmptyInputSkipsQuery(t *testing.T) {
	t.Parallel()

	database := &supersededLookupDB{}
	got, err := NewSharedIntentStore(database).SupersededGenerationIDs(context.Background(), nil)
	if err != nil || len(got) != 0 || database.queries != 0 {
		t.Fatalf("got=%v err=%v queries=%d, want empty/nil/0", got, err, database.queries)
	}
}

func TestSupersededGenerationIDsPropagatesQueryError(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection reset")
	_, err := NewSharedIntentStore(&supersededLookupDB{err: boom}).
		SupersededGenerationIDs(context.Background(), []string{"gen-a"})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want wrapped %v", err, boom)
	}
}

// TestSupersededGenerationIDsAgainstPostgres proves the predicate on real data:
// a superseded generation is returned; active, pending, failed, and an id that
// is not a scope generation (a relationship-generation id) are not. It
// bootstraps its own schema, so a bare disposable Postgres is enough. Set
// ESHU_SUPERSEDED_GENERATION_PROOF_DSN to run it; skipped otherwise.
func TestSupersededGenerationIDsAgainstPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	conn := openSupersededProofSchema(t, ctx, "eshu_7121_lookup")

	scopeID := "superseded-proof:" + time.Now().UTC().Format("20060102150405.000000000")
	now := time.Now().UTC()
	if _, err := conn.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
		VALUES ($1,'git','git-collector',$1,'git',$1,$2,$2,'active','{}'::jsonb)`, scopeID, now); err != nil {
		t.Fatalf("insert scope: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `DELETE FROM ingestion_scopes WHERE scope_id=$1`, scopeID)
	})
	statuses := map[string]string{
		scopeID + ":superseded": "superseded",
		scopeID + ":active":     "active",
		scopeID + ":pending":    "pending",
		scopeID + ":failed":     "failed",
	}
	for id, status := range statuses {
		if _, err := conn.ExecContext(ctx, `INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
			VALUES ($1,$2,'snapshot',$3,$3,$4,'{}'::jsonb)`, id, scopeID, now.Add(time.Duration(len(id))*time.Nanosecond), status); err != nil {
			t.Fatalf("insert generation %s: %v", id, err)
		}
	}

	ids := []string{
		scopeID + ":superseded", scopeID + ":active", scopeID + ":pending",
		scopeID + ":failed", "relationship-generation-not-a-scope-generation",
	}
	got, err := NewSharedIntentStore(SQLDB{DB: conn}).SupersededGenerationIDs(ctx, ids)
	if err != nil {
		t.Fatalf("SupersededGenerationIDs() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("result = %v, want exactly the superseded generation", got)
	}
	if _, ok := got[scopeID+":superseded"]; !ok {
		t.Fatalf("result = %v, missing the superseded generation", got)
	}
}
