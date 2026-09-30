// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func cappedPackageIDs(count int) []string {
	ids := make([]string, 0, count)
	for n := range count {
		ids = append(ids, fmt.Sprintf("pkg:deb/debian/pkg-%04d", n))
	}
	return ids
}

// TestListOSPackageAdvisoryFactEnvelopesNarrowedPagesToCompletionLive proves the
// narrowed reader against real Postgres: 1203 installed targets requested by
// package id are returned once each across three pages, a narrower key set
// returns only its own rows, and rows of an inactive generation are read past
// but never returned.
func TestListOSPackageAdvisoryFactEnvelopesNarrowedPagesToCompletionLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	const targets = 1203
	seedCappedScanScope(t, ctx, db, targets)
	// Rows of a superseded generation: candidates by package id, not active.
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ('generation:7154:scan-old', $1, 'synthetic', $2, $2, 'superseded', $2, '{}'::jsonb)`,
		cappedScanScope, replaceSetLiveNow); err != nil {
		t.Fatalf("insert old generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload, fencing_token)
SELECT 'os-package:7154:old-' || lpad(n::text, 6, '0'), $1, 'generation:7154:scan-old', 'vulnerability.os_package',
  'old-' || n, 'synthetic', 'old-' || n, $2::timestamptz, $2::timestamptz, FALSE,
  jsonb_build_object('distro','debian','distro_version','12','package_manager','dpkg','name','pkg-'||lpad(n::text,4,'0'),
    'arch','amd64','repository_class','vendor','vendor_advisory_source','debian','installed_version_raw','0.9-1',
    'purl','pkg:deb/debian/pkg-'||lpad(n::text,4,'0')||'@0.9-1'), 0
FROM generate_series(0, 599) AS n`, cappedScanScope, replaceSetLiveNow); err != nil {
		t.Fatalf("insert inactive rows: %v", err)
	}
	store := postgres.NewFactStore(postgres.SQLDB{DB: db})

	envelopes, skipped, truncated, err := store.ListOSPackageAdvisoryFactEnvelopes(ctx, []string{"debian"}, cappedPackageIDs(targets), 1_000_000)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(envelopes) != targets || skipped != 0 || truncated {
		t.Fatalf("got %d envelopes, %d skipped, truncated=%v; want %d/0/false", len(envelopes), skipped, truncated, targets)
	}
	seen := map[string]bool{}
	for _, envelope := range envelopes {
		if seen[envelope.FactID] {
			t.Fatalf("fact id %s returned twice", envelope.FactID)
		}
		seen[envelope.FactID] = true
		if strings.Contains(envelope.FactID, "-old-") {
			t.Fatalf("inactive-generation row %s returned", envelope.FactID)
		}
	}
	for n := range targets {
		if id := fmt.Sprintf("os-package:7154:%06d", n); !seen[id] {
			t.Fatalf("active fact id %s never returned", id)
		}
	}

	few, _, _, err := store.ListOSPackageAdvisoryFactEnvelopes(ctx, []string{"debian"},
		[]string{"pkg:deb/debian/pkg-0007", "pkg:deb/debian/pkg-1100", "pkg:deb/debian/does-not-exist"}, 1_000_000)
	if err != nil || len(few) != 2 {
		t.Fatalf("narrow read = %d envelopes, err %v; want the 2 installed matches", len(few), err)
	}

	limited, _, truncated, err := store.ListOSPackageAdvisoryFactEnvelopes(ctx, []string{"debian"}, cappedPackageIDs(targets), 100)
	if err != nil || !truncated || len(limited) != 500 {
		t.Fatalf("limited read = %d envelopes, truncated=%v, err=%v; want the crossing page of 500 and truncated", len(limited), truncated, err)
	}
	other, _, _, err := store.ListOSPackageAdvisoryFactEnvelopes(ctx, []string{"alpine"}, cappedPackageIDs(targets), 1_000_000)
	if err != nil || len(other) != 0 {
		t.Fatalf("a different ecosystem returned %d envelopes, err %v; want 0", len(other), err)
	}
}

