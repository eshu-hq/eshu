// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
		"scope_id IN ($9, $10, $11, $12, $14)",
		"ORDER BY CASE WHEN scope_id IN ($9, $10, $11, $12, $14) THEN 0 ELSE 1 END",
		"LIMIT 200",
		"FROM vulnerability_source_state_candidates",
		// The unsupported target aggregation must surface the closed set of
		// kinds, scope filtering by repo_id/subject_digest, and the explicit
		// reason codes so an unrelated refactor cannot silently drop one
		// producer arm.
		"'vulnerability.unsupported_target' AS family",
		"'ecosystem' AS target_kind",
		"'package_manager_file' AS target_kind",
		"'sbom_target' AS target_kind",
		"'package_registry_metadata' AS target_kind",
		"'image_target' AS target_kind",
		"NOT IN\n          ('npm', 'nuget', 'maven', 'cargo', 'pypi', 'swift', 'composer', 'go', 'rubygems', 'hex')",
		"entity_metadata'->>'lockfile_unsupported_feature'",
		"package_dependency_gap_active AS (",
		"payload->'entity_metadata'->>'config_kind' IN (",
		"'vcs_dependency'",
		"'path_dependency'",
		"'url_dependency'",
		"'editable_dependency'",
		"'unsupported_dependency'",
		"'dependency_source' AS target_kind",
		"'vcs_dependency_unsupported'",
		"'path_dependency_unsupported'",
		"'url_dependency_unsupported'",
		"'editable_dependency_unsupported'",
		"'unsupported_dependency_unsupported'",
		"FROM package_dependency_gap_active",
		"warn.payload->>'reason' IN ('unsupported_field', 'malformed_document')",
		"doc.payload->>'subject_digest' IN (SELECT digest FROM target_image_digests)",
		"package_registry_warning_active AS (",
		"fact.fact_kind = 'package_registry.warning'",
		"FROM package_registry_warning_active AS warn",
		"warn.payload->>'warning_code' IN (",
		"'unsupported_metadata_source'",
		"'registry_not_found'",
		"'metadata_too_large'",
		"'malformed_metadata'",
		"'credentials_missing'",
		"warn.payload->>'package_id' = $10",
		"scanner_worker_warning_active AS (",
		"fact.fact_kind = 'scanner_worker.warning'",
		"target_image_digests AS (",
		"identity.payload->>'image_ref' = $14",
		"OR ($14 <> '' AND payload->>'image_ref' = $14)",
		"FROM scanner_worker_warning_active AS warn",
		"warn.payload->>'target_kind' = 'image'",
		"warn.payload->>'reason' IN ('analyzer_not_configured', 'image_analyzer_unsupported_target')",
		"warn.payload->>'image_digest' = $12",
		"warn.payload->>'image_ref' = $14",
		"warn.scope_id = $12",
		"warn.scope_id = $14",
		"RIGHT(warn.scope_id, LENGTH('@' || $12)) = '@' || $12",
		"RIGHT(warn.scope_id, LENGTH('@' || $14)) = '@' || $14",
		"FROM package_consumption_correlation_active AS consumption",
		"consumption.payload->>'repository_id' = $11",
		"package_registry_active.payload->>'package_id' = package_registry_scope_packages.package_id",
		// Package-registry freshness must be evaluated across the full
		// consumed package set. One fresh registry row for one consumed
		// package must not mask missing or stale metadata for another.
		"package_registry_scope_packages AS (",
		"package_registry_scoped AS (",
		"LEFT JOIN package_registry_active",
		"COUNT(package_registry_active.payload)::int AS fact_count",
		"BOOL_OR(fact_count = 0)",
		"MIN(latest_observed_at)",
		"$9 = '' AND $10 = '' AND $11 = '' AND $12 = '' AND $14 = ''",
		// Anchor guards: ecosystem and package_manager_file rows only count
		// when the request carries an explicit repository_id, and sbom_target
		// rows only count when the request carries an explicit
		// subject_digest. Without these gates a cve_id-only or
		// subject_digest-only scope would scan all owned dependency rows
		// globally and a repository_id-only scope would pick up SBOM
		// warnings from unrelated images.
		"WHERE $11 <> ''\n      AND payload->>'repo_id' = $11",
		"WHERE doc.payload->>'subject_digest' IN (SELECT digest FROM target_image_digests)",
	} {
		if !strings.Contains(listSupplyChainImpactReadinessQuery, want) {
			t.Fatalf("listSupplyChainImpactReadinessQuery missing %q:\n%s", want, listSupplyChainImpactReadinessQuery)
		}
	}
}

