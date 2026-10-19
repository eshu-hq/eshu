// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package handle

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workloadid"
)

// SkipReasonHandleAmbiguous is the distinct skip reason for a
// provider-asserted workload handle that names workloads in more than one
// repository. It never overloads ambiguous_anchor, which means the cloud
// anchor evidence itself was ambiguous: here the evidence was clear and the
// graph holds more than one workload with that name (compatibility doc
// section 2.3).
const SkipReasonHandleAmbiguous = "workload_handle_ambiguous"

// SurfaceReducer is the surface label every reducer consumer reports on
// eshu_dp_workload_handle_resolution_total.
const SurfaceReducer = "reducer"

// Outcome names one rung result of the handle ladder.
type Outcome string

const (
	// OutcomeExact is a full scoped id that matches one node.
	OutcomeExact Outcome = "exact"
	// OutcomeUniqueByName is a handle whose bare name matches exactly one
	// node in the candidate set.
	OutcomeUniqueByName Outcome = "unique_by_name"
	// OutcomeAmbiguous is a handle whose bare name matches more than one
	// node in the candidate set. It resolves to nothing.
	OutcomeAmbiguous Outcome = "ambiguous"
	// OutcomeMiss matches no node in the candidate set.
	OutcomeMiss Outcome = "miss"
)

// Candidate is one Workload node the ladder may resolve to. Callers read
// candidates from the graph (one workload_name index seek per handle,
// batched per intent) already restricted to what the fact may prove; the
// allowed set below narrows them further to the scope or an explicit
// repository field.
type Candidate struct {
	ID     string
	Name   string
	RepoID string
}

// Resolution is the ladder result for one selector.
type Resolution struct {
	Outcome Outcome
	// WorkloadID is the resolved full node id on exact and unique_by_name,
	// empty otherwise.
	WorkloadID string
	// CandidateRepoIDs holds the sorted distinct repository ids that share
	// the handle on ambiguous, empty otherwise.
	CandidateRepoIDs []string
}

// Resolve runs the pure handle ladder over injected candidates: no backend,
// no I/O. allowedRepoIDs restricts the candidate set to the scope's
// repositories (ListScopeRepositoryIDs) or to one explicit repository
// (workload_repository_id / decoded RepositoryID); empty means the whole
// candidate set as read.
func Resolve(selector string, candidates []Candidate, allowedRepoIDs []string) Resolution {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		return Resolution{Outcome: OutcomeMiss}
	}
	allowed := allowedRepoSet(allowedRepoIDs)
	switch workloadid.ClassifyWorkloadSelector(trimmed) {
	case workloadid.SelectorKindFullID:
		for _, candidate := range candidates {
			if candidate.ID == "" || !allowed(candidate.RepoID) {
				continue
			}
			if candidate.ID == trimmed {
				return Resolution{Outcome: OutcomeExact, WorkloadID: candidate.ID}
			}
		}
		return Resolution{Outcome: OutcomeMiss}
	default:
		name := workloadid.StripWorkloadHandlePrefix(trimmed)
		if name == "" {
			return Resolution{Outcome: OutcomeMiss}
		}
		var matched []Candidate
		for _, candidate := range candidates {
			if candidate.ID == "" || candidate.Name == "" || !allowed(candidate.RepoID) {
				continue
			}
			if candidate.Name == name {
				matched = append(matched, candidate)
			}
		}
		switch len(matched) {
		case 0:
			return Resolution{Outcome: OutcomeMiss}
		case 1:
			return Resolution{Outcome: OutcomeUniqueByName, WorkloadID: matched[0].ID}
		default:
			return Resolution{Outcome: OutcomeAmbiguous, CandidateRepoIDs: distinctRepoIDs(matched)}
		}
	}
}

// ResolveBatch resolves every selector over the same candidate set,
// deduplicating repeated selectors. It performs no lookup itself: callers
// read candidates once with HandleNames and pass them here.
func ResolveBatch(selectors []string, candidates []Candidate, allowedRepoIDs []string) map[string]Resolution {
	out := make(map[string]Resolution, len(selectors))
	for _, selector := range selectors {
		if _, ok := out[selector]; ok {
			continue
		}
		out[selector] = Resolve(selector, candidates, allowedRepoIDs)
	}
	return out
}

