// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oci

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// MaxKeysPerStatement is the per-statement IN-list bound the OCI
// registry-truth reads are recorded against in
// go/internal/queryplan/testdata/query-source-coverage.yaml
// (bounded_key_batch, max_keys: 250).
//
// The bound used to be an assumption rather than a property of the code
// (#6590). The keys are deduplicated upstream but their COUNT is not capped:
// they come from a workload row set capped at ServiceStoryItemLimit rows, but
// a single workload can declare any number of containers and initContainers,
// so 50 workloads with six images each already puts 300 keys into one
// IN-list. Enforcing it here makes the recorded bound true by construction.
//
// Batching rather than truncating is deliberate: a truncated key set would
// silently drop images from the deployment trace, which is an accuracy loss.
// Capping at the source is also wrong -- collectContainerImages lives in the
// YAML parser, and a cap there would discard facts at ingest.
const MaxKeysPerStatement = 250

// RegistryTruthFanOut declares the per-key row multiplicity the OCI
// registry-truth statements are bounded against (#6590): an image_ref or
// digest key is expected to produce at most this many rows. It is 3x the
// multiplicity measured on in-tree corpora and ops-qa (1 observation/image
// per key), matching the taghistory.BuiltFromMaxRows precedent
// (go/internal/query/taghistory/builtfrom.go) of declaring headroom above a
// measured worst case rather than picking a number. Tag multiplicity per ref
// is by design (the writer MERGEs on (repository_id, tag, resolved_digest)),
// so a higher multiplicity is expected input, not a bug, and must be
// disclosed rather than silently dropped.
const RegistryTruthFanOut = 3

// RegistryTruthRowLimit is the LIMIT $row_limit bound on every OCI
// registry-truth statement in go/internal/query/impact/trace_deployment_oci.go
// (ociTagObservationByRefCypher and ociImageByDigestCypher):
// MaxKeysPerStatement keys per statement, each bounded to RegistryTruthFanOut
// rows. Before #6590 these statements carried ORDER BY only, no LIMIT, so the
// declared per-key bound in
// go/internal/queryplan/testdata/query-source-coverage.yaml was not actually
// enforced by anything in code.
const RegistryTruthRowLimit = MaxKeysPerStatement * RegistryTruthFanOut

// ReasonTagObservationRowLimit and ReasonImageRowLimit are the two
// telemetry.AttrReason values eshu_dp_query_oci_registry_truth_truncated_total
// is labeled with: which statement shape hit RegistryTruthRowLimit.
const (
	ReasonTagObservationRowLimit = "tag_observation_row_limit"
	ReasonImageRowLimit          = "image_row_limit"
)

// RegistryTruthRowLimitReason is the machine-readable reason paired with
// image_registry_truth_complete=false in a trace_deployment_chain response's
// image_registry_truth_limits block.
const RegistryTruthRowLimitReason = "oci_registry_truth_row_limit_reached"

// RegistryTruthResult is the disclosed-bound outcome of one bounded OCI
// registry-truth read (impact.FetchOCIImageRegistryTruthResult): the resolved
// truth rows, the image refs withheld because a statement's LIMIT $row_limit
// was reached for their key (directly, for a digest-addressed ref or an
// overflowing tag ref, or transitively, for a tag ref one of whose
// observations resolved to a withheld digest), and the disclosure limits
// block for the trace_deployment_chain response's image_registry_truth_limits
// field. A withheld ref never gets a placeholder row in Rows;
// TruncatedImageRefs names it instead.
type RegistryTruthResult struct {
	Rows               []map[string]any
	TruncatedImageRefs []string
	Limits             map[string]any
}

// RegistryTruthLimits builds the image_registry_truth_limits disclosure block
// for one bounded OCI registry-truth read. truncated_image_refs and
// image_registry_truth_incomplete_reason are added only when the read was
// incomplete; the other four keys are always present.
func RegistryTruthLimits(truncatedRefs []string) map[string]any {
	complete := len(truncatedRefs) == 0
	limits := map[string]any{
		"max_keys_per_statement":        MaxKeysPerStatement,
		"statement_row_limit":           RegistryTruthRowLimit,
		"image_registry_truth_complete": complete,
		"truncated_image_ref_count":     len(truncatedRefs),
	}
	if !complete {
		limits["truncated_image_refs"] = truncatedRefs
		limits["image_registry_truth_incomplete_reason"] = RegistryTruthRowLimitReason
	}
	return limits
}

// AdvanceBoundedRead is the pure per-statement continuation step for an OCI
// registry-truth bounded read (#6590). keys is the sorted, deduplicated key
// set just queried by one Run call (one statement's IN-list); rows is that
// statement's returned rows -- sorted ascending by keyField, which every OCI
// registry-truth statement's ORDER BY guarantees; limit is the statement's
// LIMIT $row_limit. It issues no Run call itself: each fetcher in
// go/internal/query/impact/trace_deployment_oci.go keeps its own statement
// and its own call site, so
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
func AdvanceBoundedRead(
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

// SortUniqueStrings returns the sorted, deduplicated, non-empty values of
// values. AdvanceBoundedRead's irreducible-overflow check (last == keys[0])
// requires a batch's first key to be its minimum, and distinctFieldValues
// (go/internal/query/impact/blast_radius_rows.go) is insertion-ordered rather
// than sorted, so every OCI bounded-read caller normalizes its key set
// through this helper before batching.
func SortUniqueStrings(values []string) []string {
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

// KeyBatches splits keys into consecutive batches of at most
// MaxKeysPerStatement, preserving order. It is pure: it issues no statement.
//
// That is deliberate. The query-plan registry
// (go/internal/queryplan/testdata/query-source-coverage.yaml) attributes a
// graph read to the function that calls Run, and records a separate bound for
// each OCI registry-truth read. A shared helper that called Run itself would
// collapse differently-bounded queries into one anonymous callsite and erase
// the per-query audit this bound exists for. So each fetcher in
// go/internal/query/impact/trace_deployment_oci.go keeps its own Run, looped
// over these batches, and remains its own registered callsite.
func KeyBatches(keys []string) [][]string {
	if len(keys) == 0 {
		return nil
	}
	batches := make([][]string, 0, (len(keys)+MaxKeysPerStatement-1)/MaxKeysPerStatement)
	for start := 0; start < len(keys); start += MaxKeysPerStatement {
		batches = append(batches, keys[start:min(start+MaxKeysPerStatement, len(keys))])
	}
	return batches
}
