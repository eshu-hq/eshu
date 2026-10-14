// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Live differential for the repository-catalog key projection (#8082).
//
// backfill.LoadRepositoryCatalog used to stream every repository fact payload
// (4.4 GB on the QA corpus) and decode each into a map to read six keys. It now
// projects those keys in SQL. This proof seeds the shapes where a projection
// can drift from a whole-payload decode and requires the shipped loader to
// return exactly what the old statement plus the same Go decode returned, and
// to ship only the projected object. It runs on the disposable PostgreSQL
// named by ESHU_TARGETED_MAINTENANCE_PROOF_DSN with
// ESHU_TARGETED_MAINTENANCE_PROOF_DISPOSABLE=1.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// repositoryCatalogOracleQuery is the statement LoadRepositoryCatalog shipped
// before #8082. It stays here as the oracle the projected statement is
// compared with; production code must never use it.
const repositoryCatalogOracleQuery = `
SELECT payload, observed_at
FROM fact_records
WHERE fact_kind = 'repository'
ORDER BY observed_at DESC, fact_id DESC
`

// repositoryCatalogByteCountingQueryer wraps a Queryer and tracks the bytes
// every scanned []byte destination received, so the proof can show the loader
// no longer ships whole payloads.
type repositoryCatalogByteCountingQueryer struct {
	inner      db.Queryer
	queries    []string
	maxPayload int
	totalBytes int
}

func (q *repositoryCatalogByteCountingQueryer) QueryContext(
	ctx context.Context, query string, args ...any,
) (db.Rows, error) {
	q.queries = append(q.queries, query)
	rows, err := q.inner.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &repositoryCatalogByteCountingRows{Rows: rows, owner: q}, nil
}

type repositoryCatalogByteCountingRows struct {
	db.Rows
	owner *repositoryCatalogByteCountingQueryer
}

func (r *repositoryCatalogByteCountingRows) Scan(dest ...any) error {
	if err := r.Rows.Scan(dest...); err != nil {
		return err
	}
	for _, d := range dest {
		raw, ok := d.(*[]byte)
		if !ok {
			continue
		}
		r.owner.totalBytes += len(*raw)
		if len(*raw) > r.owner.maxPayload {
			r.owner.maxPayload = len(*raw)
		}
	}
	return nil
}

