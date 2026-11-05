// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9777 step 2 disposable PostgreSQL proof that the changed_name fact read returns the delta feed's rows from active generations only, with the raw feed spelling and the verbatim withdrawn spelling (the intersection strict-parses it); isolated bootstrap schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"
)

// changedNamesGenerations lays out one advisory scope with an active
// generation that replaced a superseded one, plus a pending-generation
// scope that must never read as change truth.
const changedNamesGenerations = `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
     observed_at, ingested_at, status, active_generation_id)
VALUES
  ('scope-delta-a', 'advisory', 'osv-delta', 'osv-delta-a', 'vulnerability_intelligence', 'advisory:delta-a', now(), now(), 'active', 'gen-delta-a2'),
  ('scope-delta-b', 'advisory', 'osv-delta', 'osv-delta-b', 'vulnerability_intelligence', 'advisory:delta-b', now(), now(), 'active', NULL);
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES
  ('gen-delta-a1', 'scope-delta-a', 'snapshot', now() - interval '2 hours', now(), 'superseded'),
  ('gen-delta-a2', 'scope-delta-a', 'snapshot', now() - interval '1 hour', now(), 'active'),
  ('gen-delta-b1', 'scope-delta-b', 'snapshot', now(), now(), 'pending');
`

type changedRow struct {
	scope, generation, factID, advisory, ecosystem, name, withdrawn, feedTS string
	tombstone                                                               bool
}

