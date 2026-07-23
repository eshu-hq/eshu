// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

const (
	// CodeReachabilityStateReachable marks a node reached through strong
	// modeled code evidence.
	CodeReachabilityStateReachable = "reachable"
	// CodeReachabilityStateAmbiguous marks a node whose strongest known path
	// depends on weak modeled code evidence.
	CodeReachabilityStateAmbiguous = "ambiguous"

	defaultCodeReachabilityMaxDepth = 10

	// defaultCodeReachabilityMaxVisited bounds how many distinct reachable
	// entities one repository-generation snapshot may materialize. The cap
	// protects a single pathological mega-repo from unbounded traversal memory
	// and row counts; entities omitted by truncation are not asserted dead,
	// because the dead-code query falls back to the legacy incoming-edge lookup
	// for entities absent from the materialized slice.
	defaultCodeReachabilityMaxVisited = 200000
)

// CodeReachabilityProjectionInput is the bounded in-memory snapshot used to
// compute a reachable-set read model for one repository generation.
type CodeReachabilityProjectionInput struct {
	ScopeID           string
	GenerationID      string
	RepositoryID      string
	Roots             []CodeReachabilityRoot
	Edges             []CodeReachabilityEdge
	AffectedEntityIDs []string
	// RubyClasses is the repo-wide Ruby class-ancestry snapshot used by the
	// #5376 code-root verdict builder to downgrade over-kept Rails controller
	// actions whose real base lives in another file. It is loaded from the same
	// content_entities latest-state as Roots, so verdict inputs stay mutually
	// consistent mid-re-parse. Empty for non-Ruby repositories or repositories
	// with no controller roots.
	RubyClasses []RubyClassEntity
	// RubyRoutes is the repo-wide Rails route-fact snapshot the #5494
	// route-liveness verdict extension joins against. Zero-value (its default)
	// carries HasAnyRouteEvidence=false, which keeps every ancestry-confirmed
	// controller action exactly as #5376 left it -- #5494 can only ever
	// additionally downgrade, never newly confirm.
	RubyRoutes RubyRailsRouteFacts
	MaxDepth   int
	// MaxVisited bounds the distinct reachable entities materialized for this
	// snapshot. Zero selects defaultCodeReachabilityMaxVisited.
	MaxVisited int
	ObservedAt time.Time
	UpdatedAt  time.Time
}

// CodeReachabilityProjectionStats reports bounded-traversal outcomes for one
// snapshot so the runner can surface truncation to operators.
type CodeReachabilityProjectionStats struct {
	// Visited counts the distinct reachable entities retained in the snapshot.
	Visited int
	// Truncated is true when the MaxVisited bound stopped traversal before the
	// full reachable set was enumerated.
	Truncated bool
}

// CodeReachabilityRoot identifies an entrypoint/root entity.
type CodeReachabilityRoot struct {
	EntityID  string
	RootKinds []string
	// ClassContext is the simple name of the root method's enclosing class,
	// stamped by the Ruby parser (metadata->>'class_context'). It bridges a
	// root METHOD entity to its class for the #5376 controller verdict; empty
	// for roots without a class context (which are never downgraded).
	ClassContext string
	// ActionName is the root method's own simple (entity) name, e.g. "show".
	// Combined with ClassContext ("PostsController.show") it forms the exact
	// handler shape the Ruby parser's Rails route_entries emit
	// (rubyControllerClassName + "." + action), letting the #5494 route
	// liveness check join a root against RubyRailsRouteFacts.RoutedHandlers.
	// Empty for roots loaded before #5494 (lag-safe: an empty ActionName never
	// matches a routed handler, so evaluateRouteLiveness's join always misses
	// and treats it like any other data gap -- see RouteEvidenceNoData).
	ActionName string
}

// CodeReachabilityEdge is one modeled code relationship the reachable-set
// projection may traverse.
type CodeReachabilityEdge struct {
	SourceEntityID   string
	TargetEntityID   string
	RelationshipType string
	ResolutionMethod string
}

// CodeReachabilityRow is one materialized reachable-set fact for query lookup.
type CodeReachabilityRow struct {
	ScopeID             string
	GenerationID        string
	RepositoryID        string
	RootEntityID        string
	EntityID            string
	Depth               int
	State               string
	Confidence          float64
	MinResolutionMethod string
	Evidence            []string
	RootKinds           []string
	ObservedAt          time.Time
	UpdatedAt           time.Time
}

type codeReachabilityPath struct {
	rootEntityID        string
	entityID            string
	depth               int
	confidence          float64
	minResolutionMethod string
	evidence            []string
	rootKinds           []string
}