// repositoryCatalogOracle runs the pre-#8082 statement and the pre-#8082 Go
// decode (whole payload into a map, then RepositoryCatalogEntryFromMap).
func repositoryCatalogOracle(
	ctx context.Context, t *testing.T, database *sql.DB,
) ([]relationships.CatalogEntry, map[string]time.Time) {
	t.Helper()
	rows, err := database.QueryContext(ctx, repositoryCatalogOracleQuery)
	if err != nil {
		t.Fatalf("oracle query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	seen := map[string]struct{}{}
	catalog := make([]relationships.CatalogEntry, 0)
	observedAt := map[string]time.Time{}
	for rows.Next() {
		var raw []byte
		var at time.Time
		if err := rows.Scan(&raw, &at); err != nil {
			t.Fatalf("oracle scan: %v", err)
		}
		if len(raw) == 0 {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		entry, ok := backfill.RepositoryCatalogEntryFromMap(payload)
		if !ok {
			continue
		}
		if _, dup := seen[entry.RepoID]; dup {
			continue
		}
		seen[entry.RepoID] = struct{}{}
		catalog = append(catalog, entry)
		observedAt[entry.RepoID] = at
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("oracle rows: %v", err)
	}
	return catalog, observedAt
}

type repositoryCatalogSeedFact struct {
	factID  string
	repoKey string
	at      time.Time
	payload string
}

// seedRepositoryCatalogFacts inserts each fact under its own generation so
// one repository can carry several generations.
func seedRepositoryCatalogFacts(
	ctx context.Context, t *testing.T, database *sql.DB, facts []repositoryCatalogSeedFact,
) {
	t.Helper()
	for _, fact := range facts {
		scopeID := "git:" + fact.repoKey
		generationID := "gen-" + fact.factID
		if _, err := database.ExecContext(ctx,
			`INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', NULL)
ON CONFLICT DO NOTHING`,
			scopeID, fact.at); err != nil {
			t.Fatalf("seed scope %q: %v", scopeID, err)
		}
		if _, err := database.ExecContext(ctx,
			`INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'poll', $3, $3, 'pending')`,
			generationID, scopeID, fact.at); err != nil {
			t.Fatalf("seed generation %q: %v", generationID, err)
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)`,
			fact.factID, scopeID, generationID, fact.at, fact.payload); err != nil {
			t.Fatalf("seed repository fact %q: %v", fact.factID, err)
		}
	}
}

// TestLoadRepositoryCatalogProjectionMatchesWholePayloadDecode proves the
// SQL key projection returns the same entries and freshness keys as the old
// whole-payload decode, over the payload shapes a projection could drift on,
// and that the loader ships only the projected object.
func TestLoadRepositoryCatalogProjectionMatchesWholePayloadDecode(t *testing.T) {
	dsn := targetedMaintenanceProofDSN(t)
	database := openIsolatedBootstrapSchema(t, dsn, "catalog8082")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	base := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	bigBlob := strings.Repeat("x", 1<<20)
	facts := []repositoryCatalogSeedFact{
		// Two generations of one repository: the newer one must win, and
		// only the newer one's observed_at may reach the freshness map.
		{"f-big-old", "big", base, fmt.Sprintf(
			`{"repo_id":"repo-big","name":"big-old","blob":%q,"remote_url":"https://github.com/Org/Big.git"}`, bigBlob)},
		{"f-big-new", "big", base.Add(time.Hour), fmt.Sprintf(
			`{"repo_id":"repo-big","name":"big","repo_slug":"org/big","blob":%q,"remote_url":"https://github.com/Org/Big.git"}`, bigBlob)},
		// Same repository, identical observed_at: fact_id DESC decides.
		{"f-tie-a", "tie", base, `{"repo_id":"repo-tie","name":"tie-a"}`},
		{"f-tie-b", "tie", base, `{"repo_id":"repo-tie","name":"tie-b"}`},
		// A numeric repo_id is not a string: the entry falls back to name.
		{"f-numeric", "numeric", base, `{"repo_id":42,"name":"numeric-id"}`},
		// name absent: repo_id and graph_id still identify the repository.
		{"f-noname", "noname", base, `{"repo_id":"repo-noname","graph_id":"graph-noname"}`},
		{"f-graph-only", "graphonly", base, `{"graph_id":"graph-only","repo_name":"graph-only-name"}`},
		// JSON null and a whitespace-only value read like an absent key.
		{"f-null-slug", "nullslug", base, `{"repo_id":"repo-nullslug","name":"null-slug","repo_slug":null}`},
		{"f-ws-url", "wsurl", base, `{"repo_id":"repo-ws","name":"ws","remote_url":"   "}`},
		// A non-string name is skipped; repo_name still aliases.
		{"f-obj-name", "objname", base, `{"repo_id":"repo-objname","name":{"x":1},"repo_name":"fallback-name"}`},
		{"f-unicode", "unicode", base, `{"repo_id":"repo-uni","name":"café \"q\"","repo_slug":"org/café"}`},
		// Nothing identifying: skipped by both.
		{"f-empty", "empty", base, `{}`},
		// Non-object payloads: skipped by the old decode into a map.
		{"f-array", "array", base, `[1,2,3]`},
		{"f-string", "string", base, `"repo-from-string"`},
		{"f-null", "jsonnull", base, `null`},
	}
	seedRepositoryCatalogFacts(ctx, t, database, facts)

	wantCatalog, wantObservedAt := repositoryCatalogOracle(ctx, t, database)
	if len(wantCatalog) < 9 {
		t.Fatalf("oracle returned %d entries, want at least 9 so the proof is not vacuous", len(wantCatalog))
	}

	counting := &repositoryCatalogByteCountingQueryer{inner: SQLDB{DB: database}}
	gotCatalog, gotObservedAt, err := backfill.LoadRepositoryCatalog(ctx, counting)
	if err != nil {
		t.Fatalf("LoadRepositoryCatalog() error = %v", err)
	}

	if !reflect.DeepEqual(gotCatalog, wantCatalog) {
		t.Fatalf("catalog differs from whole-payload oracle:\n got  %#v\n want %#v", gotCatalog, wantCatalog)
	}
	if len(gotObservedAt) != len(wantObservedAt) {
		t.Fatalf("observedAt size = %d, want %d", len(gotObservedAt), len(wantObservedAt))
	}
	for repoID, want := range wantObservedAt {
		got, ok := gotObservedAt[repoID]
		if !ok || !got.Equal(want) {
			t.Fatalf("observedAt[%q] = %v (present=%t), want %v", repoID, got, ok, want)
		}
	}
	if !gotObservedAt["repo-big"].Equal(base.Add(time.Hour)) {
		t.Fatalf("repo-big freshness key = %v, want the newer generation %v",
			gotObservedAt["repo-big"], base.Add(time.Hour))
	}
	for _, skipped := range []string{"repo-from-string", "42"} {
		if _, ok := gotObservedAt[skipped]; ok {
			t.Fatalf("entry %q must not exist: its payload carries no usable identity", skipped)
		}
	}

	// The loader must ship the projection, not the 2 MB of seeded blobs. Six
	// short keys stay far below 4 KiB per row.
	const maxProjectedRowBytes = 4 << 10
	if counting.maxPayload > maxProjectedRowBytes {
		t.Fatalf("largest shipped payload = %d bytes, want <= %d: the loader still ships whole payloads",
			counting.maxPayload, maxProjectedRowBytes)
	}
	if len(counting.queries) != 1 {
		t.Fatalf("loader ran %d statements, want 1", len(counting.queries))
	}
	for _, key := range relationships.RepositoryCatalogPayloadKeys() {
		for _, fragment := range []string{"'" + key + `', x."` + key + `"`, `"` + key + `" jsonb`} {
			if !strings.Contains(counting.queries[0], fragment) {
				t.Fatalf("shipped statement lacks %q for key %q", fragment, key)
			}
		}
	}
	t.Logf("projected catalog entries=%d shipped_bytes=%d max_row_bytes=%d",
		len(gotCatalog), counting.totalBytes, counting.maxPayload)
}
