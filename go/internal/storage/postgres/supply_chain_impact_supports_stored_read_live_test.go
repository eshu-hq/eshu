// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// Live proofs of the supports-backed reads of #10880 PR-C1 (arbiter ruling R5): the
// writer's stored read and the three-leg recovery read give the same answer as the
// reads they replaced on one seed, and the scope-delete capture (migration 352) stages
// the same identifiers the fact read staged, whatever the delete order. The frozen
// legacy statements below are the oracles; the supports statements come from the
// writer's own package through the proof seam, so the proof runs what the writer runs.

// supportsLegacyStoredRead is the stored read before C1 b, frozen. Its digest is the
// one core's TestLegacyStoredFindingsQueryStaysFrozen pins.
const supportsLegacyStoredRead = `
SELECT fact_id,
       generation_id,
       stable_fact_key,
       COALESCE(payload #>> '{provenance,content_digest}', ''),
       COALESCE(payload ->> 'cve_id', ''),
       COALESCE(payload ->> 'advisory_id', ''),
       fencing_token,
       COALESCE(payload #>> '{provenance,advisory_group_members}', '[]'),
       COALESCE(NULLIF(payload ->> 'suppression_state', ''), 'active'),
       COALESCE(payload #>> '{evidence_fact_ids}', '[]')
FROM fact_records
WHERE fact_kind = 'reducer_supply_chain_impact_finding'
  AND scope_id = $1
  AND is_tombstone = FALSE
  AND generation_id IN ($2, (
    SELECT generation.generation_id
    FROM scope_generations AS generation
    WHERE generation.scope_id = $1
      AND generation.generation_id <> $2
      AND generation.status IN ('active', 'superseded')
    ORDER BY generation.activated_at DESC NULLS LAST, generation.ingested_at DESC
    LIMIT 1
))
`

const supportsLegacyStoredReadDigest = "00d9ad56e624ab3f780d8da4c4af8e6f72906ed47c00530ef83f4bc06a91622e"

// storedReadRow is one row of the stored read.
type storedReadRow struct {
	FactID, GenerationID, Key, Digest, CVE, Advisory string
	Token                                            int64
	Members, Suppression, Evidence                   string
}

func scanStoredRead(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) map[string]storedReadRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("stored read: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]storedReadRow{}
	for rows.Next() {
		var r storedReadRow
		if err := rows.Scan(&r.FactID, &r.GenerationID, &r.Key, &r.Digest, &r.CVE, &r.Advisory, &r.Token, &r.Members, &r.Suppression, &r.Evidence); err != nil {
			t.Fatalf("scan stored read: %v", err)
		}
		out[r.FactID] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("stored read rows: %v", err)
	}
	return out
}

// supportsReadSeed writes n findings with distinct CVEs through the real writer
// (facts, supports and keys together) and returns their fact ids in order.
func supportsReadSeed(t *testing.T, ctx context.Context, db *sql.DB, intent string, from, to int) []string {
	t.Helper()
	findings := []supplychaincore.SupplyChainImpactFinding{}
	for i := from; i < to; i++ {
		f := replaceSetFinding(fmt.Sprintf("repository:r_c1_read_%d", i))
		f.CVEID = fmt.Sprintf("CVE-2026-91%03d", i)
		findings = append(findings, f)
	}
	if _, err := replaceSetWriter(db).WriteSupplyChainImpactFindings(ctx, replaceSetWrite(intent, findings...)); err != nil {
		t.Fatalf("seed write %s: %v", intent, err)
	}
	return c1ReadLiveFactIDs(t, ctx, db, replaceSetLiveScope)
}

