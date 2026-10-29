// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9227 F7 disposable PostgreSQL API proof that repository, CVE, and normalized package requests join derived scopes through collector-written package keys, and that legacy states without keys read as unmatched
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/vulnerabilityintelligence/vulnruntime"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// seedDerivedFacts activates one fact scope and writes one fact per payload.
func seedDerivedFacts(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, scope, kind string, payloads []map[string]any) {
	t.Helper()
	generation := scope + "-generation"
	if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes
        (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
        VALUES ($1, 'vulnerability_intelligence', 'vulnerability_intelligence', $1, 'vulnerability_intelligence', $1, $2, $2, 'active', $3)`,
		scope, now, generation); err != nil {
		t.Fatalf("seed fact scope %s: %v", scope, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations
        (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
        VALUES ($1, $2, 'schedule', $3, $3, 'active')`, scope, generation, now); err != nil {
		t.Fatalf("seed fact generation %s: %v", scope, err)
	}
	for index, payload := range payloads {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal fact payload: %v", err)
		}
		factID := fmt.Sprintf("%s-%d", scope, index)
		if _, err := db.ExecContext(ctx, `INSERT INTO fact_records
            (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
             source_fact_key, observed_at, ingested_at, is_tombstone, payload)
            VALUES ($1, $2, $3, $4, $1, 'test', $1, $5, $5, FALSE, $6)`,
			factID, scope, generation, kind, now, encoded); err != nil {
			t.Fatalf("seed fact %s: %v", factID, err)
		}
	}
}

// collectorPackageKeys derives a scope's keys with the collector's own
// parser and normalizer, so these cases exercise the real write contract.
func collectorPackageKeys(t *testing.T, scopeID string, ecosystems ...string) [][2]string {
	t.Helper()
	keys, ok := vulnruntime.DerivedTargetPackageKeys(scopeID, vulnruntime.DerivedTargetConfig{
		Enabled: true, Sources: []vulnruntime.Source{vulnruntime.SourceOSV}, Ecosystems: ecosystems,
	})
	if !ok {
		t.Fatalf("collector rejected scope id %q", scopeID)
	}
	out := make([][2]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, [2]string{key.Ecosystem, key.PackageName})
	}
	return out
}

func TestSupplyChainImpactReadinessDerivedRepositoryAcrossBatchScopesLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-npm", "npm")
	consumptions := make([]map[string]any, 0, 300)
	for index := range 300 {
		name := fmt.Sprintf("repo-pkg-%03d", index)
		consumptions = append(consumptions, map[string]any{
			"package_id": "npm://registry.npmjs.org/" + name, "ecosystem": "npm",
			"package_name": name, "repository_id": "repository:r_9227",
		})
		freshness, terminal := "fresh", "succeeded"
		if index == 150 {
			freshness, terminal = "failed", "failed_retryable"
		}
		scope := fmt.Sprintf("vuln-intel://osv/npm/_batch?query=%s%%401.0.0", name)
		seedDerivedScope(t, ctx, db, now, "derived-npm", scope, "npm", freshness, terminal, [][2]string{{"npm", name}})
	}
	seedDerivedFacts(t, ctx, db, now, "consumption-scope", "reducer_package_consumption_correlation", consumptions)

	readiness := readDerivedReadiness(t, db, "repository_id=repository:r_9227")
	if readiness.State == string(impact.ReadinessStateReadinessUnavailable) {
		t.Fatalf("repository readiness across 300 derived scopes unavailable: %+v", readiness)
	}
	state := onlyDerivedState(t, readiness)
	if state.ScopeCount != 300 || state.PackageCount != 300 || state.UnmatchedPackageCount != 0 ||
		state.FreshnessState != "failed" || state.FailedCount != 1 || state.FreshCount != 299 ||
		len(state.ScopeIDs) != 10 {
		t.Fatalf("repository derived aggregate = %+v, want 300 scopes, worst failed", state)
	}
}

