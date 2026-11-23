// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B2a disposable PostgreSQL 18 plan-shape guard for the two partial indexes of ruling V: the supplement probe of an unscoped precise read reaches the supports table by an Index Scan on the ordered precise partial index with canonical_key in its Index Cond, the image read fetches its candidates through the image partial index inside a MATERIALIZED CTE, neither by a sequential or bitmap scan of the probe; each guard rejects its seeded violation (the index dropped in a rolled-back transaction); reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/jackc/pgx/v5/stdlib"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	preciseOrderedIndex = "supply_chain_impact_finding_supports_precise_key_ordered_idx"
	imageRefIndex       = "supply_chain_impact_finding_supports_image_ref_idx"
)

// planRowQueryer is what the guard needs to explain a statement on a connection
// or inside a transaction.
type planRowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// planAccess is one access to the supports table in a plan: its node type, its
// index, and its Index Cond (a Bitmap Heap Scan reports the Bitmap Index Scan
// beneath it).
type planAccess struct {
	nodeType, index, cond string
}

type planWithCond struct {
	NodeType     string         `json:"Node Type"`
	RelationName string         `json:"Relation Name"`
	IndexName    string         `json:"Index Name"`
	IndexCond    string         `json:"Index Cond"`
	Plans        []planWithCond `json:"Plans"`
}

func supportsAccesses(node planWithCond) []planAccess {
	var out []planAccess
	if node.RelationName == supportsPlanTable {
		access := planAccess{nodeType: node.NodeType, index: node.IndexName, cond: node.IndexCond}
		if node.NodeType == "Bitmap Heap Scan" {
			for _, child := range node.Plans {
				if child.NodeType == "Bitmap Index Scan" {
					access.index, access.cond = child.IndexName, child.IndexCond
				}
			}
		}
		out = append(out, access)
	}
	for _, child := range node.Plans {
		out = append(out, supportsAccesses(child)...)
	}
	return out
}

