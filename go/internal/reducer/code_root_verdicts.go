// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/rubycontroller"
)

const (
	// CodeRootKindRubyRailsControllerAction is the only dead-code root kind the
	// #5376 verdict builder evaluates today. The code_root_verdicts table is
	// deliberately kind-generic so other guess-based framework roots can be
	// added later.
	CodeRootKindRubyRailsControllerAction = "ruby.rails_controller_action"

	// CodeRootVerdictConfirmed marks a root the repo-wide decision still
	// considers a genuine framework root. Stored for provenance only; the query
	// never acts on it.
	CodeRootVerdictConfirmed = "confirmed"
	// CodeRootVerdictDowngraded marks a root the repo-wide decision positively
	// resolved onward to a non-controller reject branch. The query acts ONLY on
	// downgraded rows; absence means "the reducer proved nothing" and the root
	// is kept (lag-safety keystone).
	CodeRootVerdictDowngraded = "downgraded"
)

// RubyClassEntity is one Ruby class definition's ancestry, loaded per
// repository from content_entities. Reopened or namespace-colliding classes
// appear as multiple entries with the same QualifiedName; the verdict builder
// unions their QualifiedBases.
type RubyClassEntity struct {
	// Name is the class's simple (last-segment) name, matching the class_context
	// stamped on method entities.
	Name string
	// QualifiedName is the class's namespace-qualified name (e.g. "Admin::Base",
	// #5376 F3). It is the registry key: a base reference is resolved by
	// segment-aligned suffix match over qualified names, so "Admin::Base"
	// resolves to this class and never to a same-last-segment impostor like
	// "Reporting::Base". Empty for pre-upgrade rows; the registry falls back to
	// Name (simple-key behavior) so stale data degrades safely under the F1
	// floor rather than producing a false positive.
	QualifiedName string
	// QualifiedBases holds the declared, possibly module-qualified superclass
	// names (e.g. "ActionController::Base"). Empty for a class with no declared
	// superclass.
	QualifiedBases []string
}

// CodeRootVerdictBasis is the JSONB provenance for one verdict row.
type CodeRootVerdictBasis struct {
	// Chain is the class/base names the decisive superclass path walked.
	Chain []string `json:"chain"`
	// Terminal names the event that ended the decisive path, e.g.
	// "accepted:ActionController::Base" or "unresolved_base:ApplicationRecord".
	Terminal string `json:"terminal"`
	// Reason is one of the rubycontroller.Reason* classifications.
	Reason string `json:"reason"`
}

// CodeRootVerdictRow is one reducer-materialized code_root_verdicts fact. It is
// keyed on the root METHOD entity, not the class.
type CodeRootVerdictRow struct {
	ScopeID      string
	GenerationID string
	RepositoryID string
	EntityID     string
	RootKind     string
	Verdict      string
	Basis        CodeRootVerdictBasis
	ObservedAt   time.Time
	UpdatedAt    time.Time
}

// CodeRootVerdictStats reports verdict-builder outcomes for operator telemetry.
type CodeRootVerdictStats struct {
	// Confirmed counts roots the repo-wide decision kept.
	Confirmed int
	// Downgraded counts roots positively resolved onward to a reject branch.
	Downgraded int
	// InconclusiveMissingContext counts rails_controller_action roots with no
	// class_context; they write no row and are therefore kept (lag-safety).
	InconclusiveMissingContext int
}

