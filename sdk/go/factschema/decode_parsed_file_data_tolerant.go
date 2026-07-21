// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

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
	for _, item := range items {
		obj, ok := asObjectMap(item)
		if !ok {
			continue
		}
		var decoded T
		if err := decodeMapInto(obj, &decoded); err != nil {
			continue
		}
		out = append(out, decoded)
	}
	return out, true
}