func explainAccesses(ctx context.Context, queryer planRowQueryer, query string, args []any) ([]planAccess, error) {
	var raw []byte
	if err := queryer.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		return nil, err
	}
	var plan []struct {
		Plan planWithCond `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, err
	}
	if len(plan) != 1 {
		return nil, fmt.Errorf("explain returned %d plans", len(plan))
	}
	return supportsAccesses(plan[0].Plan), nil
}

// supplementProbeUsesTheOrderedIndex reports whether some access to the supports
// table is the ordered partial index by an Index Scan or Index Only Scan with
// canonical_key in its Index Cond, and none is a Seq Scan or a Bitmap scan. The
// check names the index and the node type; "no Seq Scan" alone would pass a plan
// that reached the table by another index.
func supplementProbeUsesTheOrderedIndex(accesses []planAccess) (bool, string) {
	probed := false
	for _, access := range accesses {
		switch {
		case access.nodeType == "Seq Scan", strings.HasPrefix(access.nodeType, "Bitmap"):
			return false, fmt.Sprintf("%s of the supports table (%s)", access.nodeType, access.index)
		case (access.nodeType == "Index Scan" || access.nodeType == "Index Only Scan") &&
			access.index == preciseOrderedIndex && strings.Contains(access.cond, "canonical_key"):
			probed = true
		}
	}
	if !probed {
		return false, fmt.Sprintf("no Index Scan on %s with canonical_key in its Index Cond (accesses %v)", preciseOrderedIndex, accesses)
	}
	return true, ""
}

// imageReadUsesTheImageIndex reports whether the candidate fetch reaches the
// supports table through the image partial index (Index Scan, Index Only Scan,
// or a Bitmap Heap Scan whose Bitmap Index Scan names it, with image_ref in the
// Index Cond) and never by a Seq Scan. Point lookups by primary key and by the
// operator index are the payload fetch and the override probe, not candidates.
func imageReadUsesTheImageIndex(accesses []planAccess) (bool, string) {
	candidates := 0
	for _, access := range accesses {
		if access.nodeType == "Seq Scan" {
			return false, "sequential scan of the supports table"
		}
		// The payload fetch by primary key and the operator override probe by
		// canonical_key are point lookups downstream of the candidate fetch.
		if access.index == "supply_chain_impact_finding_supports_pkey" || access.index == "supply_chain_impact_finding_supports_operator_idx" {
			continue
		}
		candidates++
		if access.index != imageRefIndex || !strings.Contains(access.cond, "image_ref") {
			return false, fmt.Sprintf("%s reaches the supports table by %q (cond %q), want %s with image_ref in the Index Cond", access.nodeType, access.index, access.cond, imageRefIndex)
		}
	}
	if candidates == 0 {
		return false, "the plan never fetches image candidates from the supports table"
	}
	return true, ""
}

// seedRouteVPlanCorpus writes 4,000 keys of 12 supports each: a third of each
// key's supports are precise, the best-priority support is often not, and every
// key carries one image reference per support (500 distinct). The winners table is
// built from the supports with the maintainer's order, then everything is analyzed.
func seedRouteVPlanCorpus(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'scope:rv:' || n, 'synthetic', 'synthetic', 'k' || n, 'synthetic', 'k' || n, NOW(), NOW(), 'active', 'gen:rv:' || n, '{}'::jsonb
FROM generate_series(1, 200) AS n`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
SELECT 'gen:rv:' || n, 'scope:rv:' || n, 'synthetic', NOW(), NOW(), 'active', NOW(), '{}'::jsonb FROM generate_series(1, 200) AS n`,
		`INSERT INTO supply_chain_impact_finding_supports (fact_id, canonical_key, scope_id, generation_id, finding_id,
  has_payload_finding_id, priority_score, source_confidence, suppression_state, impact_status, ecosystem, severity_bucket,
  repository_id, cve_id, advisory_id, package_id, subject_digest, image_ref, priority_bucket, detection_profile,
  observed_version, match_reason, payload)
SELECT 'fact:rv:' || k || ':' || j, 'key:' || k, 'scope:rv:' || (((k * 7 + j) % 200) + 1), 'gen:rv:' || (((k * 7 + j) % 200) + 1),
       'finding:' || k, 1, (j * 17 + k) % 100, 'observed', 'active', 'affected_exact', 'npm', 'high',
       'repository:rv:' || (k % 300), 'CVE-2026-' || k, '', 'pkg:npm/p' || (k % 500), '',
       'registry.example/img' || ((k * 13 + j) % 500) || ':1', 'high', '', '1.0',
       CASE WHEN j % 3 = 0 THEN 'npm_semver_affected_range' ELSE 'dpkg_affected_range' END,
       jsonb_build_object('finding_id', 'finding:' || k, 'priority_score', (j * 17 + k) % 100)
FROM generate_series(1, 4000) AS k, generate_series(1, 12) AS j`,
		`INSERT INTO supply_chain_impact_canonical_winners (canonical_key, winner_fact_id, winner_scope_id, finding_id,
  priority_score, source_count, impact_status, ecosystem, severity_bucket, repository_id, cve_id, advisory_id, package_id,
  subject_digest, image_ref, priority_bucket, detection_profile, observed_version, match_reason, suppression_state,
  materialized_at, payload, source_confidence)
SELECT DISTINCT ON (s.canonical_key) s.canonical_key, s.fact_id, s.scope_id, s.finding_id, s.priority_score,
  count(*) OVER (PARTITION BY s.canonical_key)::int, s.impact_status, s.ecosystem, s.severity_bucket, s.repository_id,
  s.cve_id, s.advisory_id, s.package_id, s.subject_digest, s.image_ref, s.priority_bucket, s.detection_profile,
  s.observed_version, s.match_reason, s.suppression_state, NOW(), s.payload, s.source_confidence
FROM supply_chain_impact_finding_supports AS s
ORDER BY s.canonical_key, s.priority_score DESC, s.has_payload_finding_id DESC, s.fact_id ASC`,
		// The two indexes ship in migrations 343 and 344; creating them here by the
		// same name and predicate keeps the guard independent of the migration order.
		`CREATE INDEX IF NOT EXISTS ` + preciseOrderedIndex + `
ON supply_chain_impact_finding_supports (canonical_key, priority_score DESC, has_payload_finding_id DESC, fact_id ASC)
WHERE ` + SupportsPreciseIndexPredicateSQL,
		`CREATE INDEX IF NOT EXISTS ` + imageRefIndex + ` ON supply_chain_impact_finding_supports (image_ref) WHERE image_ref <> ''`,
		`ANALYZE supply_chain_impact_finding_supports`,
		`ANALYZE supply_chain_impact_canonical_winners`,
		`ANALYZE ingestion_scopes`,
		`ANALYZE scope_generations`,
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed the route V plan corpus: %v\n%.120s", err, statement)
		}
	}
}

// TestRouteVReadsKeepTheirIndexedPlanShapeLive is ruling V's plan-shape guard
// (required proof 4). The supplement probe must be an Index Scan on the ordered
// precise index with canonical_key in its Index Cond; the image read must reach
// the supports table only through the image index, inside a MATERIALIZED CTE.
// Each checker then rejects the plan the same statement gets once its index is
// dropped (in a rolled-back transaction), so a guard that passes everything
// cannot hide.
func TestRouteVReadsKeepTheirIndexedPlanShapeLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(
		t,
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DSN"),
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE"),
		10*time.Minute,
		func(ctx context.Context, db *sql.DB) error {
			return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
		},
	)
	seedRouteVPlanCorpus(t, ctx, database)

	supplementFilter := FindingFilter{Limit: 50, Status: "affected_exact", DetectionProfile: ProfilePrecise}
	imageFilter := FindingFilter{Limit: 50, Status: "affected_exact", ImageRef: "registry.example/img7:1"}
	if !strings.Contains(ListFindingsFromSupportsImageQuery, "image_supports AS MATERIALIZED") {
		t.Fatal("the image read lost its MATERIALIZED candidate fetch")
	}
	cases := []struct {
		name  string
		query string
		args  []any
		index string
		check func([]planAccess) (bool, string)
	}{
		{"supplement probe", ListFindingsFromWinnersSupplementQuery, multiSupportWinnersListArgs(ctx, supplementFilter), preciseOrderedIndex, supplementProbeUsesTheOrderedIndex},
		{"image read", ListFindingsFromSupportsImageQuery, multiSupportWinnersListArgs(ctx, imageFilter), imageRefIndex, imageReadUsesTheImageIndex},
	}
	explainImage := supplyChainImpactExplanationQueryArgs(ctx, packet.ExplanationFilter{CVEID: "CVE-2026-77", ImageRef: "registry.example/img7:1"}, nil)
	cases = append(cases,
		struct {
			name  string
			query string
			args  []any
			index string
			check func([]planAccess) (bool, string)
		}{"explain by image", ExplainFindingFromImageQuery, explainImage, imageRefIndex, imageReadUsesTheImageIndex},
		// The identity arm on the image statement: $2 is the fact id of a lower copy
		// (fact:rv:100:2 carries registry.example/img302:1), so the identity predicate
		// is non-empty and the image fence must still drive the read.
		struct {
			name  string
			query string
			args  []any
			index string
			check func([]planAccess) (bool, string)
		}{
			"explain by image with a lower-copy fact id", ExplainFindingFromImageQuery,
			supplyChainImpactExplanationQueryArgs(ctx, packet.ExplanationFilter{FindingID: "fact:rv:100:2", ImageRef: "registry.example/img302:1"}, nil),
			imageRefIndex, imageReadUsesTheImageIndex,
		},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			accesses, err := explainAccesses(ctx, database, tc.query, tc.args)
			if err != nil {
				t.Fatalf("explain: %v", err)
			}
			t.Logf("accesses of the supports table: %v", accesses)
			if ok, reason := tc.check(accesses); !ok {
				t.Fatalf("plan lost its shape: %s", reason)
			}

			// Seeded violation: the same statement with its index dropped.
			tx, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer func() { _ = tx.Rollback() }()
			drop := "DROP INDEX " + tc.index
			if _, err := tx.ExecContext(ctx, drop); err != nil {
				t.Fatalf("drop %s: %v", tc.index, err)
			}
			dropped, err := explainAccesses(ctx, tx, tc.query, tc.args)
			if err != nil {
				t.Fatalf("explain without the index: %v", err)
			}
			t.Logf("accesses without the index: %v", dropped)
			if ok, _ := tc.check(dropped); ok {
				t.Fatalf("the checker accepted the plan without %s (accesses %v): it would not catch a lost index", tc.index, dropped)
			}
		})
	}
}

// explainGenericAccesses plans a statement as a generic plan (EXPLAIN (GENERIC_PLAN),
// no bound values), the plan a connection reuses after a statement's fifth
// execution. It sends the text over the simple protocol on one connection, so the
// $n placeholders are not bound, and runs prefix statements first on that same
// connection (the dropped-index check puts BEGIN and DROP INDEX there and ROLLBACK
// after).
func explainGenericAccesses(ctx context.Context, database *sql.DB, query string, before, after []string) ([]planAccess, error) {
	conn, err := database.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	var accesses []planAccess
	err = conn.Raw(func(driverConn any) error {
		pgConn := driverConn.(*stdlib.Conn).Conn().PgConn()
		sql := strings.Join(before, "; ")
		if sql != "" {
			sql += "; "
		}
		sql += "EXPLAIN (GENERIC_PLAN, FORMAT JSON) " + query
		for _, statement := range after {
			sql += "; " + statement
		}
		results, err := pgConn.Exec(ctx, sql).ReadAll()
		if err != nil {
			return err
		}
		for _, result := range results {
			if len(result.Rows) == 1 && len(result.Rows[0]) == 1 && strings.HasPrefix(string(result.Rows[0][0]), "[") {
				var plan []struct {
					Plan planWithCond `json:"Plan"`
				}
				if err := json.Unmarshal(result.Rows[0][0], &plan); err != nil {
					return err
				}
				if len(plan) != 1 {
					return fmt.Errorf("explain returned %d plans", len(plan))
				}
				accesses = supportsAccesses(plan[0].Plan)
				return nil
			}
		}
		return fmt.Errorf("no plan in the simple-protocol result")
	})
	return accesses, err
}

// TestImageExplainKeepsItsIndexUnderAGenericPlanLive pins that the image statement,
// which a public-id miss with an image reference now runs with a non-empty identity
// predicate, still drives from the image partial index under the generic plan a
// connection reuses (the bound identity is unknown to a generic plan), and that the
// checker rejects the generic plan once the index is dropped in a rolled-back
// transaction.
func TestImageExplainKeepsItsIndexUnderAGenericPlanLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(
		t,
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DSN"),
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE"),
		10*time.Minute,
		func(ctx context.Context, db *sql.DB) error {
			return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
		},
	)
	seedRouteVPlanCorpus(t, ctx, database)
	accesses, err := explainGenericAccesses(ctx, database, ExplainFindingFromImageQuery, nil, nil)
	if err != nil {
		t.Fatalf("generic plan: %v", err)
	}
	t.Logf("generic-plan accesses of the supports table: %v", accesses)
	if ok, reason := imageReadUsesTheImageIndex(accesses); !ok {
		t.Fatalf("the generic plan lost the image index: %s", reason)
	}
	dropped, err := explainGenericAccesses(ctx, database, ExplainFindingFromImageQuery,
		[]string{"BEGIN", "DROP INDEX " + imageRefIndex}, []string{"ROLLBACK"})
	if err != nil {
		t.Fatalf("generic plan without the index: %v", err)
	}
	if ok, _ := imageReadUsesTheImageIndex(dropped); ok {
		t.Fatalf("the checker accepted the generic plan without %s (accesses %v)", imageRefIndex, dropped)
	}
}