func c1ReadLiveFactIDs(t *testing.T, ctx context.Context, db *sql.DB, scope string) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT fact_id FROM fact_records
WHERE scope_id = $1 AND fact_kind = 'reducer_supply_chain_impact_finding' AND NOT is_tombstone
ORDER BY fact_id`, scope)
	if err != nil {
		t.Fatalf("list fact ids: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func supportsExec(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec %q: %v", strings.Fields(query)[0], err)
	}
}

// TestSupplyChainImpactSupportsStoredReadEqualsLegacyReadLive proves the supports-backed
// stored read returns the rows, columns and keys the fact read returned (the fencing
// token aside, which is the constant 0), including a NULL support key with a twin, with
// no twin, with an empty twin key, and a filled support key that differs from its twin.
func TestSupplyChainImpactSupportsStoredReadEqualsLegacyReadLive(t *testing.T) {
	sum := sha256.Sum256([]byte(supportsLegacyStoredRead))
	if got := hex.EncodeToString(sum[:]); got != supportsLegacyStoredReadDigest {
		t.Fatalf("the frozen legacy stored read hashes to %s, want %s: the oracle was edited", got, supportsLegacyStoredReadDigest)
	}
	ctx, db := openReplaceSetLiveDB(t)
	ids := supportsReadSeed(t, ctx, db, "intent:c1:stored-read", 0, 6)
	if len(ids) != 6 {
		t.Fatalf("seeded %d findings, want 6", len(ids))
	}
	nullTwin, noTwin, emptyTwin, differs := ids[1], ids[2], ids[3], ids[4]
	supportsExec(t, ctx, db, `UPDATE supply_chain_impact_finding_supports SET stable_fact_key = NULL WHERE fact_id = $1`, nullTwin)
	supportsExec(t, ctx, db, `UPDATE supply_chain_impact_finding_supports SET stable_fact_key = NULL WHERE fact_id = $1`, noTwin)
	supportsExec(t, ctx, db, `DELETE FROM fact_records WHERE fact_id = $1`, noTwin)
	supportsExec(t, ctx, db, `UPDATE supply_chain_impact_finding_supports SET stable_fact_key = NULL WHERE fact_id = $1`, emptyTwin)
	supportsExec(t, ctx, db, `UPDATE fact_records SET stable_fact_key = '' WHERE fact_id = $1`, emptyTwin)
	supportsExec(t, ctx, db, `UPDATE supply_chain_impact_finding_supports SET stable_fact_key = 'support-key-wins' WHERE fact_id = $1`, differs)

	legacy := scanStoredRead(t, ctx, db, supportsLegacyStoredRead, replaceSetLiveScope, replaceSetLiveGeneration)
	supports := scanStoredRead(t, ctx, db, supplychaincore.ProofStoredFindingsQuery(), replaceSetLiveScope, replaceSetLiveGeneration)

	for _, id := range ids {
		s, ok := supports[id]
		if !ok {
			t.Errorf("fact %s: the supports read returned no row", id)
			continue
		}
		if s.Token != 0 {
			t.Errorf("fact %s: supports fencing token = %d, want the constant 0", id, s.Token)
		}
		l, inLegacy := legacy[id]
		switch id {
		case noTwin:
			if inLegacy {
				t.Errorf("fact %s: the legacy read returned a row for a deleted fact", id)
			}
			if s.Key != "" {
				t.Errorf("fact %s (no twin): supports key = %q, want the empty key the reader replaces", id, s.Key)
			}
			continue
		case differs:
			if s.Key != "support-key-wins" {
				t.Errorf("fact %s: supports key = %q, want the support's own key over its twin's", id, s.Key)
			}
			l.Key, s.Key = "", ""
		case emptyTwin:
			if s.Key != "" {
				t.Errorf("fact %s (empty twin): supports key = %q, want empty", id, s.Key)
			}
		}
		if !inLegacy {
			t.Errorf("fact %s: the legacy read returned no row", id)
			continue
		}
		l.Token, s.Token = 0, 0
		if !reflect.DeepEqual(l, s) {
			t.Errorf("fact %s: supports read %+v != legacy read %+v", id, s, l)
		}
	}
	// The NULL-key-with-twin row carries the twin's key, the legacy value.
	if got, want := supports[nullTwin].Key, legacy[nullTwin].Key; got == "" || got != want {
		t.Errorf("fact %s (NULL key, twin): supports key = %q, want the twin's %q", nullTwin, got, want)
	}
	if len(supports) != len(ids) {
		t.Errorf("supports read returned %d rows, want %d", len(supports), len(ids))
	}
}

type recoveryRow struct{ A, B string }

func scanRecovery(t *testing.T, ctx context.Context, db *sql.DB, query string) []recoveryRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, replaceSetLiveScope, replaceSetLiveGeneration)
	if err != nil {
		t.Fatalf("recovery read: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []recoveryRow
	for rows.Next() {
		var r recoveryRow
		if err := rows.Scan(&r.A, &r.B); err != nil {
			t.Fatalf("scan recovery read: %v", err)
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]) < fmt.Sprint(out[j]) })
	return out
}

// TestSupplyChainImpactSupportsRecoveryReadEqualsLegacyReadLive proves the three-leg
// recovery reads name the same CVE ids, group members and (cve, advisory) pairs as the
// frozen fact statement after a pass retracted findings, and that the retracted keys
// alone (the supports and retracted-keys legs, no fact leg) still name every retracted
// finding: the state a retry meets after an attempt that committed its retraction and
// died before anything else.
func TestSupplyChainImpactSupportsRecoveryReadEqualsLegacyReadLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	supportsReadSeed(t, ctx, db, "intent:c1:recovery-seed", 0, 6)
	// A second authoritative pass that keeps findings 0 and 1: 2..5 are retracted, so
	// their supports are gone and their keys are recorded.
	supportsReadSeed(t, ctx, db, "intent:c1:recovery-retract", 0, 2)

	for _, tc := range []struct {
		name, three, factsLeg string
	}{
		{"cve ids", supplychaincore.ProofRecoveryCVEIDsQuery(), supplychaincore.ProofRecoveryCVEIDsFactsLegQuery()},
		{"group ids", supplychaincore.ProofRecoveryGroupIDsQuery(), supplychaincore.ProofRecoveryGroupIDsFactsLegQuery()},
	} {
		three := scanRecovery(t, ctx, db, tc.three)
		legacy := scanRecovery(t, ctx, db, tc.factsLeg)
		if len(legacy) == 0 {
			t.Fatalf("%s: the legacy fact statement named nothing: the seed is empty", tc.name)
		}
		if !reflect.DeepEqual(three, legacy) {
			t.Errorf("%s: the three-leg read %v != the legacy fact read %v", tc.name, three, legacy)
		}
		// Crash after commit: without the fact leg the retracted findings are still named.
		twoLegs := strings.TrimSuffix(strings.TrimSuffix(tc.three, tc.factsLeg), "UNION")
		if twoLegs == tc.three {
			t.Fatalf("%s: the three-leg text does not end in the fact leg", tc.name)
		}
		without := scanRecovery(t, ctx, db, twoLegs)
		if !reflect.DeepEqual(without, legacy) {
			t.Errorf("%s: the supports and retracted-keys legs alone %v != the legacy fact read %v: a retry after a committed retraction would lose a retracted finding", tc.name, without, legacy)
		}
	}
}

// supportsCaptureIdentifiers reads the identifier tuples of the scope's live findings
// from table, with the capture function's own expressions.
func supportsCaptureIdentifiers(t *testing.T, ctx context.Context, db *sql.DB, factsTable bool, scope string) []string {
	t.Helper()
	query := `
