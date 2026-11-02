// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9777 PostgreSQL 18.6 proof that the stored-name read returns the names Eshu holds for an advisory id from active generations only, so a record that dropped a name still re-queries the old name; isolated bootstrap schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"testing"
	"time"

	vuln "github.com/eshu-hq/eshu/go/internal/collector/vulnerabilityintelligence"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// storedNamesGenerations lays out three advisory scopes. scope-adv-a holds an
// active generation (gen-a2) that replaced a superseded one (gen-a1).
// scope-adv-b is a separate active scope. scope-adv-c separates the two
// conditions that define an active generation: gen-c1 has the active status but
// the scope pointer does not name it, and the pointer names gen-c2, which is
// still pending. A fact in either one is not active truth.
const storedNamesGenerations = `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
     observed_at, ingested_at, status, active_generation_id)
VALUES
  ('scope-adv-a', 'advisory', 'osv', 'osv-a', 'vulnerability_intelligence', 'advisory:a', now(), now(), 'active', 'gen-a2'),
  ('scope-adv-b', 'advisory', 'osv', 'osv-b', 'vulnerability_intelligence', 'advisory:b', now(), now(), 'active', 'gen-b1'),
  ('scope-adv-c', 'advisory', 'osv', 'osv-c', 'vulnerability_intelligence', 'advisory:c', now(), now(), 'active', 'gen-c2');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES
  ('gen-a1', 'scope-adv-a', 'snapshot', now() - interval '2 hours', now(), 'superseded'),
  ('gen-a2', 'scope-adv-a', 'snapshot', now() - interval '1 hour', now(), 'active'),
  ('gen-b1', 'scope-adv-b', 'snapshot', now(), now(), 'active'),
  ('gen-c1', 'scope-adv-c', 'snapshot', now() - interval '1 hour', now(), 'active'),
  ('gen-c2', 'scope-adv-c', 'snapshot', now(), now(), 'pending');
`

// storedNamesGuardRows are rows the OSV collector never writes. They are here
// only to pin the read's guards: each one states the guard it exercises. Every
// other fixture row comes from seedOSVRecord, so its shape is the producer's.
const storedNamesGuardRows = `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT f.fact_id, 'scope-adv-b', 'gen-b1', f.fact_kind, 'stable-' || f.fact_id, 'vulnerability_intelligence',
    'osv', f.fact_id, now(), now(), FALSE, f.payload
FROM (VALUES
  -- Another kind that carries the same keys is not an affected package.
  ('f-guard-kind',  'vulnerability.cve',              '{"advisory_id":"ADV-GUARD","ecosystem":"npm","package_name":"cve-kind-name"}'::jsonb),
  -- No usable pair: skipped, not returned as a blank.
  ('f-guard-blank', 'vulnerability.affected_package', '{"advisory_id":"ADV-GUARD","ecosystem":"npm","package_name":"  "}'::jsonb),
  ('f-guard-noeco', 'vulnerability.affected_package', '{"advisory_id":"ADV-GUARD","package_name":"no-ecosystem"}'::jsonb),
  -- Ecosystem case and padding are folded; a name and id are only trimmed.
  ('f-guard-case',  'vulnerability.affected_package', '{"advisory_id":"ADV-GUARD","ecosystem":" NPM ","package_name":" Spaced ","package_id":" npm://registry.npmjs.org/Spaced "}'::jsonb),
  -- A row with no package_id still names a package.
  ('f-guard-noid',  'vulnerability.affected_package', '{"advisory_id":"ADV-GUARD","ecosystem":"npm","package_name":"no-id"}'::jsonb)
) AS f(fact_id, fact_kind, payload);
`

func osvPackage(ecosystem, name string) vuln.OSVAffected {
	return vuln.OSVAffected{Package: vuln.OSVPackage{Ecosystem: ecosystem, Name: name}}
}

// seedOSVRecord writes one OSV record through the real collector and fact
// store, so the stored payload is the producer's shape by construction.
func seedOSVRecord(
	ctx context.Context,
	t *testing.T,
	store *FactStore,
	scopeID, generationID string,
	tombstone bool,
	id string,
	affected ...vuln.OSVAffected,
) {
	t.Helper()
	envelopes, err := vuln.OSVRecordEnvelopes(vuln.OSVEnvelopeContext{
		ScopeID:             scopeID,
		GenerationID:        generationID,
		CollectorInstanceID: "osv-test",
		ObservedAt:          time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		SourceURI:           "https://api.osv.dev/v1/vulns/" + id,
	}, vuln.OSVVulnerability{ID: id, Affected: affected})
	if err != nil {
		t.Fatalf("OSVRecordEnvelopes(%s) error = %v", id, err)
	}
	for i := range envelopes {
		envelopes[i].IsTombstone = tombstone
	}
	if err := store.UpsertFacts(ctx, envelopes); err != nil {
		t.Fatalf("UpsertFacts(%s) error = %v", id, err)
	}
}

