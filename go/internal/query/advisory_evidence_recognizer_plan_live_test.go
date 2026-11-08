// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// TestAdvisoryEvidenceRecognizerPlanLive pins the #10410 performance proof:
// the recognizer runs inside seed_keys over the bounded seed, so the
// seed/matched legs still probe the v2 lookup indexes with no
// fact_records sequence scan.
func TestAdvisoryEvidenceRecognizerPlanLive(t *testing.T) {
	db := openRecognizerLiveDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	seedRecognizerLiveFixtures(t, ctx, db)

	for _, lookup := range []string{"CVE-2026-5101", "PYSEC-2026-5101"} {
		rows, err := db.QueryContext(ctx, "EXPLAIN "+listAdvisoryEvidenceQuery,
			array.Of(recognizerLiveKinds),
			array.Of([]string{lookup}),
			array.Of([]string{}),
			"",
			advisory.EvidenceMaxFactRows,
			"", "", "",
			array.Of([]string{}),
		)
		if err != nil {
			t.Fatalf("explain advisory evidence for %q: %v", lookup, err)
		}
		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				_ = rows.Close()
				t.Fatalf("scan explain row: %v", err)
			}
			lines = append(lines, line)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate explain rows: %v", err)
		}
		plan := strings.Join(lines, "\n")
		t.Logf("evidence plan for %q:\n%s", lookup, plan)
		// Scope the scan assertion to the seed/matched CTEs, matching the
		// #9474 package-leg proof: impact_candidates legitimately walks
		// fact_records in a schema without the impact read's own indexes.
		sections := map[string][]string{}
		current := ""
		for _, line := range lines {
			if m := cteHeader.FindStringSubmatch(line); m != nil {
				current = m[1]
				continue
			}
			if current != "" {
				sections[current] = append(sections[current], line)
			}
		}
		for _, section := range []string{"seed_candidates", "matched_candidates"} {
			body := strings.Join(sections[section], "\n")
			if body == "" {
				t.Fatalf("plan for %q is missing CTE %s:\n%s", lookup, section, plan)
			}
			if strings.Contains(body, "Seq Scan on fact_records") {
				t.Fatalf("plan for %q seq-scans fact_records in CTE %s:\n%s", lookup, section, plan)
			}
		}
		for _, want := range []string{
			"fact_records_vulnerability_active_cve_lookup_v2_idx",
			"fact_records_vulnerability_active_advisory_lookup_v2_idx",
			"fact_records_vulnerability_active_ghsa_lookup_v2_idx",
		} {
			if !strings.Contains(plan, want) {
				t.Fatalf("plan for %q does not probe %s:\n%s", lookup, want, plan)
			}
		}
	}
}
