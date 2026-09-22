// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"fmt"
	"slices"
	"strings"
)

// AdvisoryStatementMaxLen is the default per-statement truncation bound for
// [TopAdvisoryStatementReports]: long enough to identify a Cypher statement,
// short enough that a report line stays readable in a CI log (#6941).
const AdvisoryStatementMaxLen = 120

// Divergence kinds name the check that caught a difference, so the
// divergence allowlist can excuse a named dialect divergence without ever
// excusing a result disagreement, and the gate can hold execution-count
// noise advisory (see [AdvisoryKind]): "missing" (one-sided group), "results" (digest sets
// differ), "executions" (same result sets, different execution counts),
// "failures" (failed-execution counts differ), "rowcount" (same result sets
// and execution counts, different row totals).
const (
	DivergenceMissing    = "missing"
	DivergenceResults    = "results"
	DivergenceExecutions = "executions"
	DivergenceFailures   = "failures"
	DivergenceRowCount   = "rowcount"
)

// DifferentialDifference is one divergence between two recordings.
type DifferentialDifference struct {
	Fingerprint DifferentialFingerprint
	Kind        string
	Detail      string
}

// CompareRecordings diffs two recordings group by group. Groups key by
// fingerprint, except UNWIND batch statements, which explode into one group
// per batch element (see [explodeRecordGroups]): two legs slice the same
// rows into different batches run to run, so whole-batch fingerprints never
// pair and every batch would report one-sided.
//
// Inside a group the comparison is layered: differing digest SETS mean the
// backends answered differently ("results"); agreeing sets with different
// failed-execution counts mean one backend errored ("failures"); agreeing
// sets and failures with different execution counts mean scheduling noise —
// poll iterations, retries, regrouped batches ("executions"); and agreeing
// sets, failures, and counts with different row totals ("rowcount") cover
// failed-record row asymmetry. Set (not multiset) result comparison keeps a
// statement that answers differently across repeated executions from masking
// itself, while execution-count noise stays visible as its own kind instead
// of masquerading as a result divergence.
func CompareRecordings(a, b []DifferentialRecord) []DifferentialDifference {
	byGroup := func(records []DifferentialRecord) map[DifferentialFingerprint][]DifferentialRecord {
		out := make(map[DifferentialFingerprint][]DifferentialRecord)
		for _, rec := range records {
			for _, group := range explodeRecordGroups(rec.Fingerprint) {
				out[group] = append(out[group], rec)
			}
		}
		return out
	}
	left, right := byGroup(a), byGroup(b)
	digestSet := func(recs []DifferentialRecord) []string {
		seen := make(map[string]struct{}, len(recs))
		for _, rec := range recs {
			seen[rec.Digest] = struct{}{}
		}
		out := make([]string, 0, len(seen))
		for digest := range seen {
			out = append(out, digest)
		}
		slices.Sort(out)
		return out
	}
	counts := func(recs []DifferentialRecord) int {
		total := 0
		for _, rec := range recs {
			total += rec.RowCount
		}
		return total
	}
	failures := func(recs []DifferentialRecord) int {
		total := 0
		for _, rec := range recs {
			if rec.Failed {
				total++
			}
		}
		return total
	}
	// failureErrors names the distinct recorded error texts behind a
	// failures-kind divergence, so the report carries the failure itself
	// instead of only the failed-execution counts.
	failureErrors := func(groups ...[]DifferentialRecord) string {
		seen := make(map[string]struct{})
		var errs []string
		for _, recs := range groups {
			for _, rec := range recs {
				if !rec.Failed || rec.Error == "" {
					continue
				}
				if _, ok := seen[rec.Error]; ok {
					continue
				}
				seen[rec.Error] = struct{}{}
				errs = append(errs, rec.Error)
			}
		}
		if len(errs) == 0 {
			return ""
		}
		slices.Sort(errs)
		return "; errors: [" + strings.Join(errs, "; ") + "]"
	}
	backend := func(recs []DifferentialRecord) string {
		if len(recs) == 0 {
			return ""
		}
		return recs[0].Backend
	}
	var diffs []DifferentialDifference
	for fp, lrecs := range left {
		rrecs, ok := right[fp]
		if !ok {
			diffs = append(diffs, DifferentialDifference{Fingerprint: fp, Kind: DivergenceMissing, Detail: "recorded on the first backend only"})
			continue
		}
		if !slices.Equal(digestSet(lrecs), digestSet(rrecs)) {
			diffs = append(diffs, DifferentialDifference{
				Fingerprint: fp,
				Kind:        DivergenceResults,
				Detail:      fmt.Sprintf("row digest differs (%s=%d rows, %s=%d rows)", backend(lrecs), counts(lrecs), backend(rrecs), counts(rrecs)),
			})
			continue
		}
		// Writes carry no rows, so a one-sided write failure shares the
		// empty digest on both sides: the failed-execution count is part
		// of the comparison, or the slice-3 gate would miss exactly the
		// divergence it exists to catch.
		if failures(lrecs) != failures(rrecs) {
			diffs = append(diffs, DifferentialDifference{
				Fingerprint: fp,
				Kind:        DivergenceFailures,
				Detail: fmt.Sprintf("failed executions differ (%s=%d, %s=%d)%s",
					backend(lrecs), failures(lrecs), backend(rrecs), failures(rrecs), failureErrors(lrecs, rrecs)),
			})
			continue
		}
		if len(lrecs) != len(rrecs) {
			diffs = append(diffs, DifferentialDifference{
				Fingerprint: fp,
				Kind:        DivergenceExecutions,
				Detail:      fmt.Sprintf("execution count differs with agreeing results (%s=%d, %s=%d)", backend(lrecs), len(lrecs), backend(rrecs), len(rrecs)),
			})
			continue
		}
		if counts(lrecs) != counts(rrecs) {
			diffs = append(diffs, DifferentialDifference{
				Fingerprint: fp,
				Kind:        DivergenceRowCount,
				Detail:      fmt.Sprintf("row count differs (%s=%d, %s=%d) with equal digests", backend(lrecs), counts(lrecs), backend(rrecs), counts(rrecs)),
			})
		}
	}
	for fp := range right {
		if _, ok := left[fp]; !ok {
			diffs = append(diffs, DifferentialDifference{Fingerprint: fp, Kind: DivergenceMissing, Detail: "recorded on the second backend only"})
		}
	}
	slices.SortFunc(diffs, func(x, y DifferentialDifference) int {
		if x.Fingerprint.Statement != y.Fingerprint.Statement {
			return strings.Compare(x.Fingerprint.Statement, y.Fingerprint.Statement)
		}
		return strings.Compare(x.Fingerprint.Parameters, y.Fingerprint.Parameters)
	})
	return diffs
}