SELECT DISTINCT COALESCE(payload->>'package_id', ''), COALESCE(payload->>'repository_id', ''),
       COALESCE(payload->>'subject_digest', ''),
       COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), ''),
       COALESCE(payload->>'advisory_id', '')
FROM supply_chain_impact_finding_supports WHERE scope_id = $1`
	if factsTable {
		query = `
SELECT DISTINCT COALESCE(payload->>'package_id', ''), COALESCE(payload->>'repository_id', ''),
       COALESCE(payload->>'subject_digest', ''),
       COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), ''),
       COALESCE(payload->>'advisory_id', '')
FROM fact_records WHERE scope_id = $1 AND fact_kind = 'reducer_supply_chain_impact_finding' AND NOT is_tombstone`
	}
	return supportsTuples(t, ctx, db, query, scope)
}

func supportsTuples(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("tuples: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var a, b, c, d, e string
		if err := rows.Scan(&a, &b, &c, &d, &e); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Join([]string{a, b, c, d, e}, "|"))
	}
	sort.Strings(out)
	return out
}

// TestSupplyChainImpactSupportsScopeDeleteCaptureStagesTheFactSetLive proves migration
// 352: deleting a scope stages exactly the identifiers the fact-reading capture staged,
// read from the supports before the foreign-key cascade removes them. The second case
// deletes the retired generation first, then the scope: the identifiers staged must be
// the ones the fact read would have staged from what is left.
func TestSupplyChainImpactSupportsScopeDeleteCaptureStagesTheFactSetLive(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		deleteGenerationFirst bool
	}{
		{"scope deleted with its generations", false},
		{"retired generation deleted before its scope", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db := openReplaceSetLiveDB(t)
			supportsReadSeed(t, ctx, db, "intent:c1:scope-delete-current", 0, 4)
			// Findings in the retired generation: the same writer, another generation.
			prior := replaceSetLiveGeneration + ":prior"
			priorWrite := replaceSetWrite("intent:c1:scope-delete-prior",
				func() []supplychaincore.SupplyChainImpactFinding {
					var out []supplychaincore.SupplyChainImpactFinding
					for i := 10; i < 13; i++ {
						f := replaceSetFinding(fmt.Sprintf("repository:r_c1_prior_%d", i))
						f.CVEID = fmt.Sprintf("CVE-2026-92%03d", i)
						out = append(out, f)
					}
					return out
				}()...)
			priorWrite.GenerationID = prior
			if _, err := replaceSetWriter(db).WriteSupplyChainImpactFindings(ctx, priorWrite); err != nil {
				t.Fatalf("write the retired generation: %v", err)
			}
			if tc.deleteGenerationFirst {
				supportsExec(t, ctx, db, `DELETE FROM scope_generations WHERE generation_id = $1`, prior)
			}
			wantFromFacts := supportsCaptureIdentifiers(t, ctx, db, true, replaceSetLiveScope)
			wantFromSupports := supportsCaptureIdentifiers(t, ctx, db, false, replaceSetLiveScope)
			if len(wantFromFacts) == 0 || !reflect.DeepEqual(wantFromFacts, wantFromSupports) {
				t.Fatalf("before the delete the fact set %v and the supports set %v differ or are empty: the seed is not a fair oracle", wantFromFacts, wantFromSupports)
			}

			supportsExec(t, ctx, db, `DELETE FROM supply_chain_impact_winners_dirty_staging`)
			supportsExec(t, ctx, db, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, replaceSetLiveScope)
			got := supportsTuples(t, ctx, db, `
SELECT package_id, repository_id, subject_digest, c0, advisory_id
FROM supply_chain_impact_winners_dirty_staging
WHERE kind = 'fact' AND source = 'activation' AND $1::text IS NOT NULL`, replaceSetLiveScope)
			dedup := map[string]bool{}
			var staged []string
			for _, g := range got {
				if !dedup[g] {
					dedup[g] = true
					staged = append(staged, g)
				}
			}
			if !reflect.DeepEqual(staged, wantFromFacts) {
				t.Fatalf("the scope-delete capture staged %v, want the identifiers the fact read named %v (supports read before the cascade, after a delete order of %q)", staged, wantFromFacts, tc.name)
			}
		})
	}
}
