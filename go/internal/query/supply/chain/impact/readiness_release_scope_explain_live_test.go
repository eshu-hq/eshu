// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10015 disposable PostgreSQL EXPLAIN (ANALYZE, BUFFERS) proof that release coverage expands a worst-case planner run once for an OS request, not at all for a non-OS request, and never scans the source-state tables
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// releaseScopeCorpus sizes the fixture. Each Alpine package gets a current scope
// on two releases plus one superseded version, the shapes that make several
// rows match one package.
type releaseScopeCorpus struct {
	alpinePackages int
	npmPackages    int
	historicalRuns int
}

// seedReleaseScopeExplainCorpus writes the states, keys, and planner runs the
// derived legs read. The latest run plans every Alpine package on both
// releases as single targets, the shape with the most rows to expand.
func seedReleaseScopeExplainCorpus(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, corpus releaseScopeCorpus) {
	t.Helper()
	configuration := `{"targets": [], "derive_from_owned_packages": {"enabled": true, "sources": ["osv"], "ecosystems": ["npm"]},
        "derive_from_installed_evidence": {"enabled": true, "sources": ["osv"], "ecosystems": ["alpine"]}}`
	if _, err := db.ExecContext(ctx, `INSERT INTO collector_instances
        (instance_id, collector_kind, mode, enabled, configuration, last_observed_at, created_at, updated_at)
        VALUES ('derived', 'vulnerability_intelligence', 'continuous', TRUE, $1, $2, $2, $2)`,
		configuration, now); err != nil {
		t.Fatalf("seed derived instance: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
WITH scopes AS (
    SELECT FORMAT('vuln-intel://osv/npm/npm-%s?version=1.0.0', i) AS scope_id, 'npm' AS ecosystem, 'npm' AS key_ecosystem,
        FORMAT('npm-%s', i) AS package_name, $2::timestamptz - INTERVAL '1 hour' AS at
    FROM GENERATE_SERIES(0, $3 - 1) AS i
    UNION ALL
    SELECT FORMAT('vuln-intel://osv/alpine/alp-%s?release=%s&version=%s', i, shape.release, shape.version), 'alpine', 'os',
        FORMAT('alp-%s', i), $2::timestamptz - shape.age
    FROM GENERATE_SERIES(0, $1 - 1) AS i
    CROSS JOIN (VALUES ('v3.17', '1.0.0-r0', INTERVAL '30 minutes'), ('v3.18', '1.0.0-r0', INTERVAL '20 minutes'),
        ('v3.18', '0.9.0-r0', INTERVAL '20 days')) AS shape(release, version, age)
), states AS (
    INSERT INTO vulnerability_source_states
        (collector_instance_id, scope_id, source, ecosystem, freshness_state, terminal_status,
         last_attempt_at, last_success_at, updated_at)
    SELECT 'derived', scope_id, 'osv', ecosystem, 'fresh', 'succeeded', at, at, at FROM scopes
)
INSERT INTO vulnerability_source_state_packages (collector_instance_id, scope_id, ecosystem, package_name)
SELECT 'derived', scope_id, key_ecosystem, package_name FROM scopes`,
		corpus.alpinePackages, now, corpus.npmPackages); err != nil {
		t.Fatalf("seed release scope states: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workflow_runs
        (run_id, trigger_kind, status, requested_scope_set, requested_collector, created_at, updated_at)
        SELECT 'historical-' || i, 'schedule', 'collection_complete',
            JSONB_BUILD_OBJECT('collector_instance_id', 'derived', 'targets', '[]'::jsonb),
            'vulnerability_intelligence', $1::timestamptz - (i || ' hours')::interval, $1::timestamptz - (i || ' hours')::interval
        FROM GENERATE_SERIES(1, $2) AS i`, now, corpus.historicalRuns); err != nil {
		t.Fatalf("seed historical planner runs: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workflow_runs
        (run_id, trigger_kind, status, requested_scope_set, requested_collector, created_at, updated_at)
        SELECT 'latest', 'schedule', 'collection_complete', JSONB_BUILD_OBJECT('collector_instance_id', 'derived', 'targets', JSONB_AGG(
            JSONB_BUILD_OBJECT('source', 'osv', 'ecosystem', 'alpine', 'derived', TRUE, 'target_class', 'installed_os_package',
                'package_name', 'alp-' || i, 'scope_id', 'vuln-intel://osv/alpine/alp-' || i || '?release=' || release || '&version=1.0.0-r0'))),
            'vulnerability_intelligence', $1, $1
        FROM GENERATE_SERIES(0, $2 - 1) AS i CROSS JOIN (VALUES ('v3.17'), ('v3.18')) AS releases(release)`,
		now, corpus.alpinePackages); err != nil {
		t.Fatalf("seed latest planner run: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO package_manifest_consumption_key_backfill_markers (marker_name, completed_at) VALUES ($1, $2)`,
		storagepostgres.PackageManifestConsumptionKeyBackfillMarker, now); err != nil {
		t.Fatalf("seed readiness marker: %v", err)
	}
	if _, err := db.ExecContext(ctx, `ANALYZE`); err != nil {
		t.Fatalf("analyze release scope corpus: %v", err)
	}
}

// releaseScopeRequest builds readiness arguments for count packages named
// prefix-0.. in one ecosystem, through the same key builder the store uses.
func releaseScopeRequest(t *testing.T, ecosystem, packageIDPrefix, namePrefix string, count int) []any {
	t.Helper()
	target := readinessTarget{}
	for index := range count {
		packageID := fmt.Sprintf("%s%s-%d", packageIDPrefix, namePrefix, index)
		keys := packageidentity.ConsumptionKeys(ecosystem, fmt.Sprintf("%s-%d", namePrefix, index))
		if len(keys) == 0 {
			t.Fatalf("ConsumptionKeys(%s, %s-%d) is empty, the probe would match nothing", ecosystem, namePrefix, index)
		}
		target.PackageIDs = append(target.PackageIDs, packageID)
		for _, key := range keys {
			target.Keys = append(target.Keys, readinessTargetKey{packageID: packageID, key: key})
		}
	}
	return readinessArgsForTarget(target, ReadinessQuery{PackageID: target.PackageIDs[0]})
}

// explainPlanNode is the part of an EXPLAIN (FORMAT JSON) node the proof reads.
type explainPlanNode struct {
	NodeType        string            `json:"Node Type"`
	RelationName    string            `json:"Relation Name"`
	IndexName       string            `json:"Index Name"`
	SubplanName     string            `json:"Subplan Name"`
	ActualLoops     float64           `json:"Actual Loops"`
	ActualRows      float64           `json:"Actual Rows"`
	SharedHitBlocks float64           `json:"Shared Hit Blocks"`
	Plans           []explainPlanNode `json:"Plans"`
}

// walk visits the node and every node below it.
func (n explainPlanNode) walk(visit func(explainPlanNode)) {
	visit(n)
	for _, child := range n.Plans {
		child.walk(visit)
	}
}

func explainReleaseScopeStatement(t *testing.T, ctx context.Context, db *sql.DB, statement string, args []any) (explainPlanNode, float64) {
	t.Helper()
	var raw []byte
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+statement, args...).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN release scope readiness query: %v", err)
	}
	var plans []struct {
		Plan          explainPlanNode `json:"Plan"`
		ExecutionTime float64         `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode release scope readiness plan: err=%v raw=%.200s", err, raw)
	}
	return plans[0].Plan, plans[0].ExecutionTime
}

// TestSupplyChainImpactReadinessReleaseScopeQueryPlanLive locks the shape that
// keeps release coverage bounded. An OS request expands the plan exactly once,
// so a batch of requested packages does not rerun it per configured ecosystem.
// A non-OS request reads no target from the plan. Neither scans a state table.
func TestSupplyChainImpactReadinessReleaseScopeQueryPlanLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #10015 release scope plan proof")
	}
	database := postgresproof.OpenIsolatedSchema(t, dsn, "impact_release_scope", func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	const plannedPackages = 2500
	seedReleaseScopeExplainCorpus(t, ctx, database, time.Now().UTC().Truncate(time.Second), releaseScopeCorpus{
		alpinePackages: plannedPackages, npmPackages: 2500, historicalRuns: 200,
	})

	for _, tc := range []struct {
		name         string
		args         []any
		wantTargets  float64
		wantRequests float64
	}{
		{"os request expands the plan once", releaseScopeRequest(t, "os", "os://os/", "alp", 100), 2 * plannedPackages, 200},
		{"npm request reads no plan target", releaseScopeRequest(t, "npm", "npm://registry.npmjs.org/", "npm", 100), 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, execution := explainReleaseScopeStatement(t, ctx, database, ListReadinessQuery, tc.args)
			t.Logf("release scope readiness plan: execution_ms=%.3f", execution)
			nodes := map[string]explainPlanNode{}
			plan.walk(func(node explainPlanNode) {
				if strings.HasPrefix(node.SubplanName, "CTE ") {
					nodes[strings.TrimPrefix(node.SubplanName, "CTE ")] = node
				}
				if node.NodeType == "Seq Scan" && strings.HasPrefix(node.RelationName, "vulnerability_source_state") {
					t.Errorf("sequential scan on %s, want keyed probes only", node.RelationName)
				}
			})
			for name, wantRows := range map[string]float64{
				"derived_vulnerability_planned_release_targets": tc.wantTargets,
				"derived_vulnerability_planned_requested":       tc.wantRequests,
			} {
				node, ok := nodes[name]
				if !ok {
					t.Fatalf("plan has no materialized %s step", name)
				}
				if wantRows > 0 && node.ActualLoops != 1 {
					t.Fatalf("%s ran %v times, want once", name, node.ActualLoops)
				}
				if node.ActualRows != wantRows {
					t.Fatalf("%s produced %v rows, want %v", name, node.ActualRows, wantRows)
				}
			}
			if execution > 500 {
				t.Fatalf("execution time = %.3fms, want <=500ms (local-performance-envelope budget)", execution)
			}
		})
	}
}

// seedFleetProbeFacts writes one active scope generation per image holding the
// named packages at distroVersion: the installed rows the fleet probe reads.
// The probe joins the same active-generation shape the planner's reader uses,
// so the helper activates what it writes.
func seedFleetProbeFacts(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, image, distroVersion string, count int) {
	t.Helper()
	scopeID, generationID := "scan-target:fleet:"+image, "generation:fleet:"+image
	if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload) VALUES ($1, 'container_image', 'synthetic', $1, 'synthetic', $1, $2, $2, 'active', '{}'::jsonb)`, scopeID, now); err != nil {
		t.Fatalf("seed fleet scope %s: %v", image, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`, generationID, scopeID, now); err != nil {
		t.Fatalf("seed fleet generation %s: %v", image, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, generationID, scopeID); err != nil {
		t.Fatalf("activate fleet %s: %v", image, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload, fencing_token)
SELECT 'os-package:fleet:' || $1 || ':' || i, $1, $2, 'vulnerability.os_package', $1 || ':' || i, 'synthetic', $1 || ':' || i,
  $3::timestamptz, $3::timestamptz, FALSE,
  jsonb_build_object('distro','alpine','distro_version',$4::text,'package_manager','apk','name','alp-' || i,
    'arch','x86_64','repository_class','vendor','vendor_advisory_source','alpine','installed_version_raw','1.0.0-r0',
    'purl','pkg:apk/alpine/alp-' || i || '@1.0.0-r0'), 0
FROM GENERATE_SERIES(0, $5 - 1) AS i`, scopeID, generationID, now, distroVersion, count); err != nil {
		t.Fatalf("seed fleet packages %s: %v", image, err)
	}
}

// TestSupplyChainImpactReadinessFleetProbePlanLive proves the #10135 probe
// fires through the fleet indexes on an OS request: every resolved non-fresh
// release triple seeks the binary-name or source arm instead of scanning the
// installed packages, whether the release is still installed or left the
// fleet, inside the same local-performance envelope as the release plan proof.
func TestSupplyChainImpactReadinessFleetProbePlanLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #10135 fleet probe plan proof")
	}
	for _, tc := range []struct {
		name         string
		olderInFleet bool
	}{
		{"both releases installed", true},
		{"older release left the fleet", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := postgresproof.OpenIsolatedSchema(t, dsn, "impact_fleet_probe", func(ctx context.Context, db *sql.DB) error {
				return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			const packages = 100
			seedReleaseScopeExplainCorpus(t, ctx, database, time.Now().UTC().Truncate(time.Second), releaseScopeCorpus{
				alpinePackages: packages, npmPackages: 0, historicalRuns: 0,
			})
			if _, err := database.ExecContext(ctx, `UPDATE vulnerability_source_states SET freshness_state = 'failed',
        terminal_status = 'failed_retryable' WHERE scope_id LIKE '%release=v3.17%'`); err != nil {
				t.Fatalf("fail the older release states: %v", err)
			}
			if tc.olderInFleet {
				seedFleetProbeFacts(t, ctx, database, time.Now().UTC().Truncate(time.Second), "img-317", "3.17.3", packages)
			}
			seedFleetProbeFacts(t, ctx, database, time.Now().UTC().Truncate(time.Second), "img-318", "3.18.4", packages)
			if _, err := database.ExecContext(ctx, `ANALYZE`); err != nil {
				t.Fatalf("analyze fleet probe corpus: %v", err)
			}

			plan, execution := explainReleaseScopeStatement(t, ctx, database, ListReadinessQuery,
				releaseScopeRequest(t, "os", "os://os/", "alp", packages))
			t.Logf("fleet probe readiness plan: execution_ms=%.3f shared_hit_blocks=%.0f", execution, plan.SharedHitBlocks)
			// The probe's own CTE subplans must seek the fleet indexes and
			// never scan: other legs may probe tiny fact slices with a
			// sequential scan the same way the statement before this change
			// did, so the ban stays scoped to the probe.
			probeScans := map[string]int{}
			var walkProbe func(node explainPlanNode, probe *int)
			walkProbe = func(node explainPlanNode, probe *int) {
				if probe != nil {
					if node.NodeType == "Seq Scan" && node.RelationName == "fact_records" {
						t.Errorf("sequential scan on fact_records inside the fleet probe")
					}
					if strings.Contains(node.IndexName, "fleet_release") {
						*probe++
					}
				}
				for _, child := range node.Plans {
					next := probe
					if child.SubplanName == "CTE derived_vulnerability_release_fleet_probe" ||
						child.SubplanName == "CTE derived_vulnerability_release_fleet_presence" {
						count := 0
						next = &count
						defer func() { probeScans[child.SubplanName] = count }()
					}
					walkProbe(child, next)
				}
			}
			walkProbe(plan, nil)
			// The presence leg holds the fact seeks; the probe-keys leg only
			// resolves triples from the state side, so it needs no index.
			// Both legs must stay scan-free on fact_records.
			if _, ok := probeScans["CTE derived_vulnerability_release_fleet_probe"]; !ok {
				t.Fatalf("fleet probe keys ran no evaluated subplan: the probe shape changed")
			}
			if probeScans["CTE derived_vulnerability_release_fleet_presence"] == 0 {
				t.Fatalf("fleet presence used no fleet_release index: the probe falls back to a scan")
			}
			if execution > 500 {
				t.Fatalf("execution time = %.3fms, want <=500ms (local-performance-envelope budget)", execution)
			}
		})
	}
}
