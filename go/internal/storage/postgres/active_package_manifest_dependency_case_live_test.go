// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9568 disposable PostgreSQL 18 proof that ListActivePackageManifestDependencyFacts loads manifest rows whose stored entity_name keeps registry case (NuGet Newtonsoft.Json, pypi PyYAML, rubygems Sidekiq, composer Laravel/Tinker) from lowercase advisory candidate names derived through packageidentity, on both the direct and the Maven-family (sbt) query paths, while tombstoned and other-ecosystem same-name rows stay out; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

type mixedCaseLiveFact struct {
	id         string
	name       string
	manager    string
	tombstone  bool
	repository string
}

func seedMixedCaseLiveFact(t *testing.T, ctx context.Context, database *sql.DB, fact mixedCaseLiveFact) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"repo_id":         fact.repository,
		"entity_type":     "Variable",
		"entity_name":     fact.name,
		"entity_metadata": map[string]any{"config_kind": "dependency", "package_manager": fact.manager},
	})
	if err != nil {
		t.Fatalf("marshal %s: %v", fact.id, err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, 'sidecar-live-scope', 'sidecar-live-generation', 'content_entity', $1,
        'git', $1, clock_timestamp(), clock_timestamp(), $2, $3::jsonb)`, fact.id, fact.tombstone, string(payload)); err != nil {
		t.Fatalf("seed %s: %v", fact.id, err)
	}
}

func mixedCaseCandidateNames(ecosystem packageidentity.Ecosystem, advisoryNames ...string) []string {
	var names []string
	for _, advisory := range advisoryNames {
		names = append(names, packageidentity.ManifestEntityNameCandidates(ecosystem, advisory)...)
	}
	seen := map[string]struct{}{}
	out := names[:0]
	sort.Strings(names)
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// TestListActivePackageManifestDependencyFactsLoadsMixedCaseNamesLive is the
// #9568 RED test: lowercase advisory candidate names derived through
// packageidentity must load the registry-cased manifest rows the exact
// entity_name match misses.
func TestListActivePackageManifestDependencyFactsLoadsMixedCaseNamesLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedPackageManifestConsumptionBackfillScope(t, ctx, database)
	if _, err := database.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = 'sidecar-live-generation'
WHERE scope_id = 'sidecar-live-scope'`); err != nil {
		t.Fatalf("activate generation: %v", err)
	}

	const repo = "repository:sidecar-live"
	for _, fact := range []mixedCaseLiveFact{
		{id: "nuget-newtonsoft", name: "Newtonsoft.Json", manager: "nuget", repository: repo},
		{id: "nuget-serilog", name: "Serilog", manager: "nuget", repository: repo},
		{id: "pypi-pyyaml", name: "PyYAML", manager: "pypi", repository: repo},
		{id: "pypi-flask", name: "Flask", manager: "pypi", repository: repo},
		{id: "gem-sidekiq", name: "Sidekiq", manager: "rubygems", repository: repo},
		{id: "composer-tinker", name: "Laravel/Tinker", manager: "composer", repository: repo},
		{id: "npm-leftpad", name: "left-pad", manager: "npm", repository: repo},
		{id: "npm-newtonsoft", name: "Newtonsoft.Json", manager: "npm", repository: repo},
		{id: "nuget-tombstone", name: "Newtonsoft.Json", manager: "nuget", tombstone: true, repository: repo},
	} {
		seedMixedCaseLiveFact(t, ctx, database, fact)
	}

	var names []string
	names = append(names, mixedCaseCandidateNames(packageidentity.EcosystemNuGet, "newtonsoft.json", "serilog")...)
	names = append(names, mixedCaseCandidateNames(packageidentity.EcosystemPyPI, "pyyaml", "flask")...)
	names = append(names, mixedCaseCandidateNames(packageidentity.EcosystemRubyGems, "sidekiq")...)
	names = append(names, mixedCaseCandidateNames(packageidentity.EcosystemComposer, "laravel/tinker")...)
	loaded, err := NewFactStore(SQLDB{DB: database}).ListActivePackageManifestDependencyFacts(
		ctx, []string{"nuget", "pypi", "rubygems", "composer"}, names,
	)
	if err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts(): %v", err)
	}
	var got []string
	for _, envelope := range loaded {
		got = append(got, envelope.FactID)
	}
	sort.Strings(got)
	want := []string{
		"composer-tinker", "gem-sidekiq", "nuget-newtonsoft", "nuget-serilog", "pypi-flask", "pypi-pyyaml",
	}
	if len(got) != len(want) {
		t.Fatalf("loaded facts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("loaded facts = %v, want %v", got, want)
		}
	}
}

// TestListActivePackageManifestDependencyFactsLoadsMixedCaseNamesThroughSBTQueryLive
// pins the mixed-batch hole: when a Maven advisory shares the handler batch,
// the Maven-family query serves every ecosystem, so its entity_name prefilter
// must fold case too or the NuGet row the direct query loads goes missing.
func TestListActivePackageManifestDependencyFactsLoadsMixedCaseNamesThroughSBTQueryLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedPackageManifestConsumptionBackfillScope(t, ctx, database)
	if _, err := database.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = 'sidecar-live-generation'
WHERE scope_id = 'sidecar-live-scope'`); err != nil {
		t.Fatalf("activate generation: %v", err)
	}

	const repo = "repository:sidecar-live"
	for _, fact := range []mixedCaseLiveFact{
		{id: "nuget-newtonsoft", name: "Newtonsoft.Json", manager: "nuget", repository: repo},
		{id: "gradle-lang", name: "org.apache.commons:commons-lang3", manager: "gradle", repository: repo},
	} {
		seedMixedCaseLiveFact(t, ctx, database, fact)
	}

	names := mixedCaseCandidateNames(packageidentity.EcosystemNuGet, "newtonsoft.json")
	names = append(names, mixedCaseCandidateNames(packageidentity.EcosystemMaven, "org.apache.commons/commons-lang3")...)
	loaded, err := NewFactStore(SQLDB{DB: database}).ListActivePackageManifestDependencyFacts(
		ctx, []string{"maven", "nuget"}, names,
	)
	if err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts(): %v", err)
	}
	var got []string
	for _, envelope := range loaded {
		got = append(got, envelope.FactID)
	}
	sort.Strings(got)
	want := []string{"gradle-lang", "nuget-newtonsoft"}
	if len(got) != len(want) {
		t.Fatalf("loaded facts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("loaded facts = %v, want %v", got, want)
		}
	}
}
