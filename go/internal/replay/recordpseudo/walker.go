// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"sort"
	"strings"
)

// safeTagKeys are well-known tag keys that carry no organisation data and
// are kept so a recording stays readable; every other tag key is a customer
// string and is pseudonymized like a tag value.
var safeTagKeys = map[string]struct{}{
	"Name": {}, "Environment": {}, "Owner": {}, "Team": {}, "Project": {}, "Service": {},
	"Application": {}, "Component": {}, "Version": {}, "CostCenter": {}, "ManagedBy": {},
	"Terraform": {}, "Stage": {}, "Tier": {}, "Role": {}, "Purpose": {}, "env": {}, "app": {},
}

func safeTagKey(key string) bool {
	if strings.HasPrefix(key, "aws:") {
		return true
	}
	_, ok := safeTagKeys[key]
	return ok
}

// walker applies the policy to a payload tree, in one of two passes: learn
// (fill the dictionary from every classified string) and rewrite (produce
// the pseudonymized copy, making unclassified strings opaque and recording
// their paths).
type walker struct {
	policy       Policy
	dict         *dictionary
	opaque       map[string]int
	unclassified map[string]int
}

func newWalker(policy Policy, dict *dictionary) *walker {
	return &walker{policy: policy, dict: dict, opaque: map[string]int{}, unclassified: map[string]int{}}
}

// learn walks the value under key (class taken from the policy, or inherited
// when the parent is a tag map) and feeds every classified string to the
// dictionary. Map children are visited in sorted key order: with class
// precedence in the dictionary this makes the learned table, and so the
// output, independent of map iteration.
func (w *walker) learn(key string, value any, inherited Class) {
	class := w.classFor(key, inherited)
	switch v := value.(type) {
	case string:
		w.dict.learn(class, v)
	case []any:
		for _, element := range v {
			w.learn(key, element, inherited)
		}
	case map[string]any:
		for _, childKey := range sortedMapKeys(v) {
			child := v[childKey]
			if class == ClassTagValue {
				if !safeTagKey(childKey) {
					w.dict.learn(ClassTagValue, childKey)
				}
				w.learn(childKey, child, ClassTagValue)
				continue
			}
			w.learn(childKey, child, ClassUnknown)
		}
	case map[string]string:
		for _, childKey := range sortedMapKeys(v) {
			child := v[childKey]
			if class == ClassTagValue && !safeTagKey(childKey) {
				w.dict.learn(ClassTagValue, childKey)
			}
			w.learn(childKey, child, class)
		}
	}
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// classFor resolves the class of a key: an inherited tag-value class wins
// for the entries of a tags map, otherwise the policy table decides.
func (w *walker) classFor(key string, inherited Class) Class {
	if inherited == ClassTagValue {
		return ClassTagValue
	}
	return w.policy.classOf(key)
}

// rewrite returns the pseudonymized copy of value. path names the field for
// the report ("payload.attributes.containers[].runtime_id"); it never carries
// a value.
func (w *walker) rewrite(path, key string, value any, inherited Class) any {
	class := w.classFor(key, inherited)
	switch v := value.(type) {
	case string:
		return w.rewriteString(path, class, v)
	case []any:
		out := make([]any, len(v))
		for i, element := range v {
			out[i] = w.rewrite(path+"[]", key, element, inherited)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for childKey, child := range v {
			childInherited := ClassUnknown
			outKey := childKey
			if class == ClassTagValue {
				childInherited = ClassTagValue
				outKey = w.dict.rewrite(childKey, true)
			}
			out[outKey] = w.rewrite(path+"."+childKey, childKey, child, childInherited)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(v))
		for childKey, child := range v {
			outKey := childKey
			if class == ClassTagValue {
				outKey = w.dict.rewrite(childKey, true)
			}
			out[outKey], _ = w.rewrite(path+"."+childKey, childKey, child, class).(string)
		}
		return out
	default:
		return value
	}
}

func (w *walker) rewriteString(path string, class Class, v string) string {
	switch class {
	case ClassUnknown:
		w.unclassified[path]++
		w.opaque[path]++
		return w.opaqueValue(v)
	case ClassOpaque:
		w.opaque[path]++
		return w.opaqueValue(v)
	case ClassEnum:
		if !customerTypeName(v) {
			return v
		}
		return w.dict.rewrite(v, true)
	case ClassKeep:
		// Keep values are substituted by substitutable tokens only: an
		// exact-only short or numeric token never rewrites one.
		return w.dict.rewrite(v, false)
	default:
		return w.dict.rewrite(v, true)
	}
}

func (w *walker) opaqueValue(v string) string {
	if strings.TrimSpace(v) == "" {
		return v
	}
	return "o" + w.dict.key.hexOf(v, 11)
}