// BuildCodeReachabilityRows computes a bounded transitive reachable set from
// root entities over modeled code edges. When AffectedEntityIDs is non-empty,
// only the affected entities and their descendants are emitted, allowing a
// reducer caller to rewrite one changed slice without refreshing the whole
// repository read model.
func BuildCodeReachabilityRows(input CodeReachabilityProjectionInput) []CodeReachabilityRow {
	rows, _ := BuildCodeReachabilityRowsWithStats(input)
	return rows
}

// BuildCodeReachabilityRowsWithStats is BuildCodeReachabilityRows plus a
// CodeReachabilityProjectionStats report. Traversal is bounded by both MaxDepth
// and MaxVisited; when the MaxVisited bound stops expansion before the full
// reachable set is enumerated, Stats.Truncated is true. The traversal is
// uid-anchored and single-connected-path: each entity keeps only its strongest
// shortest root path, mirroring the depth/frontier discipline of the
// NornicDB hop-by-hop call-chain fallback.
func BuildCodeReachabilityRowsWithStats(input CodeReachabilityProjectionInput) ([]CodeReachabilityRow, CodeReachabilityProjectionStats) {
	roots := cleanCodeReachabilityRoots(input.Roots)
	if len(roots) == 0 {
		return nil, CodeReachabilityProjectionStats{}
	}
	edgesBySource := codeReachabilityEdgesBySource(input.Edges)
	maxDepth := normalizeCodeReachabilityMaxDepth(input.MaxDepth)
	maxVisited := normalizeCodeReachabilityMaxVisited(input.MaxVisited)
	truncated := false
	affected := codeReachabilityAffectedSet(input.AffectedEntityIDs)
	now := time.Now().UTC()
	observedAt := input.ObservedAt
	if observedAt.IsZero() {
		observedAt = now
	}
	updatedAt := input.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = observedAt
	}

	best := make(map[string]codeReachabilityPath)
	queue := make([]codeReachabilityPath, 0, len(roots))
	for _, root := range roots {
		path := codeReachabilityPath{
			rootEntityID:        root.EntityID,
			entityID:            root.EntityID,
			depth:               0,
			confidence:          1,
			minResolutionMethod: codeprovenance.MethodDeclared,
			rootKinds:           root.RootKinds,
		}
		queue = append(queue, path)
		codeReachabilityKeepBest(best, path)
	}

	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		if path.depth >= maxDepth {
			continue
		}
		for _, edge := range edgesBySource[path.entityID] {
			if _, seen := best[edge.TargetEntityID]; !seen && len(best) >= maxVisited {
				// Bound reached: stop discovering new entities. Omitted
				// entities are not asserted dead; the dead-code query falls
				// back to the legacy incoming-edge lookup for them.
				truncated = true
				continue
			}
			confidence := codeprovenance.Confidence(edge.ResolutionMethod)
			next := codeReachabilityPath{
				rootEntityID:        path.rootEntityID,
				entityID:            edge.TargetEntityID,
				depth:               path.depth + 1,
				confidence:          min(path.confidence, confidence),
				minResolutionMethod: codeReachabilityMinMethod(path, edge),
				evidence:            append(append([]string{}, path.evidence...), codeReachabilityEvidence(edge)),
				rootKinds:           path.rootKinds,
			}
			if !codeReachabilityKeepBest(best, next) {
				continue
			}
			queue = append(queue, next)
		}
	}

	rows := make([]CodeReachabilityRow, 0, len(best))
	for _, path := range best {
		if len(affected) > 0 && !codeReachabilityAffectedReachable(path.entityID, affected, edgesBySource) {
			continue
		}
		rows = append(rows, CodeReachabilityRow{
			ScopeID:             strings.TrimSpace(input.ScopeID),
			GenerationID:        strings.TrimSpace(input.GenerationID),
			RepositoryID:        strings.TrimSpace(input.RepositoryID),
			RootEntityID:        path.rootEntityID,
			EntityID:            path.entityID,
			Depth:               path.depth,
			State:               codeReachabilityState(path.confidence),
			Confidence:          path.confidence,
			MinResolutionMethod: path.minResolutionMethod,
			Evidence:            append([]string{}, path.evidence...),
			RootKinds:           append([]string{}, path.rootKinds...),
			ObservedAt:          observedAt,
			UpdatedAt:           updatedAt,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Depth != rows[j].Depth {
			return rows[i].Depth < rows[j].Depth
		}
		return rows[i].EntityID < rows[j].EntityID
	})
	return rows, CodeReachabilityProjectionStats{Visited: len(best), Truncated: truncated}
}