// seedStoredNamesFixture seeds the generations, the producer-shaped records and
// the guard rows. Each call states the behavior it exercises.
func seedStoredNamesFixture(ctx context.Context, t *testing.T, db *sql.DB, store *FactStore) {
	t.Helper()
	if _, err := db.ExecContext(ctx, storedNamesGenerations); err != nil {
		t.Fatalf("seed stored-name generations: %v", err)
	}
	// ADV-DROP is the record that dropped a name: the active generation still
	// stores both the old name and the kept one.
	seedOSVRecord(ctx, t, store, "scope-adv-a", "gen-a2", false, "ADV-DROP",
		osvPackage("npm", "old-name"), osvPackage("npm", "kept-name"))
	// The same package in a second scope is one row.
	seedOSVRecord(ctx, t, store, "scope-adv-b", "gen-b1", false, "ADV-DROP", osvPackage("npm", "kept-name"))
	// A superseded generation's name is history, not stored truth.
	seedOSVRecord(ctx, t, store, "scope-adv-a", "gen-a1", false, "ADV-DROP", osvPackage("npm", "ancient-name"))
	// A tombstone is a deletion.
	seedOSVRecord(ctx, t, store, "scope-adv-a", "gen-a2", true, "ADV-TOMB", osvPackage("npm", "tombstoned-name"))
	// Active status without the scope pointer, and the pointer without active
	// status, are both not active truth.
	seedOSVRecord(ctx, t, store, "scope-adv-c", "gen-c1", false, "ADV-DROP", osvPackage("npm", "unpointed-name"))
	seedOSVRecord(ctx, t, store, "scope-adv-c", "gen-c2", false, "ADV-DROP", osvPackage("npm", "unactivated-name"))
	// ADV-TWO carries the Maven and Swift packages whose stored name is not the
	// consumer key. The two Maven packages share an artifact and differ only in
	// the group, so only PackageID tells them apart. The Hex row keeps a
	// namespace in its purl only to pin that PackageName stays the bare name;
	// the feed carries no Hex namespace today.
	seedOSVRecord(ctx, t, store, "scope-adv-b", "gen-b1", false, "ADV-TWO",
		osvPackage("PyPI", "Some_Pkg"),
		osvPackage("Maven", "org.example:lib"),
		osvPackage("Maven", "com.other:lib"),
		osvPackage("Go", "github.com/Acme/Mod"),
		vuln.OSVAffected{Package: vuln.OSVPackage{
			Ecosystem: "SwiftURL", Name: "github.com/apple/swift-nio", PURL: "pkg:swift/github.com/apple/swift-nio",
		}},
		vuln.OSVAffected{Package: vuln.OSVPackage{Ecosystem: "Hex", Name: "plug", PURL: "pkg:hex/acme/plug"}})
	// An advisory the caller does not ask about.
	seedOSVRecord(ctx, t, store, "scope-adv-b", "gen-b1", false, "ADV-OTHER", osvPackage("npm", "other-name"))
	if _, err := db.ExecContext(ctx, storedNamesGuardRows); err != nil {
		t.Fatalf("seed stored-name guard rows: %v", err)
	}
}

