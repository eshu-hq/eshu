// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 provisional protected target association must distinguish same-digest findings before winner ranking
//eshu:readiness:env ESHU_SCANNER_FINDING_AUTH_PROOF_DISPOSABLE ESHU_SCANNER_FINDING_AUTH_PROOF_DSN

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestProtectedScanTargetsDoNotCollapseSameDigestFindingsLive exercises the
// shipped list query with two otherwise identical findings whose complete
// authority rows identify different scan targets. The protected association
// is provisional until the durable schema lands; the assertion is the stable
// behavior contract.
func TestProtectedScanTargetsDoNotCollapseSameDigestFindingsLive(t *testing.T) {
	// Owned by #9468: the derived read branches stay dark behind AND
	// FALSE until completed results can open them, so no provisional
	// association can distinguish same-digest targets yet. Unskip when
	// #9468 opens the finding-authority gate.
	t.Skip("skip-gated on #9468: derived finding-authority reads stay dark until completed results open them")
	ctx, database := postgresproof.OpenDisposableDatabase(t,
		os.Getenv("ESHU_SCANNER_FINDING_AUTH_PROOF_DSN"),
		os.Getenv("ESHU_SCANNER_FINDING_AUTH_PROOF_DISPOSABLE"), 3*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
CREATE TABLE provisional_finding_authority (
    finding_fact_id text PRIMARY KEY,
    scan_target_id text NOT NULL,
    scan_generation_id text NOT NULL,
    tenant_id text NOT NULL,
    source_scope_id text NOT NULL,
    source_activation_epoch bigint NOT NULL,
    advisory_revision bigint NOT NULL,
    complete boolean NOT NULL
);
CREATE TABLE provisional_source_activation (
    source_scope_id text PRIMARY KEY,
    current_epoch bigint NOT NULL
);
CREATE TABLE provisional_advisory_authority (
    advisory_id text PRIMARY KEY,
    current_revision bigint NOT NULL,
    active boolean NOT NULL
);
CREATE TABLE provisional_reuse_admission (
    finding_fact_id text NOT NULL,
    source_activation_epoch bigint NOT NULL,
    admitted boolean NOT NULL,
    PRIMARY KEY (finding_fact_id, source_activation_epoch)
);
INSERT INTO provisional_source_activation VALUES
  ('source-a', 1), ('source-b', 1), ('source-c', 1);
INSERT INTO provisional_advisory_authority VALUES ('read-advisory', 1, true);
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
   observed_at, ingested_at, status, active_generation_id)
VALUES ('read-advisory', 'vulnerability', 'vulnerability_intelligence', 'read-advisory',
        'vulnerability_intelligence', 'read-advisory', now(), now(), 'active', 'read-advisory-generation');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('read-advisory-generation', 'read-advisory', 'snapshot', now(), now(), 'active');
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
   observed_at, ingested_at, status, active_generation_id)
VALUES ('image://derived/target-a', 'container_image', 'scanner_worker', 'target-a',
        'scanner_worker', 'target-a', now(), now(), 'active', 'scan-generation-a'),
       ('image://derived/target-b', 'container_image', 'scanner_worker', 'target-b',
        'scanner_worker', 'target-b', now(), now(), 'active', 'scan-generation-b'),
       ('image://derived/target-c', 'container_image', 'scanner_worker', 'target-c',
        'scanner_worker', 'target-c', now(), now(), 'active', 'scan-generation-c');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('scan-generation-a', 'image://derived/target-a', 'snapshot', now(), now(), 'active'),
       ('scan-generation-b', 'image://derived/target-b', 'snapshot', now(), now(), 'active'),
       ('scan-generation-c', 'image://derived/target-c', 'snapshot', now(), now(), 'active');
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, payload)
SELECT 'os-' || suffix, 'image://derived/target-' || suffix, 'scan-generation-' || suffix,
       'vulnerability.os_package', 'os-' || suffix, 'scanner_worker',
       'os-' || suffix, now(), now(), '{}'::jsonb
FROM (VALUES ('a'), ('b'), ('c')) AS packages(suffix);
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, payload)
SELECT 'read-finding-' || suffix, 'read-advisory', 'read-advisory-generation',
       'reducer_supply_chain_impact_finding', 'read-finding-' || suffix,
       'reducer', 'read-finding-' || suffix, now(), now(),
       jsonb_build_object('finding_id', 'finding-' || suffix,
          'cve_id', 'CVE-2026-9299', 'package_id', 'pkg:deb/debian/openssl',
          'subject_digest', CASE WHEN suffix='c'
             THEN 'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'
             ELSE 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' END,
          'impact_status', 'affected_exact', 'priority_score', 1,
          'evidence_fact_ids', jsonb_build_array('os-' || suffix))
