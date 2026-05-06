package query

import (
	"context"
	"fmt"
	"strings"
)

type callChainCandidatePath struct {
	startID string
	nodeID  string
	label   string
}

type callChainCandidatePair struct {
	startID string
	endID   string
	depth   int
}

const maxCallChainReachabilityCandidatePairs = 100

func (h *CodeHandler) resolveCallChainEntityIDsByReachability(
	ctx context.Context,
	req *callChainRequest,
	startCandidates []EntityContent,
	endCandidates []EntityContent,
) (bool, error) {
	if h == nil || h.Neo4j == nil || req == nil {
		return false, nil
	}
	startCandidates = callChainEndpointCandidates(req.StartEntityID, startCandidates)
	endCandidates = callChainEndpointCandidates(req.EndEntityID, endCandidates)
	if len(startCandidates) == 0 || len(endCandidates) == 0 {
		return false, nil
	}
	if len(startCandidates)*len(endCandidates) > maxCallChainReachabilityCandidatePairs {
		return false, fmt.Errorf(
			"call-chain endpoints matched too many candidate pairs in repository %q to probe safely (%d > %d); pass start_entity_id and end_entity_id to disambiguate",
			strings.TrimSpace(req.RepoID),
			len(startCandidates)*len(endCandidates),
			maxCallChainReachabilityCandidatePairs,
		)
	}

	pairs, err := h.reachableCallChainCandidatePairs(ctx, req.MaxDepth, startCandidates, endCandidates)
	if err != nil {
		return false, err
	}
	switch len(pairs) {
	case 0:
		return false, fmt.Errorf(
			"call-chain endpoints matched multiple entities but no reachable call-chain route in repository %q within depth %d; pass start_entity_id and end_entity_id to disambiguate: start candidates %s; end candidates %s",
			strings.TrimSpace(req.RepoID),
			normalizedCallChainMaxDepth(req.MaxDepth),
			formatCallChainCandidateIDs(startCandidates),
			formatCallChainCandidateIDs(endCandidates),
		)
	case 1:
		req.StartEntityID = pairs[0].startID
		req.EndEntityID = pairs[0].endID
		return true, nil
	default:
		return false, fmt.Errorf(
			"call-chain endpoints matched multiple reachable entity pairs in repository %q: %s",
			strings.TrimSpace(req.RepoID),
			formatReachableCallChainCandidatePairs(pairs),
		)
	}
}

func (h *CodeHandler) reachableCallChainCandidatePairs(
	ctx context.Context,
	maxDepth int,
	startCandidates []EntityContent,
	endCandidates []EntityContent,
) ([]callChainCandidatePair, error) {
	if maxDepth <= 0 {
		maxDepth = 5
	}
	endIDs := make(map[string]struct{}, len(endCandidates))
	for _, candidate := range endCandidates {
		if id := strings.TrimSpace(candidate.EntityID); id != "" {
			endIDs[id] = struct{}{}
		}
	}
	if len(endIDs) == 0 {
		return nil, nil
	}

	pairs := make([]callChainCandidatePair, 0, 1)
	for _, candidate := range startCandidates {
		startID := strings.TrimSpace(candidate.EntityID)
		if startID == "" {
			continue
		}
		frontier := []callChainCandidatePath{{
			startID: startID,
			nodeID:  startID,
			label:   callChainCandidateLabel(candidate),
		}}
		seen := map[string]struct{}{startID: {}}
		// Candidate disambiguation uses the same breadth-first order as the
		// response path and is guarded by the endpoint-pair cap above.
		for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
			next := make([]callChainCandidatePath, 0)
			for _, path := range frontier {
				rows, err := h.callChainCandidateOneHopRows(ctx, path.nodeID, path.label)
				if err != nil {
					return nil, err
				}
				for _, row := range rows {
					targetID := StringVal(row, "id")
					if targetID == "" {
						continue
					}
					if _, ok := endIDs[targetID]; ok {
						pairs = append(pairs, callChainCandidatePair{
							startID: path.startID,
							endID:   targetID,
							depth:   depth,
						})
						continue
					}
					if _, ok := seen[targetID]; ok {
						continue
					}
					seen[targetID] = struct{}{}
					next = append(next, callChainCandidatePath{
						startID: path.startID,
						nodeID:  targetID,
						label:   nornicDBPrimaryEntityLabel(row),
					})
				}
			}
			frontier = next
		}
	}
	return pairs, nil
}

func (h *CodeHandler) callChainCandidateOneHopRows(
	ctx context.Context,
	sourceID string,
	sourceLabel string,
) ([]map[string]any, error) {
	if h.graphBackend() == GraphBackendNornicDB {
		return h.nornicDBCallChainOneHopRows(ctx, sourceID, sourceLabel)
	}
	return h.Neo4j.Run(ctx, `
		MATCH (source)-[:CALLS]->(target)
		WHERE `+graphEntityIDPredicate("source", "$source_id")+`
		RETURN coalesce(target.id, target.uid) as id,
		       target.name as name,
		       labels(target) as labels
	`, map[string]any{"source_id": sourceID})
}

func callChainEndpointCandidates(entityID string, candidates []EntityContent) []EntityContent {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return candidates
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.EntityID) == entityID {
			return []EntityContent{candidate}
		}
	}
	return []EntityContent{{EntityID: entityID, EntityType: "Function"}}
}

func callChainCandidateLabel(candidate EntityContent) string {
	if label := strings.TrimSpace(candidate.EntityType); label != "" {
		return label
	}
	return "Function"
}

func formatReachableCallChainCandidatePairs(pairs []callChainCandidatePair) string {
	items := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		items = append(items, fmt.Sprintf("%s -> %s (depth %d)", pair.startID, pair.endID, pair.depth))
	}
	return strings.Join(items, ", ")
}

func normalizedCallChainMaxDepth(maxDepth int) int {
	if maxDepth <= 0 {
		return 5
	}
	return maxDepth
}

func formatCallChainCandidateIDs(candidates []EntityContent) string {
	items := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if id := strings.TrimSpace(candidate.EntityID); id != "" {
			items = append(items, id)
		}
	}
	return strings.Join(items, ", ")
}
