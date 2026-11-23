// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The base digests below were taken at commit 662083f413 (origin/main when
// #11234 started) by a throwaway dump test; the frozen gate in
// testdata/stale_gate_pre_11234.sql was written by the same run. The batch and
// single digests were re-pinned once, when #10880 PR-B2 enrolled the
// supply_chain_impact_supports_not_ready class: the claim statements embed the
// non-counting class list (reducerClaimAttemptCountCaseSQL), so the one added
// predicate moved their bytes outside the gate; the funnel carries no class list
// and keeps its base digest. Enrolling another non-counting class re-pins the two
// the same way.
const (
	staleGate11234PreGateSHA    = "30a3fd23ee77b417d8c6cd5a981dfae7953c30749f8552953f41b766a38b3aeb"
	staleGate11234BaseBatchSHA  = "63d4b2b9a063e3f84e283a5a1819959421190b718b5ada8317ac533cf45785f1"
	staleGate11234BaseSingleSHA = "c7512ad37237837d15425f9f77798a145d5cf268e719488b7782239b6bc25a05"
	staleGate11234BaseFunnelSHA = "53bf5acdcabd4b6d3cf77e343c3638de3b7fb3066ed02c8829df49258369597f"
)

func staleGate11234Digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// staleGate11234PreGate returns the frozen pre-#11234 gate text with its
// comment header stripped, after checking it is the base text byte for
// byte.
func staleGate11234PreGate(t testing.TB) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "stale_gate_pre_11234.sql"))
	if err != nil {
		t.Fatalf("read frozen pre-#11234 gate: %v", err)
	}
	text := string(raw)
	for strings.HasPrefix(text, "--") {
		_, text, _ = strings.Cut(text, "\n")
	}
	// The base text ends in ")"; the file adds one newline for the
	// end-of-file hook.
	text = strings.TrimSuffix(text, "\n")
	if got := staleGate11234Digest(text); got != staleGate11234PreGateSHA {
		t.Fatalf("frozen pre-#11234 gate digest = %s, want the base digest %s", got, staleGate11234PreGateSHA)
	}
	return text
}

// staleGate11234SwapGate returns query with the shipped gate replaced by
// gate, failing unless the shipped gate occurs exactly once.
func staleGate11234SwapGate(t testing.TB, query, gate string) string {
	t.Helper()
	if n := strings.Count(query, reducerClaimStaleGateCTEs()); n != 1 {
		t.Fatalf("query composes the stale gate %d times, want 1", n)
	}
	return strings.Replace(query, reducerClaimStaleGateCTEs(), gate, 1)
}

// staleGate11234OutsideGateDigest is the digest of query with its gate
// swapped back to the pre-#11234 text: equal to the base statement's
// digest exactly when nothing outside the gate changed.
func staleGate11234OutsideGateDigest(t testing.TB, query, pre string) string {
	t.Helper()
	return staleGate11234Digest(staleGate11234SwapGate(t, query, pre))
}

// TestStaleGate11234LeavesEveryOtherByteUnchanged pins #11234's scope: in
// the batch claim, the single claim and the funnel, every byte outside the
// stale gate (the candidate chain, the FOR UPDATE SKIP LOCKED lock arms,
// their EvalPlanQual rechecks, the claim UPDATE and RETURNING) is the base
// text. A seeded edit to the lock text must break the digest.
func TestStaleGate11234LeavesEveryOtherByteUnchanged(t *testing.T) {
	t.Parallel()
	pre := staleGate11234PreGate(t)
	for name, c := range map[string]struct{ query, want string }{
		"batch":  {claimReducerWorkBatchQuery, staleGate11234BaseBatchSHA},
		"single": {claimReducerWorkQuery, staleGate11234BaseSingleSHA},
		"funnel": {reducerClaimBatchFunnelQuery, staleGate11234BaseFunnelSHA},
	} {
		if got := staleGate11234OutsideGateDigest(t, c.query, pre); got != c.want {
			t.Fatalf("%s statement outside the stale gate differs from base: digest %s, want %s", name, got, c.want)
		}
	}
	// Seeded violation: a lock-text edit is caught.
	for _, edit := range [][2]string{{"SKIP LOCKED", "NOWAIT"}, {"RETURNING", "RETURNING  "}} {
		if !strings.Contains(claimReducerWorkBatchQuery, edit[0]) {
			t.Fatalf("seeded edit anchor %q missing from the batch claim", edit[0])
		}
		mutated := strings.Replace(claimReducerWorkBatchQuery, edit[0], edit[1], 1)
		if staleGate11234OutsideGateDigest(t, mutated, pre) == staleGate11234BaseBatchSHA {
			t.Fatalf("seeded edit %q -> %q was not caught by the digest guard", edit[0], edit[1])
		}
	}
}

