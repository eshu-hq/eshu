// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// The live-Postgres EXCLUSION proof for CurrentAuthorizedCloudResourceUIDs
// (#5794).
//
// This function is the gate that stops a stale or cross-scope CloudResource
// from becoming `runtime_confirmed` supply-chain evidence. Its INCLUSION
// direction — a current, authorized resource promotes the tier — is proven live
// by the B-7 golden corpus gate. Its exclusion direction was proven only by a Go
// stub inventory that bypasses the SQL entirely, plus the observation that
// buildCloudResourceCurrentInventoryQuery's freshness predicate is
// byte-identical to the live-proven buildCloudResourceIdentityListQuery sibling.
//
// A stub cannot fail the way the real thing fails. It cannot produce the row
// that leaks, because it is not the query that would leak it — so it would keep
// passing after an edit that diverged the candidate-keyed subquery from its
// sibling, and the only thing left standing between a stale resource and a
// security evidence field would be a shape test. This runs the real SQL against
// real rows, with one row per predicate: each of the three freshness clauses
// and the authorization clause is isolated by a row that ONLY that clause
// excludes, so deleting any one of them fails this test. Verified by deleting
// each in turn.
//
// Run with:
//
//	ESHU_POSTGRES_TEST_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test -tags=integration ./internal/query -run AuthorizedCloudResourceUIDs -count=1

// authorizedUIDExclusionCase is one seeded uid and why it must or must not come
// back. Naming the reason on the row keeps a failure message pointing at the
// rule that broke rather than at an opaque uid.
type authorizedUIDExclusionCase struct {
	uid    string
	reason string
	// wantScoped is the expectation for a caller granted scope:allowed only.
	wantScoped bool
	// wantAllScopes is the expectation for an admin caller. It differs from
	// wantScoped ONLY for the cross-scope row: admin widens authorization, it
	// must never widen FRESHNESS.
	wantAllScopes bool
}

func authorizedUIDExclusionCases() []authorizedUIDExclusionCase {
	return []authorizedUIDExclusionCase{
		{
			uid:           "uid-current-authorized",
			reason:        "current generation, not tombstoned, in an allowed scope",
			wantScoped:    true,
			wantAllScopes: true,
		},
		{
			uid:           "uid-tombstoned",
			reason:        "source fact is tombstoned, so the resource no longer exists",
			wantScoped:    false,
			wantAllScopes: false,
		},
		{
			uid:           "uid-superseded",
			reason:        "source fact belongs to a generation the scope has since replaced",
			wantScoped:    false,
			wantAllScopes: false,
		},
		{
			// The predicate-isolating row, and the reason this case exists
			// separately from uid-superseded above. That one is excluded by
			// `generation.status = 'active'` on its own, so it proves nothing
			// about the scope's active-generation POINTER: deleting
			// `scope.active_generation_id = fact.generation_id` from the query
			// leaves it passing. This row carries an ACTIVE generation that is
			// simply not the one its scope currently points at, so only that
			// equality excludes it. Proven by deleting the clause and watching
			// this row leak.
			uid:           "uid-not-active-generation",
			reason:        "generation is active but is not the scope's current active_generation_id",
			wantScoped:    false,
			wantAllScopes: false,
		},
		{
			// The third freshness predicate, isolated the same way. Both rows
			// above are ALSO excluded by `scope.active_generation_id =
			// fact.generation_id`, so neither says anything about
			// `generation.status = 'active'`. This row's generation IS the one
			// its scope points at, but that generation is not active — a real
			// transient when an activation is rolled back or races, and exactly
			// what the status clause defends. Its scope is granted to the scoped
			// caller so the row tests freshness, not authorization.
			uid:           "uid-status-not-active",
			reason:        "generation is the scope's active pointer but its own status is not 'active'",
			wantScoped:    false,
			wantAllScopes: false,
		},
		{
			uid:    "uid-cross-scope",
			reason: "current and live, but in a scope the scoped caller holds no grant for",
			// The one row where the two callers legitimately disagree: it is
			// fresh, so an admin sees it; it is out of scope, so a scoped
			// caller must not.
			wantScoped:    false,
			wantAllScopes: true,
		},
	}
}

func TestCurrentAuthorizedCloudResourceUIDsExcludesStaleAndUnauthorizedLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live authorized-uid exclusion proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	seedAuthorizedUIDExclusionCorpus(t, ctx, db)
	store := NewPostgresCloudResourceListStore(db)

	cases := authorizedUIDExclusionCases()
	candidates := make([]string, 0, len(cases))
	for _, testCase := range cases {
		candidates = append(candidates, testCase.uid)
	}

	for _, caller := range []struct {
		name      string
		allScopes bool
		scopeIDs  []string
		want      func(authorizedUIDExclusionCase) bool
	}{
		{
			name:      "scoped caller",
			allScopes: false,
			scopeIDs:  []string{"scope:allowed", "scope:stale-status"},
			want:      func(c authorizedUIDExclusionCase) bool { return c.wantScoped },
		},
		{
			// Proves admin widens AUTHORIZATION without widening FRESHNESS.
			// If the freshness predicate were ever moved inside the
			// authorization branch, this is the caller that would start
			// returning tombstoned and superseded rows.
			name:      "all-scopes caller",
			allScopes: true,
			scopeIDs:  nil,
			want:      func(c authorizedUIDExclusionCase) bool { return c.wantAllScopes },
		},
	} {
		t.Run(caller.name, func(t *testing.T) {
			got, err := store.CurrentAuthorizedCloudResourceUIDs(ctx, candidates, caller.allScopes, nil, caller.scopeIDs)
			if err != nil {
				t.Fatalf("CurrentAuthorizedCloudResourceUIDs() error = %v, want nil", err)
			}

			want := make([]string, 0, len(cases))
			for _, testCase := range cases {
				if caller.want(testCase) {
					want = append(want, testCase.uid)
				}
			}
			sort.Strings(got)
			sort.Strings(want)

			// Asserted as an exact set, not "contains": an over-broad result is
			// precisely the leak this gate exists to prevent, and a containment
			// assertion cannot see one.
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("CurrentAuthorizedCloudResourceUIDs() = %v, want exactly %v\nper-uid rules:\n%s",
					got, want, authorizedUIDExclusionRules(cases))
			}
		})
	}
}