func cleanCodeReachabilityRoots(roots []CodeReachabilityRoot) []CodeReachabilityRoot {
	cleaned := make([]CodeReachabilityRoot, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		entityID := strings.TrimSpace(root.EntityID)
		if entityID == "" {
			continue
		}
		if _, ok := seen[entityID]; ok {
			continue
		}
		seen[entityID] = struct{}{}
		cleaned = append(cleaned, CodeReachabilityRoot{
			EntityID:  entityID,
			RootKinds: cleanStringSlice(root.RootKinds),
		})
	}
	return cleaned
}

func cleanStringSlice(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	sort.Strings(cleaned)
	return cleaned
}

func codeReachabilityEdgesBySource(edges []CodeReachabilityEdge) map[string][]CodeReachabilityEdge {
	result := make(map[string][]CodeReachabilityEdge)
	for _, edge := range edges {
		sourceID := strings.TrimSpace(edge.SourceEntityID)
		targetID := strings.TrimSpace(edge.TargetEntityID)
		relationshipType := strings.ToUpper(strings.TrimSpace(edge.RelationshipType))
		if sourceID == "" || targetID == "" || !codeReachabilityTraversableEdge(relationshipType) {
			continue
		}
		edge.SourceEntityID = sourceID
		edge.TargetEntityID = targetID
		edge.RelationshipType = relationshipType
		if strings.TrimSpace(edge.ResolutionMethod) == "" {
			edge.ResolutionMethod = codeprovenance.MethodUnspecified
		}
		result[sourceID] = append(result[sourceID], edge)
	}
	for sourceID := range result {
		sort.Slice(result[sourceID], func(i, j int) bool {
			if result[sourceID][i].TargetEntityID != result[sourceID][j].TargetEntityID {
				return result[sourceID][i].TargetEntityID < result[sourceID][j].TargetEntityID
			}
			return result[sourceID][i].RelationshipType < result[sourceID][j].RelationshipType
		})
	}
	return result
}

func codeReachabilityTraversableEdge(relationshipType string) bool {
	switch relationshipType {
	case "CALLS", "REFERENCES", "INHERITS":
		return true
	default:
		return false
	}
}

func normalizeCodeReachabilityMaxDepth(maxDepth int) int {
	if maxDepth <= 0 {
		return defaultCodeReachabilityMaxDepth
	}
	return maxDepth
}

func normalizeCodeReachabilityMaxVisited(maxVisited int) int {
	if maxVisited <= 0 {
		return defaultCodeReachabilityMaxVisited
	}
	return maxVisited
}

func codeReachabilityKeepBest(best map[string]codeReachabilityPath, path codeReachabilityPath) bool {
	existing, ok := best[path.entityID]
	if ok && (existing.depth < path.depth || (existing.depth == path.depth && existing.confidence >= path.confidence)) {
		return false
	}
	best[path.entityID] = path
	return true
}

func codeReachabilityMinMethod(path codeReachabilityPath, edge CodeReachabilityEdge) string {
	if codeprovenance.Confidence(edge.ResolutionMethod) < path.confidence {
		return edge.ResolutionMethod
	}
	return path.minResolutionMethod
}

func codeReachabilityEvidence(edge CodeReachabilityEdge) string {
	return edge.SourceEntityID + " " + edge.RelationshipType + " " + edge.TargetEntityID
}

func codeReachabilityState(confidence float64) string {
	if confidence <= codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName) {
		return CodeReachabilityStateAmbiguous
	}
	return CodeReachabilityStateReachable
}

func codeReachabilityAffectedSet(entityIDs []string) map[string]struct{} {
	if len(entityIDs) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(entityIDs))
	for _, entityID := range entityIDs {
		if entityID = strings.TrimSpace(entityID); entityID != "" {
			result[entityID] = struct{}{}
		}
	}
	return result
}

func codeReachabilityAffectedReachable(
	entityID string,
	affected map[string]struct{},
	edgesBySource map[string][]CodeReachabilityEdge,
) bool {
	if _, ok := affected[entityID]; ok {
		return true
	}
	queue := make([]string, 0, len(affected))
	seen := make(map[string]struct{}, len(affected))
	for id := range affected {
		queue = append(queue, id)
		seen[id] = struct{}{}
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range edgesBySource[current] {
			if edge.TargetEntityID == entityID {
				return true
			}
			if _, ok := seen[edge.TargetEntityID]; ok {
				continue
			}
			seen[edge.TargetEntityID] = struct{}{}
			queue = append(queue, edge.TargetEntityID)
		}
	}
	return false
}
