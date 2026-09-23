// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"sort"
	"strings"
)

// tokens returns the learned raw tokens longest first (ties lexicographic),
// so a composite is replaced before any component it contains.
func (d *dictionary) tokens() []string {
	if d.sorted != nil {
		return d.sorted
	}
	out := make([]string, 0, len(d.entries))
	for raw := range d.entries {
		out = append(out, raw)
	}
	sort.Slice(out, func(a, b int) bool {
		if len(out[a]) != len(out[b]) {
			return len(out[a]) > len(out[b])
		}
		return out[a] < out[b]
	})
	d.sorted = out
	return out
}

// substitute rewrites every dictionary token in s on alphanumeric
// boundaries, longest token first. A token adjacent to another letter or
// digit is left alone so "app" never rewrites "application".
func (d *dictionary) substitute(s string) string {
	for _, raw := range d.tokens() {
		if !strings.Contains(s, raw) {
			continue
		}
		s = replaceBounded(s, raw, d.entries[raw])
	}
	return s
}

func replaceBounded(s, raw, pseudonym string) string {
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(s[i:], raw)
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		start := i + j
		end := start + len(raw)
		b.WriteString(s[i:start])
		if boundedLeft(s, start) && boundedRight(s, end) {
			b.WriteString(pseudonym)
		} else {
			b.WriteString(raw)
		}
		i = end
	}
}

func boundedLeft(s string, start int) bool { return start == 0 || !isAlnum(s[start-1]) }
func boundedRight(s string, end int) bool  { return end == len(s) || !isAlnum(s[end]) }
func isAlnum(c byte) bool                  { return isDigit(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isDigit(c byte) bool                  { return c >= '0' && c <= '9' }
func isHex(c byte) bool                    { return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') }

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
// dictionary.
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
		for childKey, child := range v {
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
		for childKey, child := range v {
			if class == ClassTagValue && !safeTagKey(childKey) {
				w.dict.learn(ClassTagValue, childKey)
			}
			w.learn(childKey, child, class)
		}
	}
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
				outKey = w.dict.substitute(childKey)
			}
			out[outKey] = w.rewrite(path+"."+childKey, childKey, child, childInherited)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(v))
		for childKey, child := range v {
			outKey := childKey
			if class == ClassTagValue {
				outKey = w.dict.substitute(childKey)
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
	default:
		return w.dict.substitute(v)
	}
}

func (w *walker) opaqueValue(v string) string {
	if strings.TrimSpace(v) == "" {
		return v
	}
	return "o" + w.dict.key.hexOf(v, 11)
}
