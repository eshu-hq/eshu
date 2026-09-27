// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ociRegistryTruthFanOut declares the per-key row multiplicity the OCI
// registry-truth statements are bounded against (#6590): an image_ref or
// digest key is expected to produce at most this many rows. It is 3x the
// multiplicity measured on in-tree corpora and ops-qa (1 observation/image
// per key), matching the taghistory.BuiltFromMaxRows precedent
// (go/internal/query/taghistory/builtfrom.go) of declaring headroom above a
// measured worst case rather than picking a number. Tag multiplicity per ref
// is by design (the writer MERGEs on (repository_id, tag, resolved_digest)),
// so a higher multiplicity is expected input, not a bug, and must be
// disclosed rather than silently dropped.
const ociRegistryTruthFanOut = 3

// ociRegistryTruthRowLimit is the LIMIT $row_limit bound on every OCI
// registry-truth statement (ociTagObservationByRefCypher and
// ociImageByDigestCypher): ociMaxKeysPerStatement keys per statement, each
// bounded to ociRegistryTruthFanOut rows. Before #6590 these statements
// carried ORDER BY only, no LIMIT, so the declared per-key bound in
// go/internal/queryplan/testdata/query-source-coverage.yaml was not actually
// enforced by anything in code.
const ociRegistryTruthRowLimit = ociMaxKeysPerStatement * ociRegistryTruthFanOut

// ociReasonTagObservationRowLimit and ociReasonImageRowLimit are the two
// telemetry.AttrReason values eshu_dp_query_oci_registry_truth_truncated_total
// is labeled with: which statement shape hit ociRegistryTruthRowLimit.
const (
	ociReasonTagObservationRowLimit = "tag_observation_row_limit"
	ociReasonImageRowLimit          = "image_row_limit"
)

// ociRegistryTruthRowLimitReason is the machine-readable reason paired with
// image_registry_truth_complete=false in a trace_deployment_chain response's
// image_registry_truth_limits block.
const ociRegistryTruthRowLimitReason = "oci_registry_truth_row_limit_reached"

// OCIImageRegistryTruthResult is the disclosed-bound outcome of one bounded
// OCI registry-truth read (FetchOCIImageRegistryTruthResult): the resolved
// truth rows, the image refs withheld because a statement's LIMIT $row_limit
// was reached for their key (directly, for a digest-addressed ref or an
// overflowing tag ref, or transitively, for a tag ref one of whose
// observations resolved to a withheld digest), and the disclosure limits
// block for the trace_deployment_chain response's
// image_registry_truth_limits field. A withheld ref never gets a
// placeholder row in Rows; TruncatedImageRefs names it instead.
type OCIImageRegistryTruthResult struct {
	Rows               []map[string]any
	TruncatedImageRefs []string
	Limits             map[string]any
}

// ociRegistryTruthLimits builds the image_registry_truth_limits disclosure
// block for one bounded OCI registry-truth read. truncated_image_refs and
// image_registry_truth_incomplete_reason are added only when the read was
// incomplete; the other four keys are always present.
func ociRegistryTruthLimits(truncatedRefs []string) map[string]any {
	complete := len(truncatedRefs) == 0
	limits := map[string]any{
		"max_keys_per_statement":        ociMaxKeysPerStatement,
		"statement_row_limit":           ociRegistryTruthRowLimit,
		"image_registry_truth_complete": complete,
		"truncated_image_ref_count":     len(truncatedRefs),
	}
	if !complete {
		limits["truncated_image_refs"] = truncatedRefs
		limits["image_registry_truth_incomplete_reason"] = ociRegistryTruthRowLimitReason
	}
	return limits
}

// advanceOCIBoundedRead is the pure per-statement continuation step for an
// OCI registry-truth bounded read (#6590). keys is the sorted, deduplicated
// key set just queried by one Run call (one statement's IN-list); rows is
// that statement's returned rows -- sorted ascending by keyField, which every
// OCI registry-truth statement's ORDER BY guarantees; limit is the
// statement's LIMIT $row_limit. It issues no Run call itself: each fetcher
// keeps its own statement and its own call site, so
// go/internal/queryplan/testdata/query-source-coverage.yaml keeps
// attributing rows to the fetcher, not to this shared helper.
//
// len(rows) < limit means the statement was not cut off: every key in this
// batch returned every row it has. Keep every row; nothing to retry.
//
// len(rows) == limit means the statement WAS cut off. Because rows are
// sorted ascending by key, every row whose key is strictly less than the
// LAST row's key is guaranteed complete: the read reached and returned at
// least one row for a later key, which is only possible once every earlier
// key's rows were already exhausted. Those rows are kept; the last key's
// rows are dropped because they are not yet known complete.
//
// If the last key already IS the first key in this batch, every returned row
// shares that one key: it alone produced at least `limit` rows, and a
// same-sized retry can never tell whether it has exactly `limit` rows or
// many more -- an irreducible overflow. It is reported as truncated (never
// resolved, never given a placeholder row) and dropped entirely from the
// retry set, which still makes progress (that one key is removed for good).
//
// Otherwise the retry set is every key from the last key onward (keys >=
// last): progress is guaranteed because at least the keys strictly before
// last are removed.
func advanceOCIBoundedRead(
	keys []string,
	rows []map[string]any,
	keyField string,
	limit int,
) (kept []map[string]any, truncatedKeys []string, next []string) {
	if len(rows) < limit {
		return rows, nil, nil
	}
	last := querycontract.StringVal(rows[len(rows)-1], keyField)
	if len(keys) > 0 && last == keys[0] {
		return nil, []string{last}, keysStrictlyAfter(keys, last)
	}
	kept = make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if querycontract.StringVal(row, keyField) == last {
			continue
		}
		kept = append(kept, row)
	}
	return kept, nil, keysFromInclusive(keys, last)
}

// keysStrictlyAfter returns the suffix of the sorted slice keys whose values
// are strictly greater than last.
func keysStrictlyAfter(keys []string, last string) []string {
	for i, key := range keys {
		if key > last {
			return keys[i:]
		}
	}
	return nil
}

// keysFromInclusive returns the suffix of the sorted slice keys whose values
// are greater than or equal to from.
func keysFromInclusive(keys []string, from string) []string {
	for i, key := range keys {
		if key >= from {
			return keys[i:]
		}
	}
	return nil
}

// sortUniqueStrings returns the sorted, deduplicated, non-empty values of
// values. advanceOCIBoundedRead's irreducible-overflow check (last ==
// keys[0]) requires a batch's first key to be its minimum, and
// distinctFieldValues (blast_radius_rows.go) is insertion-ordered rather than
// sorted, so every OCI bounded-read caller normalizes its key set through
// this helper before batching.
func sortUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
