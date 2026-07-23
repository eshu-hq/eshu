// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rubycontroller

import (
	"sort"
	"strings"
)

// normalizeRef trims whitespace and a leading "::" (absolute-constant marker)
// from a ref or class name.
func normalizeRef(ref string) string {
	return strings.TrimPrefix(strings.TrimSpace(ref), "::")
}

// classNamespaceOf returns classKey's own lexical namespace: every "::"-joined
// segment except the last (classKey's own declared name). It is "" for a
// top-level class or a same-file-registry class key (which is always simple,
// per rubySameFileControllerRegistry), so the #5500 lexical restriction is a
// provable no-op for both cases. This is a pure string derivation of the
// #5376 F3 qualified_name — it does not distinguish Ruby's nested-module-block
// form (`module A; class B; end; end`, which DOES add "A" to Module.nesting)
// from the compact colon form (`class A::B`, which does NOT) because
// qualifiedClassName already collapses both into one qualified string; see
// go/internal/parser/ruby/nodes.go. This is a documented, accepted
// approximation, not a regression: it can only make more candidates
// EXACT-resolvable than before (never fewer), so it can only improve
// precision, never drop a match the pre-#5500 walk found.
func classNamespaceOf(classKey string) string {
	idx := strings.LastIndex(classKey, "::")
	if idx < 0 {
		return ""
	}
	return classKey[:idx]
}

// lexicalExactMatch tries ref's real Ruby constant-lookup candidates —
// scope::ref for namespace, then for each enclosing prefix of namespace found
// by trimming one "::"-segment off the right at a time, and finally ref alone
// (top-level) — and returns the UNION of every level's ExactMatches hit,
// sorted and deduplicated.
//
// It deliberately does NOT stop at the first hit. classNamespaceOf derives
// namespace purely from the walked classKey's own qualified name, which
// cannot distinguish Ruby's nested-module-block declaration form
// (`module Admin; class Foo < Bar; end; end`, where Module.nesting really
// does include "Admin" when Bar is resolved) from the compact colon form
// (`class Admin::Foo < Bar`, where Module.nesting does NOT include "Admin"
// unless the file also lexically wraps it) — qualifiedClassName
// (go/internal/parser/ruby/nodes.go) produces the IDENTICAL qualified name
// for both. Stopping at the first inner-scope hit would let a coincidentally
// same-named class at an inner candidate level (e.g. an unrelated
// "Admin::Base" that exists elsewhere in the corpus) SILENTLY MASK the true
// bare top-level referent for a compact-colon declaration — SuffixMatches
// only returns STRICT offset>0 matches, so the true offset-0 top-level
// referent is not reachable any other way once masked. That is the exact
// false-downgrade defect #5376/#5500 promise never to reintroduce. Returning
// the union at every level keeps every plausible candidate in play for
// onwardHop's any-path-keeps aggregation instead of picking one, which keeps
// the restriction provably additive (candidates only ever grow relative to
// the pre-#5500 reg.ExactMatches(ref) lookup, so it can only rescue via
// any-path-keeps, never mask a candidate the prior lookup would have found).
func lexicalExactMatch(reg Registry, namespace, ref string) []string {
	var matches []string
	scope := namespace
	for {
		if scope == "" {
			return unionKeys(matches, reg.ExactMatches(ref))
		}
		matches = unionKeys(matches, reg.ExactMatches(scope+"::"+ref))
		idx := strings.LastIndex(scope, "::")
		if idx < 0 {
			scope = ""
			continue
		}
		scope = scope[:idx]
	}
}

// unionKeys returns the deduplicated union of two class-key slices, preserving
// a deterministic sorted order.
func unionKeys(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, keys := range [][]string{a, b} {
		for _, key := range keys {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// normalizeBases trims the "::" prefix and whitespace from each base, drops
// empties, deduplicates, and sorts for deterministic path evaluation.
func normalizeBases(bases []string) []string {
	seen := make(map[string]struct{}, len(bases))
	out := make([]string, 0, len(bases))
	for _, base := range bases {
		base = normalizeRef(base)
		if base == "" {
			continue
		}
		if _, ok := seen[base]; ok {
			continue
		}
		seen[base] = struct{}{}
		out = append(out, base)
	}
	sort.Strings(out)
	return out
}

func cloneChain(chain []string) []string {
	return append([]string(nil), chain...)
}

func cloneVisited(visited map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(visited)+1)
	for k := range visited {
		out[k] = struct{}{}
	}
	return out
}
