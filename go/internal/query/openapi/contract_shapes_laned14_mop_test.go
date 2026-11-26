// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// TestContractShapesLaneD14Mop pins the lane-D14 mop-up bar: every
// 200-response property in the served spec names its shape and carries a
// description with its emitting owner. It walks the $ref closure reachable
// from 200 responses (inline schemas plus shared component targets), so a
// shape referenced but never described still fails.
//
// Verified artifacts (documented exemptions, not gaps):
//   - scalar array items ({"type": "string"} etc.): the array property
//     carries the description and owner; every lane follows this convention.
//   - $ref use sites: the shape is pinned at the shared target.
//   - request bodies, parameters, and non-200 responses: outside the
//     200-response bar (request-schema-only).
//   - scalar additionalProperties schemas: key semantics live on the parent
//     property description (genuine open maps).
//
// Each entry names the route or component schema plus the JSON path, so a
// failure points at the fragment and owner to fix.
func TestContractShapesLaneD14Mop(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	resolve := func(ref string) (map[string]any, bool) {
		const prefix = "#/components/"
		if !strings.HasPrefix(ref, prefix) {
			return nil, false
		}
		node, _ := spec["components"].(map[string]any)
		for _, part := range strings.Split(strings.TrimPrefix(ref, prefix), "/") {
			next, ok := node[part].(map[string]any)
			if !ok {
				return nil, false
			}
			node = next
		}
		return node, true
	}

	var bare []string
	var nodesc []string
	seen := map[string]bool{}
	reported := map[string]bool{}
	report := func(dst *[]string, msg string) {
		if !reported[msg] {
			reported[msg] = true
			*dst = append(*dst, msg)
		}
	}
	var visit func(node any, path string)
	visit = func(node any, path string) {
		m, ok := node.(map[string]any)
		if !ok {
			return
		}
		if ref, _ := m["$ref"].(string); ref != "" {
			if target, ok := resolve(ref); ok {
				key := ref + "@" + path
				if !seen[key] {
					seen[key] = true
					visit(target, ref)
				}
			} else {
				t.Errorf("%s: unresolvable $ref %q", path, ref)
			}
			return
		}
		if typ, _ := m["type"].(string); typ == "object" {
			_, hasProps := m["properties"]
			_, hasAddl := m["additionalProperties"]
			_, hasAll := m["allOf"]
			_, hasOne := m["oneOf"]
			_, hasAny := m["anyOf"]
			if !hasProps && !hasAddl && !hasAll && !hasOne && !hasAny {
				report(&bare, path)
			}
			if props, ok := m["properties"]; ok {
				pm, _ := props.(map[string]any)
				if len(pm) == 0 {
					report(&bare, path+" (empty properties)")
				}
			}
		}
		if props, ok := m["properties"].(map[string]any); ok {
			keys := make([]string, 0, len(props))
			for k := range props {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				pm, ok := props[k].(map[string]any)
				if !ok {
					continue
				}
				if desc, _ := pm["description"].(string); desc == "" {
					if _, isRef := pm["$ref"]; !isRef {
						report(&nodesc, path+".properties."+k)
					}
				}
				visit(pm, path+".properties."+k)
			}
		}
		if items, ok := m["items"].(map[string]any); ok {
			if it, _ := items["type"].(string); it == "object" {
				if desc, _ := items["description"].(string); desc == "" {
					if _, isRef := items["$ref"]; !isRef {
						report(&nodesc, path+".items")
					}
				}
			}
			visit(items, path+".items")
		}
		for _, comb := range []string{"allOf", "oneOf", "anyOf"} {
			if arr, ok := m[comb].([]any); ok {
				for i, e := range arr {
					visit(e, fmt.Sprintf("%s.%s[%d]", path, comb, i))
				}
			}
		}
		if addl, ok := m["additionalProperties"].(map[string]any); ok {
			visit(addl, path+".additionalProperties")
		}
	}

	paths, _ := spec["paths"].(map[string]any)
	for _, pname := range sortedKeys(paths) {
		pm, _ := paths[pname].(map[string]any)
		for _, method := range sortedKeys(pm) {
			om, ok := pm[method].(map[string]any)
			if !ok {
				continue
			}
			resp, _ := om["responses"].(map[string]any)
			okResp, ok := resp["200"].(map[string]any)
			if !ok {
				continue
			}
			content, _ := okResp["content"].(map[string]any)
			for _, ctype := range sortedKeys(content) {
				cm, _ := content[ctype].(map[string]any)
				if sch, ok := cm["schema"]; ok {
					visit(sch, pname+" "+method+" 200 "+ctype)
				}
			}
		}
	}

	sort.Strings(bare)
	sort.Strings(nodesc)
	for _, b := range bare {
		t.Errorf("bare object without properties or documented additionalProperties: %s", b)
	}
	for _, n := range nodesc {
		t.Errorf("200-response property without description: %s", n)
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
