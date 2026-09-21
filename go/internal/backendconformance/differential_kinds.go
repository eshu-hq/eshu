// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"fmt"
	"slices"
	"strings"
)

// Divergence kinds name the check that caught a difference, so the
// divergence allowlist can excuse scheduling noise without ever excusing a
// result disagreement: "missing" (one-sided group), "results" (digest sets
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