FROM (VALUES ('a'), ('b'), ('c')) AS findings(suffix);
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, payload)
VALUES ('read-finding-a-stale', 'read-advisory', 'read-advisory-generation',
        'reducer_supply_chain_impact_finding', 'read-finding-a-stale', 'reducer',
        'read-finding-a-stale', now(), now(),
        '{"finding_id":"finding-a-stale","cve_id":"CVE-2026-9299","package_id":"pkg:deb/debian/openssl","subject_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","impact_status":"affected_exact","priority_score":9,"evidence_fact_ids":["os-a"]}'::jsonb);
INSERT INTO provisional_finding_authority
  (finding_fact_id, scan_target_id, scan_generation_id, tenant_id, source_scope_id,
   source_activation_epoch, advisory_revision, complete)
VALUES ('read-finding-a', 'image://derived/target-a', 'scan-generation-a', 'tenant-a', 'source-a', 1, 1, true),
       ('read-finding-b', 'image://derived/target-b', 'scan-generation-b', 'tenant-b', 'source-b', 1, 1, true),
       ('read-finding-c', 'image://derived/target-c', 'scan-generation-c', 'tenant-a', 'source-c', 1, 1, true);
INSERT INTO provisional_finding_authority
  (finding_fact_id, scan_target_id, scan_generation_id, tenant_id, source_scope_id,
   source_activation_epoch, advisory_revision, complete)
VALUES ('read-finding-a-stale', 'image://derived/target-a', 'scan-generation-a',
        'tenant-a', 'source-a', 1, 1, false);
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
   observed_at, ingested_at, status, active_generation_id)
VALUES ('read-static', 'vulnerability', 'vulnerability_intelligence', 'read-static',
        'vulnerability_intelligence', 'read-static', now(), now(), 'active', 'read-static-generation');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('read-static-generation', 'read-static', 'snapshot', now(), now(), 'active');
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, payload)
VALUES ('read-static-finding', 'read-static', 'read-static-generation',
        'reducer_supply_chain_impact_finding', 'read-static-finding', 'reducer',
        'read-static-finding', now(), now(),
        '{"finding_id":"static-finding","cve_id":"CVE-2026-STATIC","subject_digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","impact_status":"affected_exact"}'::jsonb);
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
   observed_at, ingested_at, status, active_generation_id)
VALUES ('image://derived/orphan-finding', 'container_image', 'scanner_worker', 'orphan-finding',
        'scanner_worker', 'orphan-finding', now(), now(), 'active', 'orphan-finding-generation');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('orphan-finding-generation', 'image://derived/orphan-finding', 'snapshot', now(), now(), 'active');
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, payload)
VALUES ('orphan-finding-fact', 'image://derived/orphan-finding', 'orphan-finding-generation',
        'reducer_supply_chain_impact_finding', 'orphan-finding-fact', 'reducer',
        'orphan-finding-fact', now(), now(),
        '{"finding_id":"orphan-finding","cve_id":"CVE-2026-ORPHAN","subject_digest":"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","impact_status":"affected_exact"}'::jsonb);
