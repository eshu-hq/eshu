// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rubycontroller

import (
	"sort"
	"strings"
)

// MaxWalkDepth caps how many superclass hops the chain walk follows before it
// gives up and falls back to the keep-biased legacy residual. It mirrors the
// reducer's defaultCodeReachabilityMaxDepth precedent and protects a
// pathological deep or generated inheritance chain from unbounded recursion.
const MaxWalkDepth = 10

// Decision reasons. These are stored verbatim in the reducer verdict basis, so
// they are a small closed set an operator can read to see why a controller
// action was confirmed or downgraded.
const (
	// ReasonAccepted: the chain reached a known Rails controller base.
	ReasonAccepted = "accepted"
	// ReasonUnresolvedNonController: the chain resolved to a declared base that
	// is neither an accepted Rails base nor a Controller-suffixed name — the
	// only positive downgrade signal.
	ReasonUnresolvedNonController = "unresolved_non_controller"
	// ReasonUnresolvedController: the chain left the corpus at a
	// Controller-suffixed base (very likely a Rails base defined in a gem) —
	// keep-biased.
	ReasonUnresolvedController = "unresolved_controller"
	// ReasonFizzled: a class in the chain declares no base anywhere in scope —
	// inconclusive, keep-biased (an in-corpus base-less class may reopen a gem
	// controller).
	ReasonFizzled = "fizzled"
	// ReasonCollision: a class name resolves to multiple conflicting declared
	// bases (reopened/short-name-colliding classes) and every resolved path
	// votes downgrade.
	ReasonCollision = "collision"
	// ReasonCycle: the chain revisited a class — keep-biased.
	ReasonCycle = "cycle"
	// ReasonDepthCap: the chain exceeded MaxWalkDepth — keep-biased.
	ReasonDepthCap = "depth_cap"
)

// acceptedControllerBases is the set of exact, fully-qualified Rails controller
// base classes that terminate a superclass chain as a genuine Rails controller.
// A chain that reaches any of these is CONFIRMED.
var acceptedControllerBases = map[string]struct{}{
	"ApplicationController":    {},
	"ActionController::Base":   {},
	"ActionController::API":    {},
	"ActionController::Metal":  {},
	"AbstractController::Base": {},
}

// Registry provides class-ancestry lookups for the decision walk. The parser
// backs it with a same-file, single-valued view; the reducer backs it with a
// repo-wide multimap that unions declared bases across reopened and
// short-name-colliding class definitions.
type Registry interface {
	// DeclaredBases returns the declared qualified superclass names for
	// className and whether className declares any superclass at all. A class
	// that exists but declares no superclass returns (nil, false); an
	// unknown class also returns (nil, false). The walk distinguishes the two
	// via IsKnownClass, never by base absence alone (#5376 D0: base absence is
	// ambiguous and must never itself drive a downgrade).
	DeclaredBases(className string) ([]string, bool)
	// IsKnownClass reports whether className is defined anywhere in scope.
	IsKnownClass(className string) bool
}

// Decision is the outcome of the controller superclass-chain walk. Keep is the
// only load-bearing field for the parser; the reducer also records Chain,
// Terminal, and Reason as verdict provenance.
type Decision struct {
	// Keep is true when the action should remain a Rails controller root.
	// False is a positive downgrade signal (the chain resolved onward to a
	// non-controller reject branch).
	Keep bool
	// Chain is the sequence of class/base names the decisive path walked,
	// starting at className.
	Chain []string
	// Terminal names the event that ended the decisive path, e.g.
	// "accepted:ActionController::Base" or "unresolved_base:ApplicationRecord".
	Terminal string
	// Reason is one of the Reason* constants.
	Reason string
}

// IsRailsController reports whether className's declared superclass chain,
// walked through reg, terminates as a genuine Rails controller. It is the
// boolean the parser uses; it is exactly Decide(className, reg).Keep.
func IsRailsController(className string, reg Registry) bool {
	return Decide(className, reg).Keep
}