// AdvisoryKind reports whether a divergence kind is advisory at the gate.
// Only executions is: agreeing result sets with different execution counts
// are scheduling noise by this package's own definition (drain passes,
// retries, regrouped batches), and the two backends drain at systematically
// different speeds, so the difference reproduces across leg pairings and
// quorum cannot filter it. Excusing it statement by statement in the
// allowlist was open-ended whack-a-mole (#6782 permanent disposition,
// 2026-09-21). Row truth stays covered: results and missing on the reads
// compare final state, and failures still catches a one-sided error.
func AdvisoryKind(kind string) bool {
	return kind == DivergenceExecutions
}

// SplitAdvisory partitions diffs into the gate-failing divergences and the
// advisory ones (see [AdvisoryKind]), preserving input order in both. Nil
// in, nil out on both sides.
func SplitAdvisory(diffs []DifferentialDifference) (required, advisory []DifferentialDifference) {
	for _, diff := range diffs {
		if AdvisoryKind(diff.Kind) {
			advisory = append(advisory, diff)
			continue
		}
		required = append(required, diff)
	}
	return required, advisory
}

// TopAdvisoryStatementReports groups diffs by Fingerprint.Statement, counts
// how many divergences each statement contributed, and formats the top n
// groups as "<statement> (<count>)" — descending by count, tied broken by
// statement text ascending so the order is deterministic across runs instead
// of depending on map iteration. Each statement is bounded to maxLen runes
// by eliding its middle (maxLen <= 0 disables truncation), so one long
// Cypher statement cannot dominate a one-line gate report and statements
// sharing a long prefix keep distinct labels. n <= 0 returns
// every ranked group. Nil or empty diffs return nil (#6941: the advisory
// ceiling finding names its top offenders instead of only the first).
func TopAdvisoryStatementReports(diffs []DifferentialDifference, n, maxLen int) []string {
	if len(diffs) == 0 {
		return nil
	}
	counts := make(map[string]int, len(diffs))
	for _, d := range diffs {
		counts[d.Fingerprint.Statement]++
	}
	type statementCount struct {
		statement string
		count     int
	}
	ranked := make([]statementCount, 0, len(counts))
	for stmt, count := range counts {
		ranked = append(ranked, statementCount{statement: stmt, count: count})
	}
	slices.SortFunc(ranked, func(x, y statementCount) int {
		if x.count != y.count {
			return y.count - x.count
		}
		return strings.Compare(x.statement, y.statement)
	})
	if n > 0 && len(ranked) > n {
		ranked = ranked[:n]
	}
	out := make([]string, 0, len(ranked))
	for _, sc := range ranked {
		out = append(out, fmt.Sprintf("%s (%d)", truncateStatement(sc.statement, maxLen), sc.count))
	}
	return out
}