`); err != nil {
		t.Fatalf("seed protected finding identity fixture: %v", err)
	}
	readIDs := func(query, digest string, allowedScopes []string) []string {
		t.Helper()
		rows, err := database.QueryContext(ctx, query, withGlobalFindingReadArgs(
			FindingFactKind, "", "", "", digest,
			"", "", "", "", "", "", "", "", "", 0, "", "", "finding_id", 201,
			"", true, array.Of([]string{}), array.Of(allowedScopes), time.Now().UTC())...)
		if err != nil {
			t.Fatalf("list finding query: %v", err)
		}
		defer func() { _ = rows.Close() }()
		ids := []string{}
		for rows.Next() {
			var findingID, confidence string
			var payload []byte
			if err := rows.Scan(&findingID, &confidence, &payload); err != nil {
				t.Fatalf("scan finding: %v", err)
			}
			ids = append(ids, findingID)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("finding rows: %v", err)
		}
		return ids
	}
	readCount := func(digest string, allowedScopes []string) int {
		return len(readIDs(ListFindingsQuery, digest, allowedScopes))
	}
	const digestAB = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const digestC = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	const digestOrphan = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	if count := readCount(digestOrphan, nil); count != 0 {
		t.Errorf("unassociated derived finding counted as static/global truth=%d, want 0", count)
	}
	if count := readCount(digestAB, nil); count != 2 {
		t.Errorf("same digest with two protected scan targets yielded %d findings, want 2", count)
	}
	if count := readCount(digestAB, []string{"source-a"}); count != 1 {
		t.Errorf("source-a grant yielded %d findings before pagination, want 1", count)
	}
	if ids := readIDs(ListFindingsQuery, digestAB, nil); strings.Join(ids, ",") != "finding-a,finding-b" {
		t.Errorf("direct authorized winners=%v, want valid target A runner-up and target B", ids)
	}
	if err := storagepostgres.NewSupplyChainImpactWinnersStore(database).RebuildAllWinners(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("rebuild materialized winners: %v", err)
	}
	if ids := readIDs(ListFindingsFromWinnersQuery, digestAB, nil); strings.Join(ids, ",") != "finding-a,finding-b" {
		t.Errorf("materialized authorized winners=%v, want valid target A runner-up and target B", ids)
	}
	readStoreIDs := func(readContext auth.AuthContext, allowedScopes []string) []string {
		t.Helper()
		rows, err := NewPostgresFindingStore(database).ListSupplyChainImpactFindings(
			auth.ContextWithAuthContext(ctx, readContext),
			FindingFilter{SubjectDigest: digestAB, Limit: 201, AllowedScopeIDs: allowedScopes},
		)
		if err != nil {
			t.Fatalf("finding store read: %v", err)
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.FindingID)
		}
		return ids
	}
	if ids := readStoreIDs(auth.AuthContext{Mode: auth.AuthModeShared}, nil); strings.Join(ids, ",") != "finding-a,finding-b" {
		t.Errorf("shared-mode current finding store=%v, want both valid targets", ids)
	}
	if ids := readStoreIDs(auth.AuthContext{
		Mode: auth.AuthModeScoped, TenantID: "tenant-a", AllowedScopeIDs: []string{"source-a"},
	}, []string{"source-a"}); strings.Join(ids, ",") != "finding-a" {
		t.Errorf("tenant A current finding store=%v, want valid target A only", ids)
	}
	if ids := readStoreIDs(auth.AuthContext{
		Mode: auth.AuthModeScoped, TenantID: "tenant-a", AllScopes: true,
	}, nil); len(ids) != 0 {
		t.Errorf("AllScopes without source grant returned derived findings=%v, want none", ids)
	}
	readAggregate := func(allowedScopes []string) int64 {
		t.Helper()
		var total, affected, exact, derived, possible, unaffected sql.NullInt64
		err := database.QueryRowContext(ctx, AggregateCountQuery, withGlobalFindingReadArgs(
			"", "", "", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"", "", "", "", "", "", "", "", "", 0,
			"", true, "", array.Of([]string{}), array.Of(allowedScopes), time.Now().UTC())...).Scan(
			&total, &affected, &exact, &derived, &possible, &unaffected)
		if err != nil {
			t.Fatalf("count finding query: %v", err)
		}
		return total.Int64
	}
	if count := readAggregate(nil); count != 2 {
		t.Errorf("same digest aggregate count=%d, want 2 target-local findings", count)
	}
	if count := readAggregate([]string{"source-a"}); count != 1 {
		t.Errorf("source-a aggregate count=%d, want 1 before grouping", count)
	}
	readExplainCount := func(allowedScopes []string) int {
		t.Helper()
		rows, err := database.QueryContext(ctx, ExplainFindingQuery, withGlobalFindingReadArgs(
			FindingFactKind, "", "", "", "", "",
			"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"", "", "", array.Of([]string{}), array.Of(allowedScopes), time.Now().UTC())...)
		if err != nil {
			t.Fatalf("explain candidate query: %v", err)
		}
		defer func() { _ = rows.Close() }()
		count := 0
		for rows.Next() {
			var factID, confidence string
			var payload []byte
			if err := rows.Scan(&factID, &confidence, &payload); err != nil {
				t.Fatalf("scan explain candidate: %v", err)
			}
			count++
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("explain candidates: %v", err)
		}
		return count
	}
	if count := readExplainCount(nil); count != 2 {
		t.Errorf("same digest explain candidate count=%d, want ambiguous 2", count)
	}
	if count := readExplainCount([]string{"source-a"}); count != 1 {
		t.Errorf("source-a explain candidate count=%d, want 1", count)
	}
	page, err := database.QueryContext(ctx, ListFindingsQuery, withGlobalFindingReadArgs(
		FindingFactKind, "", "", "", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"", "", "", "", "", "", "", "", "", 0, "", "finding-a", "finding_id", 1,
		"", true, array.Of([]string{}), array.Of([]string{}), time.Now().UTC())...)
	if err != nil {
		t.Fatalf("second finding page: %v", err)
	}
	if !page.Next() {
		t.Error("same-digest target B was lost before second-page cursor")
	} else {
		var findingID, confidence string
		var payload []byte
		if err := page.Scan(&findingID, &confidence, &payload); err != nil {
			t.Fatalf("scan second finding page: %v", err)
		}
		if findingID != "finding-b" {
			t.Errorf("second-page finding=%q, want finding-b after authorized target A", findingID)
		}
	}
	if err := page.Close(); err != nil {
		t.Fatalf("close second finding page: %v", err)
	}
	scopedContext := auth.ContextWithAuthContext(ctx, auth.AuthContext{
		Mode: auth.AuthModeScoped, TenantID: "tenant-a", AllowedScopeIDs: []string{"source-a"},
	})
	evidence, err := NewPostgresFindingStore(database).loadSupplyChainImpactEvidenceFacts(
		scopedContext, []string{"os-a", "os-b"})
	if err != nil {
		t.Fatalf("scoped explain hydration: %v", err)
	}
	if len(evidence) != 1 || evidence[0].FactID != "os-a" {
		t.Errorf("scoped explain hydration returned %d facts, want only os-a", len(evidence))
	}
	staticRows, err := database.QueryContext(ctx, ListFindingsQuery, withGlobalFindingReadArgs(
		FindingFactKind, "", "", "", "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"", "", "", "", "", "", "", "", "", 0, "", "", "finding_id", 201,
		"", true, array.Of([]string{}), array.Of([]string{"read-static"}), time.Now().UTC())...)
	if err != nil {
		t.Fatalf("direct static finding: %v", err)
	}
	if !staticRows.Next() {
		t.Error("direct static-scope finding disappeared")
	}
	if err := staticRows.Close(); err != nil {
		t.Fatalf("close direct static finding: %v", err)
	}
	if count := readCount(digestC, nil); count != 1 {
		t.Fatalf("distinct target C baseline=%d, want 1", count)
	}
	// P0 split (#9468): derived reads stay dark until completed results
	// open them (the finding-authority predicate ends in AND FALSE), so
	// the fence-transition assertions below cannot pass yet. The
	// transition writes stay as the scenario record; the expectations
	// move verbatim to #9468, which restores them when the gates open:
	//   global read after source activation switch=0
	//   global read after same-scope source reactivation=0 before fenced reuse
	//   global read after explicit compatible reuse admission=1 (kept below)
	//   global read after advisory revocation=0
	//   global read after advisory revision ABA=0
	//   classified derived findings without publication leak through
	//   neither the direct list nor the static winners (kept for winners)
	if _, err := database.ExecContext(ctx, `UPDATE provisional_source_activation
