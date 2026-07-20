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
// repository from content_entities. Reopened or short-name-colliding classes
// appear as multiple entries with the same Name; the verdict builder unions
// their QualifiedBases.
type RubyClassEntity struct {
	// Name is the class's simple (last-segment) name, matching the class_context
	// stamped on method entities.
	Name string
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

// rubyRepoWideControllerRegistry is the repo-wide, multimap-backed
// rubycontroller.Registry. It unions declared bases across every class
// definition sharing a simple name (reopened and short-name-colliding classes),
// so the shared decision walk can evaluate all conflicting ancestry paths.
type rubyRepoWideControllerRegistry struct {
	known map[string]struct{}
	bases map[string]map[string]struct{}
}

func newRubyRepoWideControllerRegistry(classes []RubyClassEntity) rubyRepoWideControllerRegistry {
	reg := rubyRepoWideControllerRegistry{
		known: make(map[string]struct{}, len(classes)),
		bases: make(map[string]map[string]struct{}, len(classes)),
	}
	for _, class := range classes {
		name := strings.TrimSpace(class.Name)
		if name == "" {
			continue
		}
		reg.known[name] = struct{}{}
		for _, base := range class.QualifiedBases {
			base = strings.TrimSpace(base)
			if base == "" {
				continue
			}
			set := reg.bases[name]
			if set == nil {
				set = make(map[string]struct{})
				reg.bases[name] = set
			}
			set[base] = struct{}{}
		}
	}
	return reg
}

func (r rubyRepoWideControllerRegistry) DeclaredBases(className string) ([]string, bool) {
	set, ok := r.bases[className]
	if !ok || len(set) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(set))
	for base := range set {
		out = append(out, base)
	}
	sort.Strings(out)
	return out, true
}

func (r rubyRepoWideControllerRegistry) IsKnownClass(className string) bool {
	_, ok := r.known[className]
	return ok
}