// truncateStatement bounds s to maxLen runes by eliding the middle
// ("head...tail") so two statements that share a long prefix -- the corpus
// has one 120-rune prefix shared by 115 distinct UNWIND statements -- still
// print as distinct labels. Below 8 runes there is no room for a tail and
// the head is kept with a "..." suffix. maxLen <= 0 disables truncation.
func truncateStatement(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	if maxLen < 8 {
		return string(r[:maxLen]) + "..."
	}
	head := maxLen * 2 / 3
	tail := maxLen - head
	return string(r[:head]) + "..." + string(r[len(r)-tail:])
}

// quorumKey identifies one divergence across leg pairings: the same
// fingerprint diverging in the same way. Counts and error texts legitimately
// vary run to run, so Detail is not part of the key; a kind flip between
// pairings (results here, executions there) is a different phenomenon, not
// a reproduction.
type quorumKey struct {
	fingerprint DifferentialFingerprint
	kind        string
}

// QuorumIntersection keeps the divergences that reproduce across two leg
// pairings (#6782 multi-leg quorum): a divergence fails the gate only when
// both pairings report it under the same fingerprint and kind. Pairing-local
// noise — drain timing, retries, regrouped batches — drops out, while a
// systematic backend divergence reproduces and still fails. The kept Detail
// comes from the first pairing; output order matches CompareRecordings.
func QuorumIntersection(first, second []DifferentialDifference) []DifferentialDifference {
	inSecond := make(map[quorumKey]struct{}, len(second))
	for _, diff := range second {
		inSecond[quorumKey{fingerprint: diff.Fingerprint, kind: diff.Kind}] = struct{}{}
	}
	var kept []DifferentialDifference
	for _, diff := range first {
		if _, ok := inSecond[quorumKey{fingerprint: diff.Fingerprint, kind: diff.Kind}]; ok {
			kept = append(kept, diff)
		}
	}
	slices.SortFunc(kept, func(x, y DifferentialDifference) int {
		if x.Fingerprint.Statement != y.Fingerprint.Statement {
			return strings.Compare(x.Fingerprint.Statement, y.Fingerprint.Statement)
		}
		if x.Fingerprint.Parameters != y.Fingerprint.Parameters {
			return strings.Compare(x.Fingerprint.Parameters, y.Fingerprint.Parameters)
		}
		return strings.Compare(x.Kind, y.Kind)
	})
	return kept
}