// Decide walks className's declared superclass chain through reg and returns
// the keep/downgrade decision plus provenance. The outcomes are intentionally
// asymmetric and keep-biased: a false negative ("still call it live") is far
// cheaper than a false positive that recommends deleting reachable code, so
// ties resolve toward keeping the root. A downgrade is returned only on
// positive evidence — every resolved path from className ends at a declared
// base that is neither accepted nor Controller-suffixed.
func Decide(className string, reg Registry) Decision {
	name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(className), "::"))
	if name == "" {
		return Decision{Keep: false, Reason: ReasonUnresolvedNonController}
	}
	// legacyResidual is the pre-existing name-suffix signal for the method's
	// own enclosing class, used only when the chain cannot prove or disprove
	// Rails ancestry (fizzle, cycle, depth cap). It is computed once from the
	// original className and never from an intermediate hop.
	legacyResidual := strings.HasSuffix(name, "Controller")
	return decideWalk(name, legacyResidual, reg, []string{name}, map[string]struct{}{}, 0)
}

// decideWalk is the recursive, cycle-safe, depth-capped, multi-path chain walk.
// It evaluates every declared base of a class; if ANY path keeps or confirms,
// the class keeps (the collision rule). Only when every resolved path votes
// downgrade does it return Keep=false. visited is copied per branch so sibling
// paths of a colliding name do not falsely trip the cycle guard.
func decideWalk(
	current string,
	legacyResidual bool,
	reg Registry,
	chain []string,
	visited map[string]struct{},
	depth int,
) Decision {
	if _, seen := visited[current]; seen {
		return Decision{Keep: legacyResidual, Chain: chain, Terminal: "cycle:" + current, Reason: ReasonCycle}
	}
	if depth >= MaxWalkDepth {
		return Decision{Keep: legacyResidual, Chain: chain, Terminal: "depth_cap:" + current, Reason: ReasonDepthCap}
	}
	visited[current] = struct{}{}

	bases, declared := reg.DeclaredBases(current)
	if !declared {
		return Decision{Keep: legacyResidual, Chain: chain, Terminal: "fizzled:" + current, Reason: ReasonFizzled}
	}
	bases = normalizeBases(bases)
	if len(bases) == 0 {
		// Declared but every base normalized away to empty: treat as fizzle.
		return Decision{Keep: legacyResidual, Chain: chain, Terminal: "fizzled:" + current, Reason: ReasonFizzled}
	}

	multi := len(bases) > 1
	var (
		downgrade     Decision
		haveDowngrade bool
	)
	for _, base := range bases {
		branchChain := append(cloneChain(chain), base)
		if _, accepted := acceptedControllerBases[base]; accepted {
			return Decision{Keep: true, Chain: branchChain, Terminal: "accepted:" + base, Reason: ReasonAccepted}
		}
		if reg.IsKnownClass(base) {
			sub := decideWalk(base, legacyResidual, reg, branchChain, cloneVisited(visited), depth+1)
			if sub.Keep {
				return sub
			}
			if !haveDowngrade {
				downgrade, haveDowngrade = sub, true
			}
			continue
		}
		if strings.HasSuffix(base, "Controller") {
			return Decision{Keep: true, Chain: branchChain, Terminal: "unresolved_base:" + base, Reason: ReasonUnresolvedController}
		}
		if !haveDowngrade {
			downgrade = Decision{Keep: false, Chain: branchChain, Terminal: "unresolved_base:" + base, Reason: ReasonUnresolvedNonController}
			haveDowngrade = true
		}
	}

	if !haveDowngrade {
		return Decision{Keep: legacyResidual, Chain: chain, Terminal: "fizzled:" + current, Reason: ReasonFizzled}
	}
	if multi {
		// Every resolved path of a colliding name voted downgrade.
		downgrade.Reason = ReasonCollision
	}
	return downgrade
}

// normalizeBases trims the "::" prefix and whitespace from each base, drops
// empties, deduplicates, and sorts for deterministic path evaluation.
func normalizeBases(bases []string) []string {
	seen := make(map[string]struct{}, len(bases))
	out := make([]string, 0, len(bases))
	for _, base := range bases {
		base = strings.TrimPrefix(strings.TrimSpace(base), "::")
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
