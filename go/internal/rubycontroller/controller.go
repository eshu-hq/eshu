// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rubycontroller

import (
	"strings"
)

// Verdict values are the persisted code_root_verdicts.verdict strings. They are
// defined here — the one package both the reducer (writer) and the dead-code
// query (reader) import — so a rename cannot silently desync the query's SQL
// predicate from the value the reducer writes (a drift that would fail-keep:
// silently empty, a hidden feature regression).
const (
	// VerdictConfirmed marks a root the repo-wide decision kept.
	VerdictConfirmed = "confirmed"
	// VerdictDowngraded marks a root positively resolved onward to a reject
	// branch. The dead-code query acts ONLY on this value.
	VerdictDowngraded = "downgraded"
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
	// ReasonUnresolvedNonController: the chain resolved to a declared, SIMPLE
	// (unqualified) base that is neither an accepted Rails base nor a
	// Controller-suffixed name (e.g. < Thor, < StandardError) — a positive
	// downgrade signal for simple names only.
	ReasonUnresolvedNonController = "unresolved_non_controller"
	// ReasonRejectedFrameworkBase: the chain reached an exact, curated
	// non-controller Rails framework terminal (rejectedFrameworkBases, e.g.
	// ActiveRecord::Base, ApplicationRecord). This is the only way a QUALIFIED
	// base yields a downgrade — positive, unambiguous non-controller evidence.
	ReasonRejectedFrameworkBase = "rejected_framework_base"
	// ReasonUnresolvedQualified: the chain reached a QUALIFIED base (contains
	// "::") that is not accepted, not resolvable in-corpus, and not in the
	// curated reject-set. Keep-biased safety floor (#5376 P1): a namespaced base
	// we cannot resolve could be a controller base defined in a gem or a
	// namespace we do not see, so it must never downgrade a genuine controller.
	ReasonUnresolvedQualified = "unresolved_qualified"
	// ReasonSuffixOnlyAmbiguous: a base ref matched an in-corpus class ONLY by a
	// proper (offset>0) segment suffix, or is a conventional ambiguous simple
	// name ("Base"/"API") with zero corpus candidates. A proper-suffix match may
	// never feed a downgrade (#5376 P0 rev-2): the true referent may be a gem
	// class the suffix candidate merely shadows. Keep-biased; a confirm-only
	// probe of the suffix candidates could still promote it to accepted.
	ReasonSuffixOnlyAmbiguous = "suffix_only_ambiguous"
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

// rejectedFrameworkBases is the mirror of acceptedControllerBases: exact,
// fully-qualified, unambiguous non-controller Rails framework terminals.
// Reaching one (only after in-corpus resolution fails) is positive
// non-controller evidence under single inheritance, so the chain DOWNGRADES.
// This is the only qualified-base downgrade path (#5376 P1 F2); it replaces the
// old accidental-and-unsafe qualified-suffix downgrade branch that flagged a
// genuine `OrdersController < Admin::Base` dead.
var rejectedFrameworkBases = map[string]struct{}{
	"ActiveRecord::Base": {},
	"ActiveJob::Base":    {},
	"ActionMailer::Base": {},
	"ApplicationRecord":  {},
	"ApplicationJob":     {},
	"ApplicationMailer":  {},
}

// Registry provides IDENTITY-CARRYING class-ancestry resolution for the walk
// (#5376 P0 rev-2). The walk operates on RESOLVED CLASS IDENTITIES (class keys),
// never on ref strings after the first resolution, so an impostor's ancestry can
// never masquerade as a ref's. The parser backs it with a same-file table
// (SuffixMatches always empty, making same-file behavior provably unchanged);
// the reducer backs it with a repo-wide qualified-name registry.
type Registry interface {
	// ExactMatches returns the class keys whose full segment list equals ref
	// (offset-0). Only an exact in-corpus match makes a ref downgrade-eligible.
	ExactMatches(ref string) []string
	// SuffixMatches returns the class keys for which ref is a STRICT trailing
	// segment suffix (offset>0). A proper-suffix match may participate only in
	// the keep/confirm direction, never in a downgrade.
	SuffixMatches(ref string) []string
	// EntryMatches returns the candidate defining classes for a method's simple
	// class_context, by last-segment multimap. Used ONLY for the entry hop,
	// where the true referent is in-corpus by construction so the multimap stays
	// authoritative (any-path-keeps, downgrade only if every candidate does).
	EntryMatches(ctx string) []string
	// DeclaredBasesOf returns the declared superclass names for the EXACT class
	// key (no re-matching) and whether it declares any. A base-less class
	// returns (nil, false).
	DeclaredBasesOf(classKey string) ([]string, bool)
}

// conventionalAmbiguousBases are the Rails-conventional simple names for
// namespaced controller bases. With zero corpus candidates they must KEEP:
// step-6's "simple non-Controller name = positive non-controller evidence" is
// false for them (#5376 P0 rev-2, step 7).
var conventionalAmbiguousBases = map[string]struct{}{
	"Base": {},
	"API":  {},
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

// Decide resolves className's method-defining class and walks its superclass
// chain through reg, returning the keep/downgrade decision plus provenance. The
// walk operates on resolved class identities (class keys) and is keep-biased: a
// downgrade is returned only on positive evidence reached through EXACT
// resolution doors (a literal reject-set ref with zero corpus suffix matches, or
// the terminal of a fully exact-resolved chain). A proper-suffix match never
// feeds a downgrade (#5376 P0 rev-2).
func Decide(className string, reg Registry) Decision {
	name := normalizeRef(className)
	if name == "" {
		return Decision{Keep: false, Reason: ReasonUnresolvedNonController}
	}
	legacyResidual := strings.HasSuffix(name, "Controller")

	// Entry hop: the method's defining class is in-corpus by construction, so
	// the last-segment multimap stays authoritative. Any-path-keeps; downgrade
	// only if every candidate defining class votes downgrade.
	candidates := reg.EntryMatches(name)
	if len(candidates) == 0 {
		// The defining class was not loaded (A1 violated: stale/missing data).
		// Keep — the reducer proved nothing (lag-safety).
		return Decision{Keep: true, Chain: []string{name}, Terminal: "no_entry_candidate:" + name, Reason: ReasonFizzled}
	}
	return aggregateClassWalks(candidates, name, reg, legacyResidual, []string{}, map[string]struct{}{}, 0)
}

// aggregateClassWalks walks each candidate class key and applies any-path-keeps:
// the first keeping walk wins; a downgrade is returned only if every candidate
// votes downgrade. chainPrefix is the chain accumulated up to (but not
// including) these candidates; each candidate's identity is appended before its
// walk. When more than one candidate votes downgrade the reason is relabeled
// ReasonCollision.
func aggregateClassWalks(
	candidates []string,
	label string,
	reg Registry,
	legacyResidual bool,
	chainPrefix []string,
	visited map[string]struct{},
	depth int,
) Decision {
	var (
		downgrade     Decision
		haveDowngrade bool
	)
	for _, classKey := range candidates {
		branchChain := append(cloneChain(chainPrefix), classKey)
		sub := walkClass(classKey, reg, legacyResidual, branchChain, cloneVisited(visited), depth)
		if sub.Keep {
			return sub
		}
		if !haveDowngrade {
			downgrade, haveDowngrade = sub, true
		}
	}
	if !haveDowngrade {
		return Decision{Keep: legacyResidual, Chain: append(cloneChain(chainPrefix), label), Terminal: "fizzled:" + label, Reason: ReasonFizzled}
	}
	if len(candidates) > 1 {
		downgrade.Reason = ReasonCollision
	}
	return downgrade
}

// walkClass walks a RESOLVED class identity: it reads that exact class key's
// declared bases and evaluates each with the onward-hop rule. chain already ends
// with classKey (the caller appended it). visited keys on resolved class keys.
func walkClass(
	classKey string,
	reg Registry,
	legacyResidual bool,
	chain []string,
	visited map[string]struct{},
	depth int,
) Decision {
	if _, seen := visited[classKey]; seen {
		return Decision{Keep: legacyResidual, Chain: chain, Terminal: "cycle:" + classKey, Reason: ReasonCycle}
	}
	if depth >= MaxWalkDepth {
		return Decision{Keep: legacyResidual, Chain: chain, Terminal: "depth_cap:" + classKey, Reason: ReasonDepthCap}
	}
	visited[classKey] = struct{}{}

	rawBases, declared := reg.DeclaredBasesOf(classKey)
	if !declared {
		return Decision{Keep: legacyResidual, Chain: chain, Terminal: "fizzled:" + classKey, Reason: ReasonFizzled}
	}
	bases := normalizeBases(rawBases)
	if len(bases) == 0 {
		return Decision{Keep: legacyResidual, Chain: chain, Terminal: "fizzled:" + classKey, Reason: ReasonFizzled}
	}

	namespace := classNamespaceOf(classKey)
	multi := len(bases) > 1
	var (
		downgrade     Decision
		haveDowngrade bool
	)
	for _, base := range bases {
		sub := onwardHop(base.Name, base.Absolute, namespace, reg, legacyResidual, append(cloneChain(chain), base.Name), cloneVisited(visited), depth)
		if sub.Keep {
			return sub // any-path-keeps
		}
		if !haveDowngrade {
			downgrade, haveDowngrade = sub, true
		}
	}
	if !haveDowngrade {
		return Decision{Keep: legacyResidual, Chain: chain, Terminal: "fizzled:" + classKey, Reason: ReasonFizzled}
	}
	if multi {
		downgrade.Reason = ReasonCollision
	}
	return downgrade
}

// onwardHop applies the #5376 P0 rev-2 ordered rule to a single base ref R. The
// returned Decision either keeps/confirms (Keep=true) or is a downgrade vote
// (Keep=false). branchChain already ends with R. namespace is the lexical
// namespace of the class that DECLARED ref (its own qualified name minus its
// last segment), used by the #5500 lexical-scope-aware candidate restriction;
// it is "" for a top-level (non-namespaced) walked class, which makes the
// restriction a documented no-op (see lexicalExactMatch). absolute is true
// when ref was declared with an explicit leading "::" ("class Foo < ::Base");
// it disables the namespace search entirely (#5733 P1, see lexicalExactMatch).
func onwardHop(
	ref string,
	absolute bool,
	namespace string,
	reg Registry,
	legacyResidual bool,
	branchChain []string,
	visited map[string]struct{},
	depth int,
) Decision {
	// 1. Literal accepted controller base -> confirm.
	if _, accepted := acceptedControllerBases[ref]; accepted {
		return Decision{Keep: true, Chain: branchChain, Terminal: "accepted:" + ref, Reason: ReasonAccepted}
	}

	// #5500: also try the lexical-prefix names Ruby constant lookup would try —
	// P::ref, then each enclosing prefix of P, then top-level ref — alongside
	// the broad, unscoped ExactMatches(ref). This can only ADD more specific
	// exact identities to the candidate set; it never removes the top-level
	// candidate, so it cannot drop a match the pre-#5500 lookup found, and it
	// never picks one inner-scope hit over another — every level's hit stays in
	// play for the any-path-keeps aggregation below (see lexicalExactMatch doc).
	// #5733: for an ABSOLUTE ref this degrades to the bare top-level
	// ExactMatches(ref) only — real Ruby never searches the enclosing namespace
	// for "::Base".
	exact := lexicalExactMatch(reg, namespace, ref, absolute)
	suffix := reg.SuffixMatches(ref)

	// 2. EXACT resolution: R is downgrade-eligible. Recurse PER CANDIDATE class
	//    key over ExactMatches ∪ SuffixMatches (never re-unioned by ref string);
	//    any candidate that keeps/confirms rescues via any-path-keeps. Checked
	//    BEFORE the reject-set.
	if len(exact) > 0 {
		candidates := unionKeys(exact, suffix)
		return aggregateClassWalks(candidates, ref, reg, legacyResidual, branchChain[:len(branchChain)-1], visited, depth+1)
	}

	// 3. SUFFIX-ONLY ambiguity: a proper-suffix match may only confirm, never
	//    downgrade. Run a confirm-only probe whose downgrade evidence is
	//    structurally discarded. Checked BEFORE the reject-set (a corpus
	//    Legacy::ActiveRecord::Base shadows a literal ActiveRecord::Base ref).
	if len(suffix) > 0 {
		for _, classKey := range suffix {
			if probeClassConfirm(classKey, reg, cloneVisited(visited), depth+1) {
				return Decision{Keep: true, Chain: branchChain, Terminal: "accepted_via_suffix:" + ref, Reason: ReasonAccepted}
			}
		}
		return Decision{Keep: true, Chain: branchChain, Terminal: "suffix_only_ambiguous:" + ref, Reason: ReasonSuffixOnlyAmbiguous}
	}

	// 4. Literal reject-set (reachable only with zero segment-suffix matches of
	//    R) -> downgrade vote.
	if _, rejected := rejectedFrameworkBases[ref]; rejected {
		return Decision{Keep: false, Chain: branchChain, Terminal: "rejected_base:" + ref, Reason: ReasonRejectedFrameworkBase}
	}
	// 5. Unresolved qualified base -> KEEP (F1 floor).
	if strings.Contains(ref, "::") {
		return Decision{Keep: true, Chain: branchChain, Terminal: "unresolved_qualified:" + ref, Reason: ReasonUnresolvedQualified}
	}
	// 6. Unresolved simple name ending in "Controller" -> KEEP.
	if strings.HasSuffix(ref, "Controller") {
		return Decision{Keep: true, Chain: branchChain, Terminal: "unresolved_base:" + ref, Reason: ReasonUnresolvedController}
	}
	// 7. Conventional ambiguous simple name ("Base"/"API") with zero candidates
	//    -> KEEP (these are the Rails-conventional namespaced-base names).
	if _, conventional := conventionalAmbiguousBases[ref]; conventional {
		return Decision{Keep: true, Chain: branchChain, Terminal: "suffix_only_ambiguous:" + ref, Reason: ReasonSuffixOnlyAmbiguous}
	}
	// 8. Unresolved simple non-controller name with zero candidates (< Thor,
	//    < StandardError) -> downgrade vote.
	return Decision{Keep: false, Chain: branchChain, Terminal: "unresolved_base:" + ref, Reason: ReasonUnresolvedNonController}
}

// probeClassConfirm walks a suffix-candidate class in CONFIRM-ONLY mode: it
// returns true only if some path reaches the accepted controller-base set. Its
// downgrade evidence is structurally discarded (there is no downgrade return),
// so a proper-suffix match can never contribute a downgrade. It respects
// MaxWalkDepth and a resolved-class-key cycle guard.
func probeClassConfirm(classKey string, reg Registry, visited map[string]struct{}, depth int) bool {
	if _, seen := visited[classKey]; seen {
		return false
	}
	if depth >= MaxWalkDepth {
		return false
	}
	visited[classKey] = struct{}{}

	bases, declared := reg.DeclaredBasesOf(classKey)
	if !declared {
		return false
	}
	for _, base := range normalizeBases(bases) {
		if _, accepted := acceptedControllerBases[base.Name]; accepted {
			return true
		}
		for _, candidate := range unionKeys(reg.ExactMatches(base.Name), reg.SuffixMatches(base.Name)) {
			if probeClassConfirm(candidate, reg, cloneVisited(visited), depth+1) {
				return true
			}
		}
	}
	return false
}

// normalizeRef, classNamespaceOf, lexicalExactMatch, unionKeys,
// normalizeBases, cloneChain, and cloneVisited are pure string/set helpers for
// the walk above; see helpers.go.
