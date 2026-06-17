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
	MaxDepth          int
	ObservedAt        time.Time
	UpdatedAt         time.Time
}

// CodeReachabilityRoot identifies an entrypoint/root entity.
type CodeReachabilityRoot struct {
	EntityID  string
	RootKinds []string
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
	roots := cleanCodeReachabilityRoots(input.Roots)
	if len(roots) == 0 {
		return nil
	}
	edgesBySource := codeReachabilityEdgesBySource(input.Edges)
	maxDepth := normalizeCodeReachabilityMaxDepth(input.MaxDepth)
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
	return rows
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