// staleGate11234BodyHead opens the stale_reducer_candidates CTE in both
// the base and the shipped gate.
const staleGate11234BodyHead = "stale_reducer_candidates AS MATERIALIZED ("

// staleGate11234CheckBody returns nil when gate's stale_reducer_candidates
// body is the base body with exactly two lines replaced: the FROM line now
// reads stale_gate_rows, and the join to claim_active_generations is now a
// CROSS JOIN LATERAL ... OFFSET 0 lookup aliased scope that carries the
// base active-generation ON text verbatim. Any other difference fails.
func staleGate11234CheckBody(pre, gate string) error {
	_, preBody, okPre := strings.Cut(pre, staleGate11234BodyHead)
	_, gateBody, okGate := strings.Cut(gate, staleGate11234BodyHead)
	if !okPre || !okGate {
		return fmt.Errorf("stale_reducer_candidates missing (pre %v, shipped %v)", okPre, okGate)
	}
	// The base ON text of the active-generation join, from the base
	// claim_active_generations, compared line by line without indentation.
	_, preCAG, _ := strings.Cut(pre, "JOIN scope_generations AS active_generation\n")
	preOn, _, _ := strings.Cut(preCAG, "\n),")
	onLines := staleGate11234TrimmedLines(preOn)
	if len(onLines) != 2 {
		return fmt.Errorf("base active-generation ON text has %d lines, want 2: %q", len(onLines), preOn)
	}
	preLines := strings.Split(preBody, "\n")
	gateLines := strings.Split(gateBody, "\n")
	const (
		preFrom   = "    FROM fact_work_items AS stale"
		gateFrom  = "    FROM stale_gate_rows AS stale"
		preJoin   = "    JOIN claim_active_generations AS scope ON scope.scope_id = stale.scope_id"
		gateOpen  = "    CROSS JOIN LATERAL ("
		gateClose = "    ) AS scope"
	)
	replaced := 0
	j := 0
	for i := 0; i < len(preLines); i++ {
		if j >= len(gateLines) {
			return fmt.Errorf("shipped body ends early at base line %d", i)
		}
		switch preLines[i] {
		case preFrom:
			if gateLines[j] != gateFrom {
				return fmt.Errorf("FROM line = %q, want %q", gateLines[j], gateFrom)
			}
			replaced++
			j++
			continue
		case preJoin:
			if gateLines[j] != gateOpen {
				return fmt.Errorf("scope join = %q, want %q", gateLines[j], gateOpen)
			}
			end := j + 1
			for end < len(gateLines) && gateLines[end] != gateClose {
				end++
			}
			if end == len(gateLines) {
				return fmt.Errorf("lateral lookup is not closed by %q", gateClose)
			}
			want := []string{
				"SELECT scope.scope_id,",
				"scope.active_generation_id,",
				"active_generation.ingested_at AS active_ingested_at",
				"FROM ingestion_scopes AS scope",
				"JOIN scope_generations AS active_generation",
				onLines[0],
				onLines[1],
				"WHERE scope.scope_id = stale.scope_id",
				"OFFSET 0",
			}
			got := staleGate11234TrimmedLines(strings.Join(gateLines[j+1:end], "\n"))
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				return fmt.Errorf("lateral lookup =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
			replaced++
			j = end + 1
			continue
		}
		if gateLines[j] != preLines[i] {
			return fmt.Errorf("body line differs from base: %q, base %q", gateLines[j], preLines[i])
		}
		j++
	}
	if j != len(gateLines) {
		return fmt.Errorf("shipped body has %d extra lines", len(gateLines)-j)
	}
	if replaced != 2 {
		return fmt.Errorf("replaced %d base lines, want exactly 2", replaced)
	}
	return nil
}

// staleGate11234TrimmedLines splits s into its non-empty lines without
// surrounding whitespace.
func staleGate11234TrimmedLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// staleGate11234CheckColumns returns nil when stale_gate_rows selects every
// column the stale_reducer_candidates body reads through the stale alias
// (arbiter-11234-fix-e decision 1).
func staleGate11234CheckColumns(gate string) error {
	_, rows, _ := strings.Cut(gate, "stale_gate_rows AS MATERIALIZED (")
	selectList, _, _ := strings.Cut(rows, "FROM fact_work_items AS work")
	have := map[string]bool{}
	for _, m := range regexp.MustCompile(`work\.([a-z_]+)`).FindAllStringSubmatch(selectList, -1) {
		have[m[1]] = true
	}
	_, body, _ := strings.Cut(gate, staleGate11234BodyHead)
	read := regexp.MustCompile(`\bstale\.([a-z_]+)`).FindAllStringSubmatch(body, -1)
	if len(read) == 0 {
		return fmt.Errorf("body reads no stale column")
	}
	for _, m := range read {
		if !have[m[1]] {
			return fmt.Errorf("body reads stale.%s, which stale_gate_rows does not select", m[1])
		}
	}
	return nil
}

// TestStaleGate11234KeepsTheStalePredicate pins that #11234 changed only
// where the gate's rows and active generation come from, not which rows it
// fences (arbiter-11234-accept decision 1): the body is the base body with
// exactly two lines replaced, stale_gate_rows carries every column the
// body reads, and the prefilter narrows only by conditions the base body
// already applies. Seeded mutants of each rule must fail.
func TestStaleGate11234KeepsTheStalePredicate(t *testing.T) {
	t.Parallel()
	pre := staleGate11234PreGate(t)
	gate := reducerClaimStaleGateCTEs()
	if err := staleGate11234CheckBody(pre, gate); err != nil {
		t.Fatalf("stale gate body: %v", err)
	}
	if err := staleGate11234CheckColumns(gate); err != nil {
		t.Fatalf("stale gate columns: %v", err)
	}
	for name, mutant := range map[string][2]string{
		"third body line":       {"AND stale.status IN ('pending', 'retrying')", "AND stale.status IN ('pending')"},
		"lateral ON text":       {"AND active_generation.scope_id = scope.scope_id", "AND active_generation.scope_id IS NOT NULL"},
		"lateral without fence": {"        OFFSET 0\n", ""},
	} {
		mutated := strings.Replace(gate, mutant[0], mutant[1], 1)
		if mutated == gate {
			t.Fatalf("mutant %q anchor %q missing", name, mutant[0])
		}
		if staleGate11234CheckBody(pre, mutated) == nil {
			t.Fatalf("mutant %q passed the body guard", name)
		}
	}
	dropped := strings.Replace(gate, "           work.payload\n", "", 1)
	dropped = strings.Replace(dropped, "           work.status,\n", "           work.status\n", 1)
	if dropped == gate || staleGate11234CheckColumns(dropped) == nil {
		t.Fatal("mutant without work.payload passed the column guard")
	}
	_, preBody, _ := strings.Cut(pre, staleGate11234BodyHead)
	for _, cond := range []string{
		"stage = 'reducer'",
		"status IN ('pending', 'retrying')",
		"($2::text[] IS NULL OR stale.domain = ANY($2::text[]))",
		"stale.generation_id <> scope.active_generation_id",
	} {
		if !strings.Contains(preBody, cond) {
			t.Fatalf("base stale predicate lacks %q, which the prefilter assumes", cond)
		}
	}
	for _, want := range []string{
		"stale_gate_rows AS MATERIALIZED (",
		"AND work.generation_id <> work_scope.active_generation_id",
	} {
		if !strings.Contains(gate, want) {
			t.Fatalf("stale gate missing %q", want)
		}
	}
}

// TestStaleGate11234DropsThePerScopeSet pins that no claim statement still
// builds the per-scope claim_active_generations set: only the gate read it,
// and its 1-row estimate drove the #11234 loop.
func TestStaleGate11234DropsThePerScopeSet(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{
		"batch":  claimReducerWorkBatchQuery,
		"single": claimReducerWorkQuery,
		"funnel": reducerClaimBatchFunnelQuery,
	} {
		if strings.Contains(query, "claim_active_generations") {
			t.Fatalf("%s claim text still names claim_active_generations", name)
		}
	}
}