// TestListOSPackageAdvisoryFactEnvelopesPURLFormsMatchTheGoMatcherLive proves
// the SQL package-id expression equals the Go matcher's derivation for every
// purl form: TrimSpace then cut at the first '@'. For each distinct Go key the
// rows the SQL read returns must be exactly the rows Go would key that way,
// including non-ASCII whitespace padding and forms with no version at all.
func TestListOSPackageAdvisoryFactEnvelopesPURLFormsMatchTheGoMatcherLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	seedCappedScanScope(t, ctx, db, 0)
	purls := map[string]string{
		"plain":          "pkg:deb/debian/openssl",
		"versioned":      "pkg:deb/debian/openssl@3.0.11-1~deb12u2",
		"qualified":      "pkg:deb/debian/openssl@3.0.11?arch=amd64&distro=debian-12",
		"no-version-q":   "pkg:deb/debian/openssl?arch=amd64",
		"space-padded":   "  pkg:deb/debian/openssl@1.0  ",
		"nbsp-padded":    "\u00a0pkg:deb/debian/openssl@1.0\u00a0",
		"tab-padded":     "\tpkg:deb/debian/openssl@1.0\n",
		"em-space":       "\u2003pkg:deb/debian/openssl\u2003",
		"ideographic":    "\u3000pkg:deb/debian/openssl@2\u3000",
		"next-line":      "\u0085pkg:deb/debian/openssl@2\u0085",
		"uppercase":      "pkg:deb/debian/OpenSSL@1.0",
		"other-package":  "pkg:deb/debian/zlib@1.2",
		"double-at":      "pkg:deb/debian/openssl@1@2",
		"leading-at":     "@1.0",
		"only-spaces":    "   ",
		"empty":          "",
		"apk":            "pkg:apk/alpine/openssl@3.1",
		"zero-width-nbs": "pkg:deb/debian/openssl\u200b@1",
	}
	names := make([]string, 0, len(purls))
	for name := range purls {
		names = append(names, name)
	}
	slices.Sort(names)
	goKey := func(purl string) string {
		prefix, _, _ := strings.Cut(strings.TrimSpace(purl), "@")
		return prefix
	}
	for _, name := range names {
		payload, err := json.Marshal(map[string]any{
			"distro": "debian", "distro_version": "12", "package_manager": "dpkg", "name": name, "arch": "amd64",
			"repository_class": "vendor", "vendor_advisory_source": "debian", "installed_version_raw": "1.0-1",
			"purl": purls[name],
		})
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload, fencing_token)
VALUES ($1, $2, $3, 'vulnerability.os_package', $1, 'synthetic', $1, $4, $4, FALSE, $5::jsonb, 0)`,
			"os-package:forms:"+name, cappedScanScope, cappedScanGeneration, replaceSetLiveNow, string(payload)); err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
	}
	// A row with no purl key at all.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload, fencing_token)
VALUES ('os-package:forms:no-purl', $1, $2, 'vulnerability.os_package', 'x', 'synthetic', 'x', $3, $3, FALSE,
  '{"distro":"debian","distro_version":"12","package_manager":"dpkg","name":"nopurl","arch":"amd64","repository_class":"vendor","vendor_advisory_source":"debian","installed_version_raw":"1.0"}'::jsonb, 0)`,
		cappedScanScope, cappedScanGeneration, replaceSetLiveNow); err != nil {
		t.Fatalf("insert no-purl row: %v", err)
	}

	store := postgres.NewFactStore(postgres.SQLDB{DB: db})
	keys := map[string][]string{}
	for _, name := range names {
		if key := goKey(purls[name]); key != "" {
			keys[key] = append(keys[key], "os-package:forms:"+name)
		}
	}
	if len(keys) < 5 {
		t.Fatalf("fixture yields only %d distinct Go keys; it no longer exercises the forms", len(keys))
	}
	for key, want := range keys {
		envelopes, _, _, err := store.ListOSPackageAdvisoryFactEnvelopes(ctx, []string{"debian"}, []string{key}, 1_000_000)
		if err != nil {
			t.Fatalf("key %q: %v", key, err)
		}
		got := make([]string, 0, len(envelopes))
		for _, envelope := range envelopes {
			got = append(got, envelope.FactID)
		}
		slices.Sort(got)
		slices.Sort(want)
		// The apk row has a debian vendor_advisory_source in this fixture, so it
		// is returned for its own key like any other.
		if !slices.Equal(got, want) {
			t.Fatalf("key %q: SQL returned %v, the Go matcher keys %v", key, got, want)
		}
	}
}