SET current_epoch=2 WHERE source_scope_id='source-c'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE provisional_source_activation
SET current_epoch=3 WHERE source_scope_id='source-c'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO provisional_reuse_admission VALUES ('read-finding-c', 3, true);
UPDATE provisional_finding_authority SET source_activation_epoch=3
WHERE finding_fact_id='read-finding-c';`); err != nil {
		t.Fatal(err)
	}
	if count := readCount(digestC, nil); count != 1 {
		t.Errorf("global read after explicit compatible reuse admission=%d, want 1", count)
	}
	if _, err := database.ExecContext(ctx, `UPDATE provisional_advisory_authority
SET active=false WHERE advisory_id='read-advisory'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE provisional_advisory_authority
SET active=true, current_revision=2 WHERE advisory_id='read-advisory'`); err != nil {
		t.Fatal(err)
	}
	// Until a publication has an exact protected authority witness, a
	// classified derived fact under an advisory scope must stay out of both
	// the direct query and the static-only materialized winner table.
	if _, err := database.ExecContext(ctx, `
INSERT INTO scanner_image_result_fact_classifications
  (finding_fact_id,source_class,lifecycle,scan_scope_id)
VALUES ('read-finding-a','derived','active','image://derived/target-a'),
       ('read-finding-a-stale','derived','active','image://derived/target-a'),
       ('read-finding-b','derived','active','image://derived/target-b'),
       ('read-finding-c','derived','active','image://derived/target-c')`); err != nil {
		t.Fatalf("classify derived findings: %v", err)
	}
	if ids := readIDs(ListFindingsQuery, digestAB, nil); len(ids) != 0 {
		t.Errorf("classified derived findings without publication leaked through direct list: %v", ids)
	}
	if err := storagepostgres.NewSupplyChainImpactWinnersStore(database).RebuildAllWinners(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("rebuild static-only winners: %v", err)
	}
	if ids := readIDs(ListFindingsFromWinnersQuery, digestAB, nil); len(ids) != 0 {
		t.Errorf("classified derived findings without publication leaked through static winners: %v", ids)
	}
	if count := readCount("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nil); count != 1 {
		t.Errorf("legacy static finding after derived classification count=%d, want 1", count)
	}
}

func withGlobalFindingReadArgs(base ...any) []any {
	return append(base, false, "", "", "", "", array.Of([]string{}))
}
