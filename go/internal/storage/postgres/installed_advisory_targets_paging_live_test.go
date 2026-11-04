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

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
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
	// The crossing rule is strict: a drain reports truncated only when it loaded
	// more than its limit. The core marks a pass partial from this flag alone,
	// so a reader that stopped at exactly the limit without flagging it would
	// let the pass retract findings it never read; a >= would instead cut the
	// drain a page early, which the length assertion below pins.
	crossing, _, crossed, err := store.ListOSPackageAdvisoryFactEnvelopes(ctx, []string{"debian"}, cappedPackageIDs(targets), 500)
	if err != nil || !crossed || len(crossing) != 1000 {
		t.Fatalf("limit 500 read = %d envelopes, truncated=%v, err=%v; want the page that crosses the limit (1000) and truncated", len(crossing), crossed, err)
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
//
// The #9859 source rows prove the SQL source expression equals the production
// Go helper for every source form: the oracle IS
// supplychaincore.InstalledOSPackageSourceKey, not a re-derivation, so the
// two engines can only agree by sharing the implementation's exact semantics.
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
		// #9859 source rows: binary names that differ from their source.
		"source-basic":     "pkg:deb/debian/libssl3@3.0.11",
		"source-qualified": "pkg:deb/debian/libssl3@3.0.11?arch=amd64&distro=debian-12",
		"source-uppercase": "pkg:deb/debian/libssl3@3.0.11",
		"source-space":     "pkg:deb/debian/libssl3@3.0.11",
		"source-double-at": "pkg:deb/debian/libssl3@1@2",
		"source-rejected":  "pkg:deb/debian/libssl3@3.0.11",
		"source-slash":     "pkg:deb/debian/libssl3@3.0.11",
		"source-empty":     "pkg:deb/debian/libssl3@3.0.11",
		"source-lookalike": "pkg:deb/debian/libssl3@3.0.11",
		// Both-arm row: the binary name equals the source name, so the row
		// is a candidate of the binary arm and the source arm at once. The
		// UNION dedupes it; a UNION ALL would return it twice and fail the
		// exact-set assertion below. It shares the "plain" row's binary key.
		"both-arm": "pkg:deb/debian/openssl@3.0.11",
	}
	sources := map[string]string{
		"both-arm":         "openssl",
		"source-basic":     "openssl",
		"source-qualified": "openssl",
		"source-uppercase": "OpenSSL",
		"source-space":     " open ssl ",
		"source-double-at": "openssl",
		"source-rejected":  "open?ssl",
		"source-slash":     "open/ssl",
		"source-empty":     "",
		"source-lookalike": "\u043epenssl",
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
		row := map[string]any{
			"distro": "debian", "distro_version": "12", "package_manager": "dpkg", "name": name, "arch": "amd64",
			"repository_class": "vendor", "vendor_advisory_source": "debian", "installed_version_raw": "1.0-1",
			"purl": purls[name],
		}
		if source, ok := sources[name]; ok && source != "" {
			row["source_package"] = source
		}
		payload, err := json.Marshal(row)
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
	addRow := func(name, key string) {
		if key == "" {
			return
		}
		keys[key] = append(keys[key], "os-package:forms:"+name)
		slices.Sort(keys[key])
		keys[key] = slices.Compact(keys[key])
	}
	for _, name := range names {
		addRow(name, goKey(purls[name]))
		// The source-key oracle is the production helper itself: any drift
		// between the SQL CASE and InstalledOSPackageSourceKey fails here.
		addRow(name, supplychaincore.InstalledOSPackageSourceKey(purls[name], sources[name]))
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

// TestListOSPackageAdvisoryFactEnvelopesMixedArmsPageToCompletionLive proves
// the UNION narrowed read (#9859) against real Postgres: 600 binary-only rows
// and 600 source-only rows with interleaved fact ids drain across three pages
// to exactly the active set, each row once, while inactive-generation source
// rows interleaved between them are read past and never returned.
//
// The key set is derived from the production oracle
// (supplychaincore.InstalledOSPackageSourceKey), never hand-copied, so the
// SQL arms can only agree with the implementation's exact semantics. Row 0 is
// a both-arm row (binary name equals its source name): both arms return it
// and the UNION still drains it exactly once.
func TestListOSPackageAdvisoryFactEnvelopesMixedArmsPageToCompletionLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	const targets = 1200
	seedCappedScanScope(t, ctx, db, targets)
	// Odd rows become source-only: a binary name no key queries, a source
	// name the key set does. Row 0 keeps its binary name and gains it as its
	// source too, so both arms match it. The 16-char 'os-package:7154:'
	// prefix puts the six digits at positions 17-22.
	if _, err := db.ExecContext(ctx, `
UPDATE fact_records SET payload = payload || jsonb_build_object(
  'purl', CASE WHEN (m.n % 2 = 1)
    THEN 'pkg:deb/debian/bin-' || lpad((m.n % 10000)::text, 4, '0') || '@1.0-1?arch=amd64'
    ELSE payload->>'purl' END,
  'source_package', CASE WHEN (m.n % 2 = 1)
    THEN 'src-' || lpad((m.n % 10000)::text, 4, '0')
    WHEN m.n = 0 THEN 'pkg-0000'
    ELSE NULL END)
FROM (SELECT fact_id, substr(fact_id, 17, 6)::integer AS n FROM fact_records
  WHERE fact_id LIKE 'os-package:7154:______' AND substr(fact_id, 17, 6) ~ '^[0-9]{6}$') AS m
WHERE fact_records.fact_id = m.fact_id`); err != nil {
		t.Fatalf("reshape odd rows to source-only: %v", err)
	}
	// Rows of a superseded generation interleaved between the actives: their
	// fact ids sort right after every sixth active row, so every page holds
	// some. They carry queried source keys, so they are candidates the drain
	// must read past, never return.
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ('generation:7154:scan-old', $1, 'synthetic', $2, $2, 'superseded', $2, '{}'::jsonb)`,
		cappedScanScope, replaceSetLiveNow); err != nil {
		t.Fatalf("insert old generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload, fencing_token)
SELECT 'os-package:7154:' || lpad(((n * 6) % 1200)::text, 6, '0') || '-old', $1, 'generation:7154:scan-old',
  'vulnerability.os_package', 'old-' || n, 'synthetic', 'old-' || n, $2::timestamptz, $2::timestamptz, FALSE,
  jsonb_build_object('distro','debian','distro_version','12','package_manager','dpkg',
    'name','bin-' || lpad(((n * 6 + 1) % 1200)::text, 4, '0'), 'arch','amd64','repository_class','vendor',
    'vendor_advisory_source','debian','installed_version_raw','9.9-1',
    'purl','pkg:deb/debian/bin-' || lpad(((n * 6 + 1) % 1200)::text, 4, '0') || '@9.9-1',
    'source_package','src-' || lpad(((n * 6 + 1) % 1200)::text, 4, '0')), 0
FROM generate_series(0, 199) AS n`, cappedScanScope, replaceSetLiveNow); err != nil {
		t.Fatalf("insert inactive source rows: %v", err)
	}
	// The key set from the production oracle: even rows by binary key, odd
	// rows by source key. Row 0's source key equals its binary key.
	var keys []string
	for n := range targets {
		if n%2 == 0 {
			keys = append(keys, fmt.Sprintf("pkg:deb/debian/pkg-%04d", n))
			continue
		}
		purl := fmt.Sprintf("pkg:deb/debian/bin-%04d@1.0-1?arch=amd64", n)
		source := fmt.Sprintf("src-%04d", n)
		key := supplychaincore.InstalledOSPackageSourceKey(purl, source)
		if key == "" {
			t.Fatalf("oracle rejects the seeded source key for row %d", n)
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)
	store := postgres.NewFactStore(postgres.SQLDB{DB: db})

	envelopes, skipped, truncated, err := store.ListOSPackageAdvisoryFactEnvelopes(ctx, []string{"debian"}, keys, 1_000_000)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(envelopes) != targets || skipped != 0 || truncated {
		t.Fatalf("got %d envelopes, %d skipped, truncated=%v; want %d/0/false", len(envelopes), skipped, truncated, targets)
	}
	seen := map[string]int{}
	for _, envelope := range envelopes {
		seen[envelope.FactID]++
		if skippedID := envelope.FactID; len(skippedID) > 4 && skippedID[len(skippedID)-4:] == "-old" {
			t.Fatalf("inactive-generation row %s returned", envelope.FactID)
		}
	}
	for n := range targets {
		if id := fmt.Sprintf("os-package:7154:%06d", n); seen[id] != 1 {
			t.Fatalf("active fact id %s returned %d times, want exactly once", id, seen[id])
		}
	}
	if len(seen) != targets {
		t.Fatalf("drain returned %d distinct fact ids, want %d", len(seen), targets)
	}
}
