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
// single digests were re-pinned when #10880 PR-B2 enrolled the
// supply_chain_impact_supports_not_ready class, and again when #10880 PR-C0
// enrolled supply_chain_impact_fact_write_stopped, and again when #11327 enrolled
// supply_chain_impact_go_evidence_changed: the claim statements embed the
// non-counting class list (reducerClaimAttemptCountCaseSQL), so each added
// predicate moved their bytes outside the gate; the funnel carries no class list
// and keeps its base digest. Enrolling another non-counting class re-pins the two
// the same way. The batch digest was re-pinned again for #11366, which adds a
// second UNION arm to gate_waited_generations (older undrained baseline
// windows): the arm sits outside the stale gate by design, and the re-pin
// records exactly that reviewed movement and nothing else. The batch digest was
// re-pinned once more when #11327 rebased onto the post-#11366 main: the batch
// statement then carried both the second UNION arm and the
// supply_chain_impact_go_evidence_changed class, while the single statement
// carried only the class (the arm is batch-only), so the single digest moved
// from 8aa7 to 2f84 for exactly that enrollment and nothing else. Both the
// batch and the single digests move again for #11615: the claim SELECTs
// carry work.last_requeue_reason read-only (outer SELECT included), and
// nothing else outside the gate changes.
const (
	staleGate11234PreGateSHA    = "30a3fd23ee77b417d8c6cd5a981dfae7953c30749f8552953f41b766a38b3aeb"
	staleGate11234BaseBatchSHA  = "46a984126c67371ed10705b258c6e38ea6cff9d55713a4d959908bd607db74ac"
	staleGate11234BaseSingleSHA = "d56ee1b628b78f6287c3907d29270a200e63662d6e878622a13e39370cfe93eb"
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
// body is the base body with the #11234 join-order fence (FROM stale_gate_rows,
// CROSS JOIN LATERAL ... OFFSET 0 carrying the base active-generation ON text
// verbatim via LEFT JOIN for #11703) and the #11703 NULL-active retired-scope
// generation-guard arm (strictly gated on stale_generation.status = 'superseded',
// wrapping the base active-generation guard + readiness block verbatim). Any
// other difference fails.
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
	const (
		preGenStart  = "      AND stale.generation_id <> scope.active_generation_id"
		preGenEnd    = "      AND NOT ("
		gateGenStart = "      AND (\n        (\n          scope.present_active_generation_id IS NOT NULL\n          AND stale.generation_id <> scope.active_generation_id"
		gateNullArm  = "\n        )\n        OR (\n          scope.active_generation_id IS NULL\n          AND stale_generation.status = 'superseded'\n        )\n      )\n"
		preFrom      = "    FROM fact_work_items AS stale"
		gateFrom     = "    FROM stale_gate_rows AS stale"
		preJoin      = "    JOIN claim_active_generations AS scope ON scope.scope_id = stale.scope_id"
		gateOpen     = "    CROSS JOIN LATERAL ("
		gateClose    = "    ) AS scope"
	)
	// Strip the #11703 wrapper from gateBody so the base active-generation guard
	// and readiness block compare line-for-line (trimmed) against preBody, while
	// pinning the exact NULL-active 'superseded' arm.
	gHead, gRest, ok := strings.Cut(gateBody, "\n"+gateGenStart+"\n")
	if !ok {
		return fmt.Errorf("shipped body missing #11703 generation guard wrapper")
	}
	gWrapped, gTail, ok := strings.Cut(gRest, gateNullArm)
	if !ok {
		return fmt.Errorf("shipped body missing #11703 NULL-active superseded arm")
	}
	pHead, pRest, ok := strings.Cut(preBody, "\n"+preGenStart+"\n")
	if !ok {
		return fmt.Errorf("base body missing generation guard start")
	}
	pWrapped, pTail, ok := strings.Cut(pRest, "\n"+preGenEnd+"\n")
	if !ok {
		return fmt.Errorf("base body missing fan-out debt spare anchor")
	}
	// Compare the wrapped active-generation + readiness lines (ignoring the extra
	// indentation introduced by the #11703 OR-wrapper).
	if strings.Join(staleGate11234TrimmedLines(gWrapped), "\n") != strings.Join(staleGate11234TrimmedLines(pWrapped), "\n") {
		return fmt.Errorf("wrapped active-generation/readiness predicate differs from base")
	}
	// Tail (fan-out debt spare + Crossplane carrier) must remain byte-identical.
	if gTail != preGenEnd+"\n"+pTail {
		return fmt.Errorf("shipped body after generation guard differs from base")
	}
	// Head (SELECT, FROM, LATERAL lookup, stale_generation JOIN, stage/status/domain).
	preLines := strings.Split(pHead, "\n")
	gateLines := strings.Split(gHead, "\n")
	replaced := 0
	j := 0
	for i := 0; i < len(preLines); i++ {
		if j >= len(gateLines) {
			return fmt.Errorf("shipped body head ends early at base line %d", i)
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
				"active_generation.generation_id AS present_active_generation_id,",
				"active_generation.ingested_at AS active_ingested_at",
				"FROM ingestion_scopes AS scope",
				"LEFT JOIN scope_generations AS active_generation",
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
		return fmt.Errorf("shipped body head has %d extra lines", len(gateLines)-j)
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
		"null active failed":    {"AND stale_generation.status = 'superseded'", "AND stale_generation.status IN ('superseded', 'failed')"},
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
		"AND work.generation_id IS DISTINCT FROM work_scope.active_generation_id",
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