// TestOSPackageNarrowedQueryUsesTheIndexAtScaleLive proves the narrowed query
// is driven by fact_records_os_package_purl_prefix_idx at 50,000 rows rather
// than scanning every installed package, and reads few buffers for a small key
// set (60,164 per page for the ecosystem-only read it replaces).
func TestOSPackageNarrowedQueryUsesTheIndexAtScaleLive(t *testing.T) {
	ctx, db := openCappedScopeLiveDB(t)
	seedCappedScanScope(t, ctx, db, 50_000)
	// Production fact_records is dominated by other fact kinds. Without them the
	// generic plan sees a table of nothing but os_package rows and a primary-key
	// scan with a filter looks as cheap as the index, so seed the realistic mix.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload, fencing_token)
SELECT 'filler:7154:' || lpad(n::text, 7, '0'), $1, $2, 'repository', 'filler-' || n, 'synthetic', 'filler-' || n,
  $3::timestamptz, $3::timestamptz, FALSE, jsonb_build_object('n', n), 0
FROM generate_series(1, 300000) AS n`, cappedScanScope, cappedScanGeneration, replaceSetLiveNow); err != nil {
		t.Fatalf("seed filler rows: %v", err)
	}
	if _, err := db.ExecContext(ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	// pgx reaches a generic plan through its prepared-statement cache, so EXPLAIN a
	// prepared statement under force_generic_plan: bound parameters would show a
	// custom plan the reducer never runs.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `SET plan_cache_mode = force_generic_plan`); err != nil {
		t.Fatalf("force generic plan: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `PREPARE os_narrowed AS `+postgres.ListOSPackageAdvisoryTargetsForPackagesQueryForTest()); err != nil {
		t.Fatalf("prepare narrowed query: %v", err)
	}
	rows, err := conn.QueryContext(ctx,
		`EXPLAIN (ANALYZE, BUFFERS, COSTS OFF) EXECUTE os_narrowed('{pkg:deb/debian/pkg-0007,pkg:deb/debian/pkg-1100}', '', 500, '{debian}')`)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, line)
	}
	text := strings.Join(plan, "\n")
	if !strings.Contains(text, "fact_records_os_package_purl_prefix_idx") {
		t.Fatalf("the narrowed query does not use the expression index at 50,000 rows:\n%s", text)
	}
	if strings.Contains(text, "Seq Scan on fact_records") {
		t.Fatalf("the narrowed query scans fact_records sequentially:\n%s", text)
	}
}

// flipAfterFirstPageDB wraps a database so that after the first page statement
// of a read-only snapshot it commits a generation flip on another connection,
// the interleaving a collector run produces mid-drain.
type flipAfterFirstPageDB struct {
	postgres.SQLDB
	flip func()
	// statements counts page statements across every snapshot this database
	// opens, so the flip lands between page one and page two even if a reader
	// opened a snapshot per page.
	statements *int
}

func (d flipAfterFirstPageDB) BeginReadOnlyRepeatableRead(ctx context.Context) (db.Transaction, error) {
	tx, err := d.SQLDB.BeginReadOnlyRepeatableRead(ctx)
	if err != nil {
		return nil, err
	}
	return &flipAfterFirstPageTx{Transaction: tx, flip: d.flip, statements: d.statements}, nil
}

type flipAfterFirstPageTx struct {
	db.Transaction
	flip       func()
	statements *int
}

func (tx *flipAfterFirstPageTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	// The drain issues one statement per page and closes each page's rows before
	// the next statement, so committing the flip just before the second
	// statement is exactly "between page one and page two".
	if *tx.statements == 1 {
		tx.flip()
	}
	*tx.statements++
	return tx.Transaction.QueryContext(ctx, query, args...)
}

// TestListOSPackageAdvisoryFactEnvelopesGenerationFlipMidDrainLive is the #7154
// review F1 proof. An os_package fact id hashes its generation id, so when a
// scan scope's generation flips between page statements every still-installed
// package moves in the key space and a per-statement cursor skips some of them;
// the pass would then count as complete and retract their findings. The drain
// runs in one repeatable-read snapshot, so it returns exactly the pre-flip set.
func TestListOSPackageAdvisoryFactEnvelopesGenerationFlipMidDrainLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	const targets = 1203
	seedCappedScanScope(t, ctx, db, targets)

	flip := func() {
		// The collector's next run: the old generation is superseded, the same
		// packages land under a new generation with fact ids that sort
		// differently, and the scope switches to it.
		if _, err := db.ExecContext(context.Background(), `
UPDATE scope_generations SET status = 'superseded' WHERE scope_id = $1 AND generation_id = $2`,
			cappedScanScope, cappedScanGeneration); err != nil {
			t.Errorf("supersede old generation: %v", err)
			return
		}
		if _, err := db.ExecContext(context.Background(), `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ('generation:7154:scan-next', $1, 'synthetic', $2, $2, 'active', $2, '{}'::jsonb)`,
			cappedScanScope, replaceSetLiveNow); err != nil {
			t.Errorf("insert next generation: %v", err)
			return
		}
		if _, err := db.ExecContext(context.Background(), `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload, fencing_token)