// seedChangedRows inserts changed_name fact rows with the satellite's
// payload shape, plus the guard rows the test names.
func seedChangedRows(t *testing.T, ctx context.Context, db *sql.DB, rows ...changedRow) {
	t.Helper()
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		kind := "vulnerability.changed_name"
		payload := changedPayload(row.advisory, row.ecosystem, row.name, row.withdrawn, row.feedTS)
		if row.factID == "f-delta-8" {
			kind = "vulnerability.cve"
		}
		values = append(values, "("+
			quoteLit(row.factID)+`,`+quoteLit(row.scope)+`,`+quoteLit(row.generation)+`,`+
			quoteLit(kind)+`,`+quoteLit("stable-"+row.factID)+`,'vulnerability_intelligence',`+
			`'osv-delta',`+quoteLit(row.factID)+`,now(),now(),`+boolLit(row.tombstone)+`,`+
			quoteLit(payload)+`::jsonb)`)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES `+strings.Join(values, ",\n")); err != nil {
		t.Fatalf("seed changed rows: %v", err)
	}
}

func quoteLit(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

func boolLit(b bool) string {
	if b {
		return "TRUE"
	}
	return "FALSE"
}

func changedPayload(advisory, ecosystem, name, withdrawn, feedTS string) string {
	var b strings.Builder
	b.WriteString(`{"advisory_id":` + jsonQuote(advisory))
	b.WriteString(`,"ecosystem":` + jsonQuote(ecosystem))
	b.WriteString(`,"package_name":` + jsonQuote(name))
	if withdrawn != "" {
		b.WriteString(`,"withdrawn":` + withdrawn)
	}
	b.WriteString(`,"feed_timestamp":` + jsonQuote(feedTS))
	b.WriteString(`}`)
	return b.String()
}

func jsonQuote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// TestListVulnerabilityChangedNamesLive proves the reader against the real
// schema: active-generation rows read back with the raw spelling and the
// parsed withdrawn flag; superseded, pending-scope, tombstone, and
// other-kind rows do not.
func TestListVulnerabilityChangedNamesLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "changed_names_9777", "set ESHU_POSTGRES_DSN to run the #9777 changed_name read proof")
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, changedNamesGenerations); err != nil {
		t.Fatalf("seed generations: %v", err)
	}
	seedChangedRows(t, ctx, db,
		changedRow{"scope-delta-a", "gen-delta-a2", "f-delta-1", "ADV-X", "npm", "left-pad", `"false"`, "2026-11-01T12:00:00Z", false},
		changedRow{"scope-delta-a", "gen-delta-a2", "f-delta-2", "ADV-X", "npm", "kept-name", `"false"`, "2026-11-01T12:00:00Z", false},
		changedRow{"scope-delta-a", "gen-delta-a2", "f-delta-3", "ADV-W", "pypi", "foo-bar", `"true"`, "2026-11-01T12:01:00Z", false},
		// No withdrawn key: the emitter always writes it, and the reader
		// defaults a missing one to false.
		changedRow{"scope-delta-a", "gen-delta-a2", "f-delta-4", "ADV-N", "go", "example.com/mod", "", "2026-11-01T12:02:00Z", false},
		// Superseded generation: history, not change truth.
		changedRow{"scope-delta-a", "gen-delta-a1", "f-delta-5", "ADV-X", "npm", "ancient-name", `"false"`, "2026-11-01T11:00:00Z", false},
		// Pending scope without an active generation: not change truth.
		changedRow{"scope-delta-b", "gen-delta-b1", "f-delta-6", "ADV-X", "npm", "pending-name", `"false"`, "2026-11-01T12:00:00Z", false},
		// A tombstone is a deletion.
		changedRow{"scope-delta-a", "gen-delta-a2", "f-delta-7", "ADV-T", "npm", "tombstoned-name", `"false"`, "2026-11-01T12:00:00Z", true},
		// Another kind carrying the same keys is not a changed name.
		changedRow{"scope-delta-a", "gen-delta-a2", "f-delta-8", "ADV-X", "npm", "cve-kind-name", `"false"`, "2026-11-01T12:00:00Z", false},
	)

	store := NewFactStore(SQLDB{DB: db})
	changed, err := store.ListVulnerabilityChangedNames(ctx)
	if err != nil {
		t.Fatalf("ListVulnerabilityChangedNames() error = %v", err)
	}
	got := make([]string, 0, len(changed))
	withdrawnByAdvisory := map[string]string{}
	for _, row := range changed {
		got = append(got, row.AdvisoryID+"|"+row.Ecosystem+"|"+row.PackageName+"|"+row.FeedTimestamp)
		withdrawnByAdvisory[row.AdvisoryID] = row.Withdrawn
	}
	slices.Sort(got)
	want := []string{
		"ADV-N|go|example.com/mod|2026-11-01T12:02:00Z",
		"ADV-W|pypi|foo-bar|2026-11-01T12:01:00Z",
		"ADV-X|npm|kept-name|2026-11-01T12:00:00Z",
		"ADV-X|npm|left-pad|2026-11-01T12:00:00Z",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("changed = %v, want %v", got, want)
	}
	if withdrawnByAdvisory["ADV-W"] != "true" {
		t.Fatal("ADV-W reads withdrawn=false, want true (retraction must survive the read)")
	}
	if withdrawnByAdvisory["ADV-X"] != "false" || withdrawnByAdvisory["ADV-N"] != "false" {
		t.Fatalf("non-withdrawn advisories read withdrawn: %v", withdrawnByAdvisory)
	}
}

// TestListVulnerabilityChangedNamesMalformedWithdrawnRidesLive pins the
// error-scope contract (#10092 P2): a withdrawn spelling outside the
// contract's 'true'/'false' rides along verbatim instead of failing the
// whole shared read. The coordinator intersection counts it parse_failed;
// one bad fact never aborts the tick for every other instance.
func TestListVulnerabilityChangedNamesMalformedWithdrawnRidesLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "changed_names_bad_9777", "set ESHU_POSTGRES_DSN to run the #9777 changed_name read proof")
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, changedNamesGenerations); err != nil {
		t.Fatalf("seed generations: %v", err)
	}
	seedChangedRows(t, ctx, db,
		changedRow{"scope-delta-a", "gen-delta-a2", "f-bad-1", "ADV-B", "npm", "bad-name", `"yes"`, "2026-11-01T12:00:00Z", false},
		changedRow{"scope-delta-a", "gen-delta-a2", "f-ok-1", "ADV-K", "npm", "kept-name", `"false"`, "2026-11-01T12:00:00Z", false},
	)
	store := NewFactStore(SQLDB{DB: db})
	changed, err := store.ListVulnerabilityChangedNames(ctx)
	if err != nil {
		t.Fatalf("ListVulnerabilityChangedNames() error = %v, want nil (malformed withdrawn must not fail the read)", err)
	}
	byAdvisory := map[string]string{}
	for _, row := range changed {
		byAdvisory[row.AdvisoryID] = row.Withdrawn
	}
	if byAdvisory["ADV-B"] != "yes" {
		t.Fatalf("malformed withdrawn = %q, want the raw spelling", byAdvisory["ADV-B"])
	}
	if byAdvisory["ADV-K"] != "false" {
		t.Fatalf("valid row withdrawn = %q, want false (the bad row must not poison its neighbors)", byAdvisory["ADV-K"])
	}
}
