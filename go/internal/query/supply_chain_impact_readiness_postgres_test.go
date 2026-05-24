package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func TestPostgresSupplyChainImpactReadinessQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		// Each fact_kind allowlist binding is referenced.
		"fact.fact_kind = ANY($1::text[])",
		"fact.fact_kind = ANY($2::text[])",
		"fact.fact_kind = ANY($3::text[])",
		"fact.fact_kind = ANY($4::text[])",
		"fact.fact_kind = ANY($5::text[])",
		"fact.fact_kind = ANY($6::text[])",
		"fact.fact_kind = ANY($7::text[])",
		"fact.fact_kind = ANY($8::text[])",
		// Active-fact gates are pushed into every per-family CTE.
		"generation.status = 'active'",
		"fact.is_tombstone = FALSE",
		// All 7 evidence families plus the source-snapshot rollup must
		// appear so a refactor that drops a CTE branch fails loudly.
		"'vulnerability.advisory' AS family",
		"'vulnerability.exploitability' AS family",
		"'package.consumption' AS family",
		"'package.registry' AS family",
		"'sbom.component' AS family",
		"'sbom.attestation' AS family",
		"'container_image.identity' AS family",
		"'vulnerability.source_snapshot' AS family",
		// Manifest consumption uses the real content_entity discriminator.
		"fact.fact_kind = 'content_entity'",
		"entity_metadata'->>'config_kind' = 'dependency'",
		"payload->>'repo_id'",
		// Source-snapshot completion check uses JSONB containment to
		// avoid boolean-cast errors on non-canonical payload values, and
		// surfaces all distinct warning messages.
		`payload @> '{"complete": false}'::jsonb`,
		"ARRAY_AGG(DISTINCT NULLIF(TRIM(payload->>'warning_message'), ''))",
		"JSONB_STRIP_NULLS(JSONB_BUILD_OBJECT(",
		"payload->>'cache_artifact_version'",
		"payload->>'cache_snapshot_digest'",
		"payload->>'cache_updated_at'",
		"payload->>'cache_freshness'",
		// Source-state aggregation must be scoped and bounded so derived
		// per-package OSV rows cannot make unrelated readiness calls
		// target_incomplete or grow response size without limit.
		"vulnerability_source_state_candidates AS (",
		"scope_id IN ($9, $10, $11, $12)",
		"scope_id NOT LIKE 'vuln-intel://osv/%/%?version=%'",
		"ORDER BY CASE WHEN scope_id IN ($9, $10, $11, $12) THEN 0 ELSE 1 END",
		"LIMIT 200",
		"FROM vulnerability_source_state_candidates",
	} {
		if !strings.Contains(listSupplyChainImpactReadinessQuery, want) {
			t.Fatalf("listSupplyChainImpactReadinessQuery missing %q:\n%s", want, listSupplyChainImpactReadinessQuery)
		}
	}
}

type rejectingSupplyChainImpactReadinessQueryer struct{ called int }

func (r *rejectingSupplyChainImpactReadinessQueryer) QueryContext(
	_ context.Context,
	_ string,
	_ ...any,
) (*sql.Rows, error) {
	r.called++
	return nil, fmt.Errorf("Postgres must not be queried for impact_status-only readiness")
}

func TestPostgresSupplyChainImpactReadinessSkipsImpactStatusOnlyScope(t *testing.T) {
	t.Parallel()

	// Regression for the reviewer thread on impact_status-only requests.
	// impact_status is a reducer-finding attribute that does not appear on
	// source facts; an unanchored readiness scan over the active fact set
	// would be expensive and would report unrelated counts as evidence.
	// The store must short-circuit BEFORE issuing the SQL.
	db := &rejectingSupplyChainImpactReadinessQueryer{}
	store := NewPostgresSupplyChainImpactReadinessStore(db)
	snapshot, err := store.ReadSupplyChainImpactReadiness(
		context.Background(),
		SupplyChainImpactReadinessQuery{ImpactStatus: "affected_exact"},
	)
	if err != nil {
		t.Fatalf("ReadSupplyChainImpactReadiness() error = %v, want nil", err)
	}
	if db.called != 0 {
		t.Fatalf("QueryContext invocations = %d, want 0 for impact_status-only scope", db.called)
	}
	if len(snapshot.EvidenceSources) != 0 || snapshot.TargetIncomplete {
		t.Fatalf("snapshot = %#v, want empty for impact_status-only scope", snapshot)
	}
}

func TestPostgresSupplyChainImpactReadinessScansForFactAnchoredScope(t *testing.T) {
	t.Parallel()

	// Companion regression: when the scope DOES carry a fact-anchor
	// (cve_id / package_id / repository_id / subject_digest), the store
	// must still issue the SQL so the short-circuit above is narrow.
	db := &countingSupplyChainImpactReadinessQueryer{}
	store := NewPostgresSupplyChainImpactReadinessStore(db)
	_, _ = store.ReadSupplyChainImpactReadiness(
		context.Background(),
		SupplyChainImpactReadinessQuery{CVEID: "CVE-2026-0001", ImpactStatus: "affected_exact"},
	)
	if db.called != 1 {
		t.Fatalf("QueryContext invocations = %d, want 1 for fact-anchored scope", db.called)
	}
}

type countingSupplyChainImpactReadinessQueryer struct{ called int }

func (c *countingSupplyChainImpactReadinessQueryer) QueryContext(
	_ context.Context,
	_ string,
	_ ...any,
) (*sql.Rows, error) {
	c.called++
	// Returning a nil rows + error short-circuits the store call but proves
	// the SQL was issued for the anchored scope.
	return nil, fmt.Errorf("counting only")
}
