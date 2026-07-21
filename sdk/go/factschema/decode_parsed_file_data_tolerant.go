// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"log/slog"
	"reflect"
)

// decodeParsedFileDataTolerantSlice decodes a raw parsed_file_data slice value
// into a typed slice, SKIPPING (never erroring on) any element that is not a
// JSON object or whose fields do not coerce into T, and returning ok=false
// only when raw itself is not any recognized slice shape at all ([]map[string]any
// or []any).
//
// This is a deliberately more tolerant contract than asObjectSlice's
// abort-on-first-malformed-element behavior (decode_parsed_file_data.go),
// which the #4750 S1 / issue #5440 accessors above use. Those accessors had
// no real production caller yet when they were written, so choosing
// observability (abort + surface an error) over silent tolerance cost
// nothing. The issue #5445 slice 1 accessors that use this helper
// (terraform_modules, terragrunt_dependencies, terragrunt_configs,
// helm_charts, helm_values, argocd_applications, argocd_applicationsets,
// flux_git_repositories) migrate REAL production call sites in
// go/internal/relationships whose pre-typing raw-map read explicitly
// tolerated one malformed row in an otherwise-good multi-row bucket: every
// one of those call sites does `item, ok := raw.(map[string]any); if !ok {
// continue }` with no per-field type validation at all (payloadString/
// csvValues silently return a zero value for a wrong-typed field, never an
// error). Aborting the WHOLE decode because one Helm chart or one ArgoCD
// Application entry is malformed would drop every OTHER well-formed row in
// the same file -- a real accuracy regression this helper exists to avoid.
//
// A skipped element is otherwise invisible: (out, true) with a len-0 out is
// indistinguishable from "this file legitimately has no rows for this
// bucket." Issue #5445 finding 2: a producer regression that starts emitting
// a malformed element for one bucket would silently degrade that bucket's
// evidence to zero with no operator signal. When at least one element is
// skipped, this helper emits one slog.Debug record naming the decoded
// element type, the total element count, and the skipped count, so an
// operator grepping logs (or a future metric reader) can tell "zero rows,
// evaluated" apart from "zero rows, quietly dropped." The common
// nothing-skipped path stays silent.
func decodeParsedFileDataTolerantSlice[T any](raw any) ([]T, bool) {
	var items []any
	switch v := raw.(type) {
	case []map[string]any:
		items = make([]any, len(v))
		for i, m := range v {
			items[i] = m
		}
	case []any:
		items = v
	default:
		return nil, false
	}

	out := make([]T, 0, len(items))
	skipped := 0
	for _, item := range items {
		obj, ok := asObjectMap(item)
		if !ok {
			skipped++
			continue
		}
		var decoded T
		if err := decodeMapInto(obj, &decoded); err != nil {
			skipped++
			continue
		}
		out = append(out, decoded)
	}
	if skipped > 0 {
		// Warn, not Debug: the only producer of these buckets is the parser's
		// AppendBucket, which always emits well-formed objects, so a skipped
		// element means a producer regression rather than routine input noise.
		// Debug is silent at the default Info level, which would leave an
		// operator unable to tell a degraded bucket from an empty one -- the
		// same absence-vs-not-evaluated ambiguity this decode path exists to
		// surface. One record per decode call, never per element, so a
		// systematically malformed bucket cannot flood the log.
		slog.Warn("factschema: parsed_file_data tolerant decode skipped malformed element(s)",
			"element_type", reflect.TypeFor[T]().String(),
			"total_elements", len(items),
			"skipped_elements", skipped,
			"decoded_elements", len(out),
		)
	}
	return out, true
}
