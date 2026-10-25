// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"sort"
	"strings"
)

// The oracle reads only the generator's model, never the XML or the parser's
// code. It states what each row must say about properties, versions, and the
// item operations that demote a row.

// fuzzOracle is the independent expectation for property names.
type fuzzOracle struct {
	values  map[string]string
	differ  map[string]bool
	anyCond map[string]bool
	seen    map[string]bool
}

func newFuzzOracle(decls []fuzzDecl) fuzzOracle {
	o := fuzzOracle{map[string]string{}, map[string]bool{}, map[string]bool{}, map[string]bool{}}
	for _, d := range decls {
		key := strings.ToLower(d.name)
		if o.seen[key] && o.values[key] != d.value {
			o.differ[key] = true
		}
		if !o.seen[key] {
			o.values[key], o.seen[key] = d.value, true
		}
		if d.condition || d.group {
			o.anyCond[key] = true
		}
	}
	return o
}

// state returns "resolved", "ambiguous", or "unresolved" for a reference.
func (o fuzzOracle) state(name string) string {
	key := strings.ToLower(name)
	switch {
	case !o.seen[key]:
		return "unresolved"
	case o.differ[key] || o.anyCond[key]:
		return "ambiguous"
	case o.values[key] == "" || strings.Contains(o.values[key], "$(") || fuzzEscaped(o.values[key]):
		return "unresolved"
	}
	return "resolved"
}

func (o fuzzOracle) expect(parts []fuzzPart) (value string, evidence string) {
	if len(parts) == 0 {
		return "", "missing_version"
	}
	var b strings.Builder
	ambiguous, unresolved, list := false, false, false
	hasRef := false
	for _, part := range parts {
		switch part.kind {
		case 0:
			b.WriteString(part.text)
		case 1:
			hasRef = true
			switch o.state(part.text) {
			case "ambiguous":
				ambiguous = true
			case "unresolved":
				unresolved = true
			default:
				b.WriteString(o.values[strings.ToLower(part.text)])
			}
		case 2:
			unresolved = true
		default:
			list = true
		}
	}
	raw := renderParts(parts)
	switch {
	case list:
		return raw, "unresolved_msbuild_property"
	case ambiguous:
		return raw, "ambiguous_msbuild_property"
	case unresolved:
		return raw, "unresolved_msbuild_property"
	case hasRef:
		return b.String(), "project_property"
	}
	return raw, "package_reference"
}

// centralUnproven: the file sets ManagePackageVersionsCentrally to anything
// but one unconditional literal true (case-insensitive).
func (o fuzzOracle) centralUnproven() bool {
	const name = "ManagePackageVersionsCentrally"
	if !o.seen[strings.ToLower(name)] {
		return false
	}
	return o.state(name) != "resolved" || !strings.EqualFold(o.values[strings.ToLower(name)], "true")
}

// fuzzReasons returns the sorted item_ambiguity reasons MSBuild semantics give
// the item at position p of the document model.
func fuzzReasons(doc fuzzModel, oracle fuzzOracle, p int) string {
	it := doc.items[p]
	set := map[string]bool{}
	if it.condChild || it.privateCond || it.includeCond {
		set["conditional_metadata"] = true
	}
	if fuzzScope(it) == "" {
		set["unresolved_asset_metadata"] = true
	}
	if it.exclude {
		set["exclude"] = true
	}
	if doc.unevaluated {
		set["unevaluated_element"] = true
	}
	if it.kind != "PackageReference" && oracle.centralUnproven() {
		set["central_management_unproven"] = true
	}
	for q, other := range doc.items {
		if other.kind != it.kind {
			continue
		}
		if other.literal && q != p && strings.EqualFold(other.name, it.name) {
			set["duplicate_include"] = true
		}
		if other.unresolvedSpec {
			set["unresolved_item_spec"] = true
		}
		if q > p && other.op != "" && (other.opWild || strings.EqualFold(other.opTarget, it.name)) {
			set[map[string]string{"update": "updated", "remove": "removed"}[other.op]] = true
		}
	}
	reasons := make([]string, 0, len(set))
	for reason := range set {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	return strings.Join(reasons, ",")
}