func TestPostgresSupplyChainImpactReadinessScopesSourceFreshness(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"target_vulnerability_source_ecosystems AS (",
		"FROM package_consumption_correlation_active AS consumption",
		"consumption.payload->>'repository_id' = $11",
		"registry.payload->>'package_id' = $10",
		"component.payload->>'subject_digest' IN (SELECT digest FROM target_image_digests)",
		"target_vulnerability_source_scopes AS (",
		"'vuln-intel://nvd/cve' AS scope_id",
		"'vuln-intel://cisa/kev' AS scope_id",
		"'vuln-intel://first/epss' AS scope_id",
		"FROM target_vulnerability_source_scopes AS target",
		"target.ecosystem = NULLIF(LOWER(TRIM(snapshot.payload->>'ecosystem')), '')",
		"target.ecosystem = NULLIF(LOWER(TRIM(state.ecosystem)), '')",
	} {
		if !strings.Contains(listSupplyChainImpactReadinessQuery, want) {
			t.Fatalf("listSupplyChainImpactReadinessQuery missing scoped source freshness fragment %q:\n%s", want, listSupplyChainImpactReadinessQuery)
		}
	}
	if strings.Contains(listSupplyChainImpactReadinessQuery, "scope_id NOT LIKE 'vuln-intel://osv/%/%?version=%'") {
		t.Fatalf("listSupplyChainImpactReadinessQuery still has unanchored source-state fallback:\n%s", listSupplyChainImpactReadinessQuery)
	}
}

func TestPostgresSupplyChainImpactReadinessScopesAdvisoryFacts(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"target_advisory_packages AS (",
		"NULLIF(TRIM($10), '') AS package_id",
		"consumption.payload->>'repository_id' = $11",
		"component.payload->>'subject_digest' IN (SELECT digest FROM target_image_digests)",
		"payload->>'package_id' IN (SELECT package_id FROM target_advisory_packages)",
		"($9 <> '' AND payload->>'cve_id' = $9)",
	} {
		if !strings.Contains(listSupplyChainImpactReadinessQuery, want) {
			t.Fatalf("listSupplyChainImpactReadinessQuery missing advisory scope fragment %q:\n%s", want, listSupplyChainImpactReadinessQuery)
		}
	}
	if strings.Contains(listSupplyChainImpactReadinessQuery, "FROM advisory_active\n    WHERE ($9 = '' OR payload->>'cve_id' = $9)") {
		t.Fatalf("listSupplyChainImpactReadinessQuery still counts unrelated advisory facts for non-CVE scopes:\n%s", listSupplyChainImpactReadinessQuery)
	}
}

type rejectingSupplyChainImpactReadinessQueryer struct{ called int }

func (r *rejectingSupplyChainImpactReadinessQueryer) QueryContext(
	_ context.Context,
	_ string,
	_ ...any,
) (*sql.Rows, error) {
	r.called++
	return nil, fmt.Errorf("Postgres must not be queried for derived-only readiness")
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

func TestPostgresSupplyChainImpactReadinessSkipsAdvisoryOnlyScope(t *testing.T) {
	t.Parallel()

	db := &rejectingSupplyChainImpactReadinessQueryer{}
	store := NewPostgresSupplyChainImpactReadinessStore(db)
	snapshot, err := store.ReadSupplyChainImpactReadiness(
		context.Background(),
		SupplyChainImpactReadinessQuery{AdvisoryID: "GHSA-aaaa-bbbb-cccc"},
	)
	if err != nil {
		t.Fatalf("ReadSupplyChainImpactReadiness() error = %v, want nil", err)
	}
	if db.called != 0 {
		t.Fatalf("QueryContext invocations = %d, want 0 for advisory-only scope", db.called)
	}
	if len(snapshot.EvidenceSources) != 0 || snapshot.TargetIncomplete {
		t.Fatalf("snapshot = %#v, want empty for advisory-only scope", snapshot)
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

func TestPostgresSupplyChainImpactReadinessScansForImageRefScope(t *testing.T) {
	t.Parallel()

	db := &countingSupplyChainImpactReadinessQueryer{}
	store := NewPostgresSupplyChainImpactReadinessStore(db)
	_, _ = store.ReadSupplyChainImpactReadiness(
		context.Background(),
		SupplyChainImpactReadinessQuery{ImageRef: "registry.example.com/team/api:prod"},
	)
	if db.called != 1 {
		t.Fatalf("QueryContext invocations = %d, want 1 for image_ref scope", db.called)
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
