// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"slices"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

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