// BuildCodeRootVerdicts computes per-root-method Rails controller verdicts from
// the repo-wide Ruby class ancestry. It returns the verdict rows to persist
// (both confirmed and downgraded, for provenance), the set of downgraded root
// method entity IDs the runner removes from the BFS root set, and stats.
//
// The decision is the shared rubycontroller.Decide — identical to the parser's
// same-file walk but backed by the repo-wide multimap registry. A downgrade is
// returned only on positive evidence; every inconclusive outcome keeps and, for
// missing class_context, writes no row at all. This is what makes it
// structurally impossible for the feature to newly flag anything dead except via
// an active-generation downgraded row.
func BuildCodeRootVerdicts(input CodeReachabilityProjectionInput) ([]CodeRootVerdictRow, map[string]struct{}, CodeRootVerdictStats) {
	registry := newRubyRepoWideControllerRegistry(input.RubyClasses)
	observedAt, updatedAt := codeRootVerdictTimestamps(input)

	rows := make([]CodeRootVerdictRow, 0, len(input.Roots))
	downgraded := make(map[string]struct{})
	stats := CodeRootVerdictStats{}
	seen := make(map[string]struct{}, len(input.Roots))

	for _, root := range input.Roots {
		if !codeRootKindsContain(root.RootKinds, CodeRootKindRubyRailsControllerAction) {
			continue
		}
		entityID := strings.TrimSpace(root.EntityID)
		if entityID == "" {
			continue
		}
		if _, dup := seen[entityID]; dup {
			continue
		}
		seen[entityID] = struct{}{}

		classContext := strings.TrimSpace(root.ClassContext)
		if classContext == "" {
			// No bridge from method to class: prove nothing, write no row, keep.
			stats.InconclusiveMissingContext++
			continue
		}

		decision := rubycontroller.Decide(classContext, registry)
		verdict := CodeRootVerdictConfirmed
		if decision.Keep {
			stats.Confirmed++
		} else {
			verdict = CodeRootVerdictDowngraded
			downgraded[entityID] = struct{}{}
			stats.Downgraded++
		}

		rows = append(rows, CodeRootVerdictRow{
			ScopeID:      strings.TrimSpace(input.ScopeID),
			GenerationID: strings.TrimSpace(input.GenerationID),
			RepositoryID: strings.TrimSpace(input.RepositoryID),
			EntityID:     entityID,
			RootKind:     CodeRootKindRubyRailsControllerAction,
			Verdict:      verdict,
			Basis: CodeRootVerdictBasis{
				Chain:    append([]string(nil), decision.Chain...),
				Terminal: decision.Terminal,
				Reason:   decision.Reason,
			},
			ObservedAt: observedAt,
			UpdatedAt:  updatedAt,
		})
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].EntityID < rows[j].EntityID })
	return rows, downgraded, stats
}

// removeDowngradedRailsControllerRoots strips the ruby.rails_controller_action
// kind from any root whose entity ID is in downgraded, and drops the root
// entirely when that was its only kind. This keeps the materialized reachability
// rows consistent with the dead-code query: a downgraded controller action is
// no longer a BFS root (so descendants it uniquely reached become unreachable),
// yet a method that is also a root for another reason (e.g. a Rails callback)
// stays a root — exactly the query's per-kind skip semantics.
func removeDowngradedRailsControllerRoots(roots []CodeReachabilityRoot, downgraded map[string]struct{}) []CodeReachabilityRoot {
	if len(downgraded) == 0 {
		return roots
	}
	out := make([]CodeReachabilityRoot, 0, len(roots))
	for _, root := range roots {
		if _, isDown := downgraded[strings.TrimSpace(root.EntityID)]; !isDown {
			out = append(out, root)
			continue
		}
		remaining := make([]string, 0, len(root.RootKinds))
		for _, kind := range root.RootKinds {
			if kind == CodeRootKindRubyRailsControllerAction {
				continue
			}
			remaining = append(remaining, kind)
		}
		if len(remaining) == 0 {
			continue
		}
		root.RootKinds = remaining
		out = append(out, root)
	}
	return out
}

func codeRootVerdictTimestamps(input CodeReachabilityProjectionInput) (observedAt, updatedAt time.Time) {
	now := time.Now().UTC()
	observedAt = input.ObservedAt
	if observedAt.IsZero() {
		observedAt = now
	}
	updatedAt = input.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = observedAt
	}
	return observedAt, updatedAt
}

func codeRootKindsContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// rubyRepoWideControllerRegistry is the repo-wide, qualified-name-keyed
// rubycontroller.Registry (#5376 F3). A base or class reference is resolved by
// SEGMENT-ALIGNED SUFFIX MATCH over qualified names: "Admin::Base" matches
// "Admin::Base" and "Shop::Admin::Base" but never "Reporting::Base"; a simple
// ref "Base" matches any qualified name ending in the "Base" segment (the k=1
// generalization of the old simple-name multimap). All matched classes' bases
// are unioned so the shared decision walk evaluates every conflicting ancestry
// path (any-path-keeps). namesByLastSegment indexes qualified names by their
// last segment so resolution is O(candidates), not O(all classes).
type rubyRepoWideControllerRegistry struct {
	basesByQualified   map[string]map[string]struct{}
	namesByLastSegment map[string][]string
}

func newRubyRepoWideControllerRegistry(classes []RubyClassEntity) rubyRepoWideControllerRegistry {
	reg := rubyRepoWideControllerRegistry{
		basesByQualified:   make(map[string]map[string]struct{}, len(classes)),
		namesByLastSegment: make(map[string][]string, len(classes)),
	}
	for _, class := range classes {
		qualified := rubyRegistryQualifiedName(class)
		if qualified == "" {
			continue
		}
		if _, seen := reg.basesByQualified[qualified]; !seen {
			reg.basesByQualified[qualified] = make(map[string]struct{})
			last := rubyLastSegment(qualified)
			reg.namesByLastSegment[last] = append(reg.namesByLastSegment[last], qualified)
		}
		for _, base := range class.QualifiedBases {
			base = strings.TrimSpace(base)
			if base == "" {
				continue
			}
			reg.basesByQualified[qualified][base] = struct{}{}
		}
	}
	return reg
}

// rubyRegistryQualifiedName returns the class's qualified name, falling back to
// its simple name for pre-upgrade rows with no qualified_name (lag-safety: a
// stale simple-only registry degrades to simple-key resolution + the F1 floor,
// so it cannot produce a false positive).
func rubyRegistryQualifiedName(class RubyClassEntity) string {
	if qualified := strings.TrimSpace(class.QualifiedName); qualified != "" {
		return strings.TrimPrefix(qualified, "::")
	}
	return strings.TrimSpace(class.Name)
}

func (r rubyRepoWideControllerRegistry) DeclaredBases(ref string) ([]string, bool) {
	matches := r.matchingQualifiedNames(ref)
	if len(matches) == 0 {
		return nil, false
	}
	union := make(map[string]struct{})
	for _, qualified := range matches {
		for base := range r.basesByQualified[qualified] {
			union[base] = struct{}{}
		}
	}
	if len(union) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(union))
	for base := range union {
		out = append(out, base)
	}
	sort.Strings(out)
	return out, true
}

func (r rubyRepoWideControllerRegistry) IsKnownClass(ref string) bool {
	return len(r.matchingQualifiedNames(ref)) > 0
}

// matchingQualifiedNames returns every registered qualified name whose trailing
// segments equal ref's segments (segment-aligned suffix match).
func (r rubyRepoWideControllerRegistry) matchingQualifiedNames(ref string) []string {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "::")
	if ref == "" {
		return nil
	}
	refSegments := strings.Split(ref, "::")
	candidates := r.namesByLastSegment[refSegments[len(refSegments)-1]]
	if len(candidates) == 0 {
		return nil
	}
	matched := make([]string, 0, len(candidates))
	for _, qualified := range candidates {
		if rubyQualifiedNameHasSuffix(qualified, refSegments) {
			matched = append(matched, qualified)
		}
	}
	return matched
}

// rubyQualifiedNameHasSuffix reports whether qualified's trailing segments equal
// refSegments exactly (so "Shop::Admin::Base" matches ["Admin","Base"] but
// "Reporting::Base" does not match ["Admin","Base"]).
func rubyQualifiedNameHasSuffix(qualified string, refSegments []string) bool {
	qnSegments := strings.Split(qualified, "::")
	if len(qnSegments) < len(refSegments) {
		return false
	}
	offset := len(qnSegments) - len(refSegments)
	for i := range refSegments {
		if qnSegments[offset+i] != refSegments[i] {
			return false
		}
	}
	return true
}

func rubyLastSegment(qualified string) string {
	segments := strings.Split(qualified, "::")
	return segments[len(segments)-1]
}