func TestListStoredAdvisoryPackageNamesReturnsDroppedNameLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "stored_names_9777", "ESHU_POSTGRES_DSN is not set")
	store := NewFactStore(SQLDB{DB: db})
	seedStoredNamesFixture(ctx, t, db, store)

	got, err := store.ListStoredAdvisoryPackageNames(ctx,
		[]string{"ADV-DROP", "ADV-TOMB", "ADV-TWO", "ADV-GUARD", "ADV-ABSENT", "", "ADV-DROP"})
	if err != nil {
		t.Fatalf("ListStoredAdvisoryPackageNames() error = %v", err)
	}
	want := map[string][]StoredAdvisoryPackageName{
		"ADV-DROP": {
			{Ecosystem: "npm", PackageName: "kept-name", PackageID: "npm://registry.npmjs.org/kept-name"},
			{Ecosystem: "npm", PackageName: "old-name", PackageID: "npm://registry.npmjs.org/old-name"},
		},
		"ADV-TWO": {
			{Ecosystem: "gomod", PackageName: "github.com/Acme/Mod", PackageID: "gomod://proxy.golang.org/github.com/Acme/Mod"},
			// The OSV producer has no default Hex registry, so the id says unknown.
			{Ecosystem: "hex", PackageName: "plug", PackageID: "hex://unknown/acme/plug"},
			{Ecosystem: "maven", PackageName: "lib", PackageID: "maven://repo.maven.apache.org/maven2/com.other:lib"},
			{Ecosystem: "maven", PackageName: "lib", PackageID: "maven://repo.maven.apache.org/maven2/org.example:lib"},
			{Ecosystem: "pypi", PackageName: "some-pkg", PackageID: "pypi://pypi.org/simple/some-pkg"},
			{Ecosystem: "swift", PackageName: "swift-nio", PackageID: "swift://github.com/apple/swift-nio"},
		},
		"ADV-GUARD": {
			{Ecosystem: "npm", PackageName: "Spaced", PackageID: "npm://registry.npmjs.org/Spaced"},
			{Ecosystem: "npm", PackageName: "no-id"},
		},
	}
	if len(got) != len(want) {
		t.Fatalf("advisory ids resolved = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for id, wantNames := range want {
		if !slices.Equal(got[id], wantNames) {
			t.Errorf("names for %s = %v, want %v", id, got[id], wantNames)
		}
	}
	// The read returns the packageidentity spelling; the alias table maps it to
	// the owned derivation spelling, with no second table.
	if spelled := workflow.CanonicalOwnedPackageEcosystem(got["ADV-TWO"][0].Ecosystem); spelled != "go" {
		t.Errorf("owned spelling of %q = %q, want go", got["ADV-TWO"][0].Ecosystem, spelled)
	}
}

// TestListStoredAdvisoryPackageNamesChunksTheIDListLive pins that a list longer
// than one chunk returns exactly what one statement would: the chunk size is a
// planning bound, never a result boundary.
func TestListStoredAdvisoryPackageNamesChunksTheIDListLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "stored_names_chunk_9777", "ESHU_POSTGRES_DSN is not set")
	store := NewFactStore(SQLDB{DB: db})
	seedStoredNamesFixture(ctx, t, db, store)
	seedManyStoredNames(ctx, t, db, 2*storedAdvisoryNameChunkSize+7)

	ids := make([]string, 0, 2*storedAdvisoryNameChunkSize+9)
	ids = append(ids, "ADV-DROP", "ADV-TWO")
	for n := range 2*storedAdvisoryNameChunkSize + 7 {
		ids = append(ids, fmt.Sprintf("ADV-MANY-%04d", n))
	}
	got, err := store.ListStoredAdvisoryPackageNames(ctx, ids)
	if err != nil {
		t.Fatalf("ListStoredAdvisoryPackageNames() error = %v", err)
	}
	if want := len(ids); len(got) != want {
		t.Fatalf("advisory ids resolved = %d, want %d", len(got), want)
	}
	last := fmt.Sprintf("ADV-MANY-%04d", 2*storedAdvisoryNameChunkSize+6)
	name := "many-" + last
	if want := []StoredAdvisoryPackageName{
		{Ecosystem: "npm", PackageName: name, PackageID: "npm://registry.npmjs.org/" + name},
	}; !slices.Equal(got[last], want) {
		t.Errorf("names for %s = %v, want %v", last, got[last], want)
	}
	if len(got["ADV-DROP"]) != 2 {
		t.Errorf("names for ADV-DROP = %v, want 2 names across chunk boundaries", got["ADV-DROP"])
	}
}

func seedManyStoredNames(ctx context.Context, t *testing.T, db *sql.DB, count int) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'f-many-' || n, 'scope-adv-b', 'gen-b1', 'vulnerability.affected_package', 'stable-many-' || n,
    'vulnerability_intelligence', 'osv', 'f-many-' || n, now(), now(), FALSE,
    jsonb_build_object('advisory_id', 'ADV-MANY-' || lpad(n::text, 4, '0'), 'ecosystem', 'npm',
        'package_name', 'many-ADV-MANY-' || lpad(n::text, 4, '0'),
        'package_id', 'npm://registry.npmjs.org/many-ADV-MANY-' || lpad(n::text, 4, '0'))
FROM generate_series(0, $1::int - 1) AS n`, count); err != nil {
		t.Fatalf("seed many stored names: %v", err)
	}
}
