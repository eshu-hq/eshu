// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import "sort"

// seamIdentity is the semantic identity BuildManifest's one-entry-per-fact-kind
// invariant is keyed on: which fact kind, decoded into which typed struct. Two
// DecodeSeam values sharing a seamIdentity decode the SAME fact kind the SAME
// way, even when reached through different FuncName call sites (see
// mergeSeamsByIdentity).
type seamIdentity struct {
	factKind        string
	qualifiedStruct string
}

// mergeSeamsByIdentity collapses seams sharing one seamIdentity into a single
// representative seam per fact kind, and returns a usage map keyed by each
// representative's FuncName whose slice is the union of every colliding
// seam's recorded usage.
//
// mergeSeams (load.go) already dedupes an exact FuncName collision across
// surfaces, but two independent decode functions sometimes decode the same
// fact kind into the same typed struct under DIFFERENT FuncNames — e.g. the
// reducer's schemadecode.DecodeIncidentRecord and the query read model's own
// local decodeIncidentRecord wrapper both decode FactKindIncidentRecord into
// incidentv1.IncidentRecord. Before a family's decode seam moves into
// schemadecode with no root-level forwarder left behind (the schemadecode
// migration's documented convention, #6372/#6383), a reducer handler calls
// the seam through an unqualified root-local name that, by long-standing
// convention, happens to match the other surface's own local name, so
// mergeSeams' exact-FuncName dedup merges them for free. Once the reducer
// call site becomes the schemadecode-qualified name (the shape a family
// package leaves behind once its seam relocates into its own subpackage,
// e.g. #6061's incident family move into internal/reducer/incident), that
// coincidence breaks: the two FuncNames diverge, mergeSeams no longer
// recognizes them as one seam, and BuildManifest — which produces one
// KindManifest per surviving seam — emits two entries for one fact kind.
// This function makes the fact-kind-level merge explicit and identity-based
// so it holds for every future family move (sbomattest, cicdrun, codetaint,
// ...) regardless of what either surface happens to name its call site.
//
// Among colliding seams, the one with the MOST recorded usage is kept as the
// manifest's DecodeFunc: it is the more informative identity to report (the
// one whose real field reads are actually captured), and ties — including
// the common case of two seams with zero usage each — break on the
// lexicographically smallest FuncName, so the choice is deterministic
// regardless of Go map iteration order or which surface's glob ran first.
func mergeSeamsByIdentity(seams []DecodeSeam, usage map[string][]FieldUsage) ([]DecodeSeam, map[string][]FieldUsage) {
	var order []seamIdentity
	groups := map[seamIdentity][]DecodeSeam{}
	for _, s := range seams {
		id := seamIdentity{factKind: s.FactKindConst, qualifiedStruct: s.QualifiedStruct()}
		if _, seen := groups[id]; !seen {
			order = append(order, id)
		}
		groups[id] = append(groups[id], s)
	}

	merged := make([]DecodeSeam, 0, len(order))
	mergedUsage := make(map[string][]FieldUsage, len(order))
	for _, id := range order {
		group := append([]DecodeSeam(nil), groups[id]...)
		sort.Slice(group, func(i, j int) bool { return group[i].FuncName < group[j].FuncName })

		canonical := group[0]
		canonicalCount := len(usage[canonical.FuncName])
		var union []FieldUsage
		for _, s := range group {
			su := usage[s.FuncName]
			union = append(union, su...)
			if len(su) > canonicalCount {
				canonical = s
				canonicalCount = len(su)
			}
		}

		merged = append(merged, canonical)
		if len(union) > 0 {
			mergedUsage[canonical.FuncName] = union
		}
	}
	return merged, mergedUsage
}