// HandleNames returns the sorted distinct bare names that need a graph
// lookup: handles (and bare names) resolve by name, while full ids resolve
// by exact match at the writer and never need the index seek.
func HandleNames(selectors []string) []string {
	seen := make(map[string]struct{})
	var names []string
	for _, selector := range selectors {
		trimmed := strings.TrimSpace(selector)
		if workloadid.ClassifyWorkloadSelector(trimmed) == workloadid.SelectorKindFullID {
			continue
		}
		name := workloadid.StripWorkloadHandlePrefix(trimmed)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Lookup lists every Workload node whose name is in names, across all
// repositories. Implementations anchor on the workload_name index, one seek
// per handle batched into a single call per intent. A nil Lookup disables
// resolution: callers keep the legacy pass-through rows.
type Lookup interface {
	ListWorkloadHandleCandidates(ctx context.Context, names []string) ([]Candidate, error)
}

// ScopeRepos lists the repository ids that belong to a scope generation.
// It is structurally satisfied by crossrepo.ScopeRepositoryReader. A nil
// ScopeRepos leaves the candidate set graph-wide (the non-repository-scope
// row of the ladder table).
type ScopeRepos interface {
	ListScopeRepositoryIDs(ctx context.Context, scopeID, generationID string) ([]string, error)
}

// SiblingReader lists sibling Workload nodes by bare name, anchored on the
// workload_name index. It is structurally satisfied by
// reducer.WorkloadSiblingLookup (whose WorkloadSibling is an alias for
// Candidate), so every reducer consumer shares one adapter instead of each
// package hand-rolling its own.
type SiblingReader interface {
	ListWorkloadSiblings(ctx context.Context, names []string) ([]Candidate, error)
}

// SiblingLookup adapts a SiblingReader to Lookup: one workload_name index
// seek per handle, batched per intent by the caller. A nil Siblings reader
// lists nothing, which resolves every selector as a miss.
type SiblingLookup struct {
	Siblings SiblingReader
}

// ListWorkloadHandleCandidates implements Lookup.
func (l SiblingLookup) ListWorkloadHandleCandidates(ctx context.Context, names []string) ([]Candidate, error) {
	if l.Siblings == nil {
		return nil, nil
	}
	return l.Siblings.ListWorkloadSiblings(ctx, names)
}

// SiblingLookupOrNil wraps a sibling reader as a Lookup, preserving the
// nil that means legacy pass-through: a nil reader must stay a nil Lookup,
// because a non-nil Lookup over an empty candidate set would resolve every
// selector as a miss instead of keeping the caller's legacy rows (the #6061
// non-nil-interface guard, applied at the seam rather than per handler).
func SiblingLookupOrNil(reader SiblingReader) Lookup {
	if reader == nil {
		return nil
	}
	return SiblingLookup{Siblings: reader}
}

// Item is one selector to resolve with its optional explicit repository:
// workload_repository_id, else the decoded RepositoryID the fact carries,
// else empty to fall back to the scope's repositories.
type Item struct {
	Selector string
	RepoID   string
}

// Key is the map key ResolveAll files an item's resolution under: the
// selector plus the explicit repository, so two rows asserting different
// repositories for one handle resolve independently instead of sharing the
// first row's restriction.
func (i Item) Key() string {
	// Both segments compare trimmed, mirroring Resolve and HandleNames:
	// two rows differing only by surrounding whitespace resolve
	// identically and must share one filing key, or ResolveAll counts the
	// outcome twice (#8523 owner review).
	selector := strings.TrimSpace(i.Selector)
	if strings.TrimSpace(i.RepoID) == "" {
		return selector
	}
	return selector + "\x00" + strings.TrimSpace(i.RepoID)
}

// Resolver composes the batched ladder over the graph: one candidate read
// for every distinct handle name, then pure resolution per item restricted
// by the explicit repository or the scope's repositories. Every outcome is
// counted on eshu_dp_workload_handle_resolution_total{surface="reducer"};
// ambiguous items also emit the alertable structured log with the handle
// and candidate repo_ids.
type Resolver struct {
	ScopeRepos  ScopeRepos
	Lookup      Lookup
	Instruments *telemetry.Instruments
}

// ResolveAll resolves every item. A nil Lookup returns nil with no error:
// the caller keeps its legacy rows (the unwired-handler path). A nil
// ScopeRepos resolves against the whole candidate set.
func (r *Resolver) ResolveAll(ctx context.Context, scopeID, generationID string, items []Item) (map[string]Resolution, error) {
	if r == nil || r.Lookup == nil || len(items) == 0 {
		return nil, nil
	}
	selectors := make([]string, 0, len(items))
	for _, item := range items {
		selectors = append(selectors, item.Selector)
	}
	candidates, err := r.Lookup.ListWorkloadHandleCandidates(ctx, HandleNames(selectors))
	if err != nil {
		return nil, err
	}
	var scopeRepos []string
	if r.ScopeRepos != nil {
		scopeRepos, err = r.ScopeRepos.ListScopeRepositoryIDs(ctx, scopeID, generationID)
		if err != nil {
			return nil, err
		}
	}
	out := make(map[string]Resolution, len(items))
	for _, item := range items {
		key := item.Key()
		if _, ok := out[key]; ok {
			continue
		}
		allowed := scopeRepos
		if strings.TrimSpace(item.RepoID) != "" {
			allowed = []string{strings.TrimSpace(item.RepoID)}
		}
		resolution := Resolve(item.Selector, candidates, allowed)
		out[key] = resolution
		Record(ctx, r.Instruments, resolution.Outcome)
		if resolution.Outcome == OutcomeAmbiguous {
			LogAmbiguous(ctx, scopeID, item.Selector, resolution.CandidateRepoIDs)
		}
	}
	return out, nil
}

// Record counts one ladder outcome for eshu_dp_workload_handle_resolution_total.
// Nil-safe: handlers without Instruments skip the counter but keep the
// resolution and the structured log.
func Record(ctx context.Context, instruments *telemetry.Instruments, outcome Outcome) {
	if instruments == nil || instruments.WorkloadHandleResolutions == nil {
		return
	}
	instruments.WorkloadHandleResolutions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("surface", SurfaceReducer),
		attribute.String("outcome", string(outcome)),
	))
}