SELECT 'a-next-' || fact_id, scope_id, 'generation:7154:scan-next', fact_kind,
  stable_fact_key || '-next', source_system, source_fact_key || '-next', observed_at, ingested_at, FALSE, payload, 0
FROM fact_records WHERE scope_id = $1 AND generation_id = $2`,
			cappedScanScope, cappedScanGeneration); err != nil {
			t.Errorf("copy facts into the next generation: %v", err)
			return
		}
		if _, err := db.ExecContext(context.Background(),
			`UPDATE ingestion_scopes SET active_generation_id = 'generation:7154:scan-next' WHERE scope_id = $1`,
			cappedScanScope); err != nil {
			t.Errorf("activate next generation: %v", err)
		}
	}
	store := postgres.NewFactStore(flipAfterFirstPageDB{SQLDB: postgres.SQLDB{DB: db}, flip: flip, statements: new(int)})
	envelopes, _, truncated, err := store.ListOSPackageAdvisoryFactEnvelopes(ctx, []string{"debian"}, cappedPackageIDs(targets), 1_000_000)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if truncated || len(envelopes) != targets {
		t.Fatalf("got %d envelopes (truncated=%v), want exactly the %d pre-flip targets; a per-statement cursor would skip some", len(envelopes), truncated, targets)
	}
	for _, envelope := range envelopes {
		if envelope.GenerationID != cappedScanGeneration {
			t.Fatalf("envelope %s from generation %s; the snapshot must read the pre-flip generation only", envelope.FactID, envelope.GenerationID)
		}
	}
	// After the flip a fresh drain reads the new generation.
	after, _, _, err := postgres.NewFactStore(postgres.SQLDB{DB: db}).ListOSPackageAdvisoryFactEnvelopes(ctx, []string{"debian"}, cappedPackageIDs(targets), 1_000_000)
	if err != nil || len(after) != targets || after[0].GenerationID != "generation:7154:scan-next" {
		t.Fatalf("post-flip drain = %d envelopes, err %v; want %d from the next generation", len(after), err, targets)
	}
}
