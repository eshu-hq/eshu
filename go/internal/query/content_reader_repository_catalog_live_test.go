// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8837 disposable PostgreSQL proof of the grant-to-slug catalog read: repository and scope id grants map to their catalog entries, other scope kinds are excluded, and EXPLAIN (ANALYZE, BUFFERS) over a 10,000-repository catalog at grant sizes 1, 100 and 5,000 is logged, not gated; isolated bootstrapped schema per test
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const grantCatalogLiveSkip = "set ESHU_POSTGRES_DSN to run the #8837 grant-to-slug catalog live proofs"

// grantCatalogLiveFixture seeds ingestion_scopes in an isolated bootstrapped
// schema.
type grantCatalogLiveFixture struct {
	t        *testing.T
	ctx      context.Context
	database *sql.DB
	asOf     time.Time
}

func newGrantCatalogLiveFixture(t *testing.T, prefix string) *grantCatalogLiveFixture {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip(grantCatalogLiveSkip)
	}
	database := postgresproof.OpenIsolatedSchema(t, dsn, prefix, func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	return &grantCatalogLiveFixture{t: t, ctx: ctx, database: database, asOf: time.Now().UTC().Truncate(time.Second)}
}

func (f *grantCatalogLiveFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.database.ExecContext(f.ctx, query, args...); err != nil {
		f.t.Fatalf("exec: %v", err)
	}
}

// seedRepositoryCatalog inserts count repository-kind scopes with
// repo_id/payload slugs, plus one organization-kind scope carrying the first
// repository's repo_id to prove the kind filter excludes it.
func (f *grantCatalogLiveFixture) seedRepositoryCatalog(count int) {
	f.t.Helper()
	f.exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
SELECT 'scope:s_' || g, 'repository', 'test', 'key-' || g, 'test', 'part-' || g, $1, $1, 'active',
	jsonb_build_object('repo_id', 'repository:r_' || g, 'repo_slug', 'example-org/repo-' || g, 'name', 'repo-' || g)
FROM generate_series(1, $2) AS g`, f.asOf, count)
	f.exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
VALUES ('scope:org_decoy', 'organization', 'test', 'org-key', 'test', 'org-part', $1, $1, 'active',
	jsonb_build_object('repo_id', 'repository:r_1', 'repo_slug', 'example-org/repo-1'))`, f.asOf)
	f.exec(`VACUUM ANALYZE ingestion_scopes`)
}

// TestMatchRepositoriesByIDsLive proves the grant-to-slug catalog read (#8837)
// against the real schema: a canonical repository id and an ingestion scope
// id each map to their entry, an unknown id maps to nothing, and a
// non-repository-kind row carrying the same repo_id is excluded.
func TestMatchRepositoriesByIDsLive(t *testing.T) {
	f := newGrantCatalogLiveFixture(t, "grant_catalog_map")
	f.seedRepositoryCatalog(10)

	reader := NewContentReader(f.database)
	entries, err := reader.MatchRepositoriesByIDs(f.ctx, []string{"repository:r_1", "scope:s_2", "repository:r_unknown"})
	if err != nil {
		t.Fatalf("MatchRepositoriesByIDs: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("matched %d entries, want 2 (the organization-kind decoy must not match)", len(entries))
	}
	slugs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.RepoSlug == "" {
			t.Fatalf("entry %q carries no repo_slug", entry.ID)
		}
		slugs = append(slugs, entry.RepoSlug)
	}
	slices.Sort(slugs)
	if got, want := strings.Join(slugs, ","), "example-org/repo-1,example-org/repo-2"; got != want {
		t.Fatalf("slugs = %q, want %q", got, want)
	}
}

// TestMatchRepositoriesByIDsPlanLive records EXPLAIN (ANALYZE, BUFFERS) for
// the shipped grant-to-slug query over a 10,000-repository catalog at grant
// sizes 1, 100 and 5,000 (#8837 acceptance criterion). Plans and timings are
// logged, not gated; the figures feed
// docs/internal/evidence/8837-security-alert-grant-scope.md.
func TestMatchRepositoriesByIDsPlanLive(t *testing.T) {
	f := newGrantCatalogLiveFixture(t, "grant_catalog_plan")
	f.seedRepositoryCatalog(10000)

	grantIDs := func(n int) []string {
		ids := make([]string, 0, n)
		for i := 1; i <= n; i++ {
			if i%2 == 0 {
				ids = append(ids, fmt.Sprintf("scope:s_%d", i))
			} else {
				ids = append(ids, fmt.Sprintf("repository:r_%d", i))
			}
		}
		return ids
	}
	for _, size := range []int{1, 100, 5000} {
		ids := grantIDs(size)
		// EXPLAIN returns one row per plan line; concatenate them.
		rows, err := f.database.QueryContext(f.ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT `+repositoryCatalogIDExpr+` AS id,
       coalesce(payload->>'repo_slug', '') AS repo_slug,
       scope_id
FROM ingestion_scopes
WHERE scope_kind = 'repository'
  AND (`+repositoryCatalogIDExpr+` = ANY($1) OR scope_id = ANY($1))
ORDER BY scope_id`, array.Of(ids))
		if err != nil {
			t.Fatalf("explain at grant size %d: %v", size, err)
		}
		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				_ = rows.Close()
				t.Fatalf("scan plan at grant size %d: %v", size, err)
			}
			lines = append(lines, line)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate plan at grant size %d: %v", size, err)
		}
		plan := strings.Join(lines, "\n")
		timing := ""
		for _, line := range lines {
			if strings.Contains(line, "Execution Time:") {
				timing = strings.TrimSpace(line)
			}
		}
		// The read must return exactly the granted rows.
		reader := NewContentReader(f.database)
		entries, err := reader.MatchRepositoriesByIDs(f.ctx, ids)
		if err != nil {
			t.Fatalf("read at grant size %d: %v", size, err)
		}
		if len(entries) != size {
			t.Fatalf("matched %d entries at grant size %d, want %d", len(entries), size, size)
		}
		t.Logf("grant size %d: %s\n%s", size, timing, plan)
	}
}