// LogAmbiguous emits the alertable structured log for an ambiguous handle:
// the evidence was clear and the graph holds more than one workload with
// that name, so the row is skipped rather than attached to one tenant.
func LogAmbiguous(ctx context.Context, scopeID, handle string, repoIDs []string) {
	slog.WarnContext(ctx, "workload handle ambiguous: skipped rather than attached to one tenant",
		slog.String("scope_id", scopeID),
		slog.String("handle", strings.TrimSpace(handle)),
		slog.Any("candidate_repo_ids", append([]string(nil), repoIDs...)),
		slog.String("skip_reason", SkipReasonHandleAmbiguous),
	)
}

func allowedRepoSet(allowedRepoIDs []string) func(string) bool {
	if len(allowedRepoIDs) == 0 {
		return func(string) bool { return true }
	}
	set := make(map[string]struct{}, len(allowedRepoIDs))
	for _, id := range allowedRepoIDs {
		if id = strings.TrimSpace(id); id != "" {
			set[id] = struct{}{}
		}
	}
	return func(repoID string) bool {
		_, ok := set[repoID]
		return ok
	}
}

func distinctRepoIDs(matched []Candidate) []string {
	seen := make(map[string]struct{}, len(matched))
	out := make([]string, 0, len(matched))
	for _, candidate := range matched {
		if _, ok := seen[candidate.RepoID]; ok {
			continue
		}
		seen[candidate.RepoID] = struct{}{}
		out = append(out, candidate.RepoID)
	}
	sort.Strings(out)
	return out
}