func TestSupplyChainImpactReadinessDerivedKeyNormalizationLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-mixed", "npm", "pypi", "maven", "go")
	scopes := []struct{ scope, ecosystem string }{
		{"vuln-intel://osv/pypi/Django_REST?version=3.0.0", "pypi"},
		{"vuln-intel://osv/npm/@Example%2FName?version=1.0.0", "npm"},
		{"vuln-intel://osv/maven/org.apache.logging.log4j:log4j-core?version=2.17.1", "maven"},
		{"vuln-intel://osv/go/golang.org%2Fx%2Fnet?version=v0.23.0", "go"},
	}
	for _, entry := range scopes {
		seedDerivedScope(t, ctx, db, now, "derived-mixed", entry.scope, entry.ecosystem, "fresh", "succeeded",
			collectorPackageKeys(t, entry.scope, entry.ecosystem))
	}
	// OSV-style affected packages: ecosystem plus the request-side spelling,
	// no package id, so readiness derives keys from the default registry;
	// maven names need a group namespace, so that row carries a PURL.
	seedDerivedFacts(t, ctx, db, now, "affected-scope", "vulnerability.affected_package", []map[string]any{
		{"cve_id": "CVE-2026-9301", "ecosystem": "PyPI", "package_name": "django-rest"},
		{"cve_id": "CVE-2026-9301", "ecosystem": "npm", "package_name": "@example/name"},
		{"cve_id": "CVE-2026-9301", "purl": "pkg:maven/org.apache.logging.log4j/log4j-core@2.17.1"},
		{"cve_id": "CVE-2026-9301", "ecosystem": "Go", "package_name": "golang.org/x/net"},
	})

	readiness := readDerivedReadiness(t, db, "cve_id=CVE-2026-9301")
	got := map[string]derivedStateView{}
	for _, state := range readiness.SourceStates {
		if state.Derived {
			got[state.Ecosystem] = state
		}
	}
	for _, ecosystem := range []string{"pypi", "npm", "maven", "go"} {
		state, ok := got[ecosystem]
		if !ok || state.ScopeCount != 1 || state.UnmatchedPackageCount != 0 || state.FreshnessState != "fresh" {
			t.Fatalf("%s derived aggregate = %+v (found %v), want one matched fresh scope; all = %+v",
				ecosystem, state, ok, readiness.SourceStates)
		}
	}
	if len(got) != 4 {
		t.Fatalf("derived aggregates = %+v, want exactly four", got)
	}
}

func TestSupplyChainImpactReadinessDerivedLegacyStateWithoutKeysLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-npm", "npm")
	seedDerivedScope(t, ctx, db, now, "derived-npm", lodashScope, "npm", "fresh", "succeeded", nil)

	readiness := readDerivedReadiness(t, db, "package_id=npm://registry.npmjs.org/lodash")
	state := onlyDerivedState(t, readiness)
	if state.ScopeCount != 0 || state.UnmatchedPackageCount != 1 || state.FreshnessState != "pending" ||
		readiness.State != string(impact.ReadinessStateTargetIncomplete) {
		t.Fatalf("legacy keyless state readiness = %+v, want unmatched pending target_incomplete", readiness)
	}
}

func TestSupplyChainImpactReadinessDerivedCVEAffectedPackagesDoNotOverflowLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-npm", "npm")
	seedUnrelatedDerivedScopes(t, ctx, db, now, "derived-npm", 201)
	seedDerivedScope(t, ctx, db, now, "derived-npm", lodashScope, "npm", "fresh", "succeeded", lodashKeys)
	seedDerivedFacts(t, ctx, db, now, "affected-scope", "vulnerability.affected_package", []map[string]any{
		{"cve_id": "CVE-2026-9302", "package_id": "npm://registry.npmjs.org/lodash", "ecosystem": "npm", "package_name": "lodash"},
	})

	readiness := readDerivedReadiness(t, db, "cve_id=CVE-2026-9302")
	if readiness.State == string(impact.ReadinessStateReadinessUnavailable) {
		t.Fatalf("CVE readiness with 201 unrelated derived scopes unavailable: %+v", readiness)
	}
	state := onlyDerivedState(t, readiness)
	if state.ScopeCount != 1 || state.FreshnessState != "fresh" {
		t.Fatalf("CVE derived aggregate = %+v, want one fresh lodash scope", state)
	}
}