// authorizedUIDExclusionRules renders the seeded rows and their reasons for a
// failure message.
func authorizedUIDExclusionRules(cases []authorizedUIDExclusionCase) string {
	var out strings.Builder
	for _, testCase := range cases {
		out.WriteString("  ")
		out.WriteString(testCase.uid)
		out.WriteString(": ")
		out.WriteString(testCase.reason)
		out.WriteString("\n")
	}
	return out.String()
}

// seedAuthorizedUIDExclusionCorpus builds the smallest corpus that exercises
// every exclusion branch, mirroring the production table shapes used by
// seedCloudResourceListLiveCorpus.
//
// scope:allowed has REPLACED generation:allowed-old with generation:allowed, so
// a fact still carrying the old generation is superseded rather than deleted —
// the shape that a naive "is_tombstone = false" freshness check would miss.
//
// generation:allowed-stale is the same idea one level deeper: still marked
// 'active', but no longer the generation its scope points at. The freshness rule
// is two independent predicates (status AND the scope's active pointer), so it
// needs two rows to isolate them; one row would let either clause be deleted
// with the test still green.
func seedAuthorizedUIDExclusionCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	statements := []string{
		`CREATE TEMP TABLE ingestion_scopes (
          scope_id text PRIMARY KEY, scope_kind text NOT NULL, source_key text NOT NULL,
          active_generation_id text
        )`,
		`CREATE TEMP TABLE scope_generations (
          generation_id text PRIMARY KEY, scope_id text NOT NULL, status text NOT NULL
        )`,
		`CREATE TEMP TABLE fact_records (
          fact_id text PRIMARY KEY, scope_id text NOT NULL, generation_id text NOT NULL,
          is_tombstone boolean NOT NULL
        )`,
		`CREATE TEMP TABLE graph_node_owner (
          uid text PRIMARY KEY, winning_row jsonb NOT NULL
        )`,
		`INSERT INTO ingestion_scopes VALUES
          ('scope:allowed',      'repository', 'repository:allowed', 'generation:allowed'),
          ('scope:stale-status', 'repository', 'repository:stale',   'generation:status-inactive'),
          ('scope:other',        'repository', 'repository:other',   'generation:other')`,
		`INSERT INTO scope_generations VALUES
          ('generation:allowed',        'scope:allowed', 'active'),
          ('generation:allowed-old',    'scope:allowed', 'superseded'),
          ('generation:allowed-stale',  'scope:allowed', 'active'),
          ('generation:status-inactive','scope:stale-status', 'superseded'),
          ('generation:other',          'scope:other',   'active')`,
		`INSERT INTO fact_records VALUES
          ('fact-current-authorized', 'scope:allowed', 'generation:allowed',     false),
          ('fact-tombstoned',         'scope:allowed', 'generation:allowed',     true),
          ('fact-superseded',         'scope:allowed', 'generation:allowed-old', false),
          ('fact-not-active-gen',     'scope:allowed', 'generation:allowed-stale', false),
          ('fact-status-not-active',  'scope:stale-status', 'generation:status-inactive', false),
          ('fact-cross-scope',        'scope:other',   'generation:other',       false)`,
		`INSERT INTO graph_node_owner
          SELECT uid,
                 jsonb_build_object(
                   'source_fact_id', fact_id,
                   'resource_type', 'aws_ec2_instance',
                   'collector_kind', 'aws',
                   'region', 'us-east-1',
                   'account_id', '123456789012',
                   'running_image_digest', digest,
                   'arn', 'arn:example:compute:::resource/' || uid
                 )
          FROM (VALUES
            ('uid-current-authorized', 'fact-current-authorized', 'sha256:` + strings.Repeat("a", 64) + `'),
            ('uid-tombstoned',         'fact-tombstoned',         'sha256:` + strings.Repeat("b", 64) + `'),
            ('uid-superseded',         'fact-superseded',         'sha256:` + strings.Repeat("c", 64) + `'),
            ('uid-not-active-generation', 'fact-not-active-gen',  'sha256:` + strings.Repeat("e", 64) + `'),
            ('uid-status-not-active',  'fact-status-not-active',  'sha256:` + strings.Repeat("f", 64) + `'),
            ('uid-cross-scope',        'fact-cross-scope',        'sha256:` + strings.Repeat("d", 64) + `')
          ) AS seed(uid, fact_id, digest)`,
		`ANALYZE ingestion_scopes`,
		`ANALYZE scope_generations`,
		`ANALYZE fact_records`,
		`ANALYZE graph_node_owner`,
	}
	for i, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed statement %d: %v\n%s", i, err, statement)
		}
	}
}
