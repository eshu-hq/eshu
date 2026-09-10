// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/chain"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file holds the *CodeHandler methods of the call-chain family.
// Traversal requests, Cypher builders, and node shaping live in the
// chain leaf; response shaping lives in responses.go; the methods stay
// here because Go requires methods to live in their type's package.
// Three of them carry queryplan source_sha256 pins (see
// go/internal/queryplan/testdata/query-source-coverage.yaml and
// grandfathered_non_hot.go): edit their bodies only with a manifest
// update in the same change.

// callChainRequest aliases the chain leaf's request type so every
// existing spelling (including the digest-pinned method signatures)
// keeps resolving to one definition.
type callChainRequest = chain.Request

// callChainAllowedTraversalRepoIDs forwards to the chain leaf. It stays
// (rather than updating two call sites) because the digest-pinned
// nornicDBCallChainOneHopRows and callChainCandidateOneHopRows bodies
// name the bare identifier, and a grandfathered digest is never
// re-frozen.
func callChainAllowedTraversalRepoIDs(req *callChainRequest) []string {
	return chain.AllowedTraversalRepoIDs(req)
}

func (h *CodeHandler) handleCallChain(w http.ResponseWriter, r *http.Request) {
	if querycontract.CapabilityUnsupported(h.profile(), "call_graph.call_chain_path") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"call-chain analysis requires authoritative graph mode",
			"unsupported_capability",
			"call_graph.call_chain_path",
			h.profile(),
			querycontract.RequiredProfile("call_graph.call_chain_path"),
		)
		return
	}

	var req callChainRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if strings.TrimSpace(req.StartEntityID) == "" && strings.TrimSpace(req.Start) == "" {
		WriteError(w, http.StatusBadRequest, "start or start_entity_id is required")
		return
	}
	if strings.TrimSpace(req.EndEntityID) == "" && strings.TrimSpace(req.End) == "" {
		WriteError(w, http.StatusBadRequest, "end or end_entity_id is required")
		return
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = 5
	}
	if req.MaxDepth > 10 {
		req.MaxDepth = 10
	}
	if err := req.Validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, "call_graph.call_chain_path") {
		return
	}
	if req.CrossRepo {
		if !h.applyRepositorySelectorForCapability(w, r, &req.StartRepoID, "call_graph.call_chain_path") {
			return
		}
		if !h.applyRepositorySelectorForCapability(w, r, &req.EndRepoID, "call_graph.call_chain_path") {
			return
		}
	}
	if _, blocked := codeContentGrantScope(r.Context(), req.RepoID); blocked {
		writeCallChainResponse(w, r, h, req, nil)
		return
	}
	if err := h.resolveCallChainEntityIDs(r.Context(), &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	var rows []map[string]any
	if h.graphBackend() == GraphBackendNornicDB {
		nornicRows, err := h.nornicDBCallChainRows(r.Context(), req)
		if err != nil {
			if WriteGraphReadError(w, r, err, "call_graph.call_chain_path") {
				return
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows = nornicRows
	} else {
		cypher, params := chain.BuildCallChainCypher(req, h.graphBackend(), codeGrantAccessFilter(r.Context()))
		neoRows, err := h.Neo4j.Run(r.Context(), cypher, params)
		if err != nil {
			if WriteGraphReadError(w, r, err, "call_graph.call_chain_path") {
				return
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows = neoRows
	}

	writeCallChainResponse(w, r, h, req, rows)
}

func (h *CodeHandler) resolveCallChainEntityIDs(ctx context.Context, req *callChainRequest) error {
	if h == nil || req == nil {
		return nil
	}
	var (
		startCandidates []EntityContent
		endCandidates   []EntityContent
		startErr        error
		endErr          error
	)
	if strings.TrimSpace(req.StartEntityID) == "" && strings.TrimSpace(req.Start) != "" {
		var err error
		startRepoID := chain.StartRepoID(req)
		startCandidates, err = querycontract.ResolveExactGraphEntityCandidates(ctx, h.Content, startRepoID, req.Start)
		if err != nil {
			return err
		}
		resolved, err := querycontract.SelectExactGraphEntityCandidate(startRepoID, req.Start, startCandidates)
		startErr = err
		if resolved != nil {
			req.StartEntityID = resolved.EntityID
		}
	}
	if strings.TrimSpace(req.EndEntityID) == "" && strings.TrimSpace(req.End) != "" {
		var err error
		endRepoID := chain.EndRepoID(req)
		endCandidates, err = querycontract.ResolveExactGraphEntityCandidates(ctx, h.Content, endRepoID, req.End)
		if err != nil {
			return err
		}
		resolved, err := querycontract.SelectExactGraphEntityCandidate(endRepoID, req.End, endCandidates)
		endErr = err
		if resolved != nil {
			req.EndEntityID = resolved.EntityID
		}
	}
	if startErr != nil || endErr != nil {
		resolved, err := h.resolveCallChainEntityIDsByReachability(ctx, req, startCandidates, endCandidates)
		if err != nil {
			return err
		}
		if resolved {
			return nil
		}
	}
	if startErr != nil {
		return startErr
	}
	if endErr != nil {
		return endErr
	}
	return nil
}

// cloneCallChainNodeSlice deep-copies one traversal path for the next
// breadth-first frontier. It stays here (rather than joining the chain
// leaf) because it clones through the shared cloneQueryAnyMap helper,
// which the leaf cannot import without an import cycle.
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
	startCandidates = chain.EndpointCandidates(req.StartEntityID, startCandidates)
	endCandidates = chain.EndpointCandidates(req.EndEntityID, endCandidates)
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

	pairs, err := h.reachableCallChainCandidatePairs(ctx, req, startCandidates, endCandidates)
	if err != nil {
		return false, err
	}
	switch len(pairs) {
	case 0:
		return false, fmt.Errorf(
			"call-chain endpoints matched multiple entities but no reachable call-chain route in repository %q within depth %d; pass start_entity_id and end_entity_id to disambiguate: start candidates %s; end candidates %s",
			strings.TrimSpace(req.RepoID),
			chain.NormalizedMaxDepth(req.MaxDepth),
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
	req *callChainRequest,
	startCandidates []EntityContent,
	endCandidates []EntityContent,
) ([]callChainCandidatePair, error) {
	maxDepth := 5
	if req != nil {
		maxDepth = req.MaxDepth
	}
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
			label:   chain.CandidateLabel(candidate),
		}}
		seen := map[string]struct{}{startID: {}}
		// Candidate disambiguation uses the same breadth-first order as the
		// response path and is guarded by the endpoint-pair cap above.
		for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
			next := make([]callChainCandidatePath, 0)
			for _, path := range frontier {
				rows, err := h.callChainCandidateOneHopRows(ctx, req, path.nodeID, path.label)
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
	req *callChainRequest,
	sourceID string,
	sourceLabel string,
) ([]map[string]any, error) {
	if h.graphBackend() == GraphBackendNornicDB {
		return h.nornicDBCallChainOneHopRows(ctx, sourceID, sourceLabel, callChainAllowedTraversalRepoIDs(req))
	}
	params := map[string]any{"source_id": sourceID}
	repoPredicate := ""
	if repoIDs := callChainAllowedTraversalRepoIDs(req); len(repoIDs) > 0 {
		params["traversal_repo_ids"] = repoIDs
		repoPredicate = " AND coalesce(target.repo_id, '') IN $traversal_repo_ids"
	}
	if access := codeGrantAccessFilter(ctx); access.Scoped() {
		params = access.GraphParams(params)
		repoPredicate += " AND " + access.GraphConditionOnProperty("target", "repo_id")
	}
	return h.Neo4j.Run(ctx, `
		MATCH (source)-[:CALLS]->(target)
		WHERE `+graphEntityIDPredicate("source", "$source_id")+repoPredicate+`
		RETURN coalesce(target.id, target.uid) as id,
		       target.name as name,
		       labels(target) as labels
	`, params)
}

type nornicDBCallChainPath struct {
	nodeID string
	label  string
	chain  []map[string]any
}

func (h *CodeHandler) nornicDBCallChainRows(ctx context.Context, req callChainRequest) ([]map[string]any, error) {
	start, err := h.nornicDBRelationshipMetadataRow(ctx, req.StartEntityID, req.Start, chain.StartRepoID(&req))
	if err != nil || start == nil {
		return nil, err
	}
	end, err := h.nornicDBRelationshipMetadataRow(ctx, req.EndEntityID, req.End, chain.EndRepoID(&req))
	if err != nil || end == nil {
		return nil, err
	}

	endID := StringVal(end, "id")
	frontier := []nornicDBCallChainPath{{
		nodeID: StringVal(start, "id"),
		label:  nornicDBPrimaryEntityLabel(start),
		chain:  []map[string]any{chain.NornicDBCallChainNode(start)},
	}}
	seen := map[string]struct{}{StringVal(start, "id"): {}}
	rows := make([]map[string]any, 0, 1)

	// Keep NornicDB traversal breadth-first so the first returned rows are the
	// shortest paths, and stop once the response cap is satisfied.
	for depth := 1; depth <= req.MaxDepth && len(frontier) > 0 && len(rows) < 5; depth++ {
		next := make([]nornicDBCallChainPath, 0)
		for _, path := range frontier {
			targets, err := h.nornicDBCallChainOneHopRows(ctx, path.nodeID, path.label, callChainAllowedTraversalRepoIDs(&req))
			if err != nil {
				return nil, err
			}
			for _, target := range targets {
				targetID := StringVal(target, "id")
				if targetID == "" {
					continue
				}
				chain := append(cloneCallChainNodeSlice(path.chain), chain.NornicDBCallChainNode(target))
				if targetID == endID {
					rows = append(rows, map[string]any{
						"chain": chain,
						"depth": depth,
					})
					if len(rows) >= 5 {
						break
					}
					continue
				}
				if _, ok := seen[targetID]; ok {
					continue
				}
				seen[targetID] = struct{}{}
				next = append(next, nornicDBCallChainPath{
					nodeID: targetID,
					label:  nornicDBPrimaryEntityLabel(target),
					chain:  chain,
				})
			}
		}
		frontier = next
	}
	return rows, nil
}

func (h *CodeHandler) nornicDBCallChainOneHopRows(
	ctx context.Context,
	sourceID string,
	sourceLabel string,
	allowedRepoIDs []string,
) ([]map[string]any, error) {
	sourcePattern := nornicDBNodePattern("source", sourceLabel, "$source_id")
	params := map[string]any{"source_id": sourceID}
	access := codeGrantAccessFilter(ctx)
	predicates := make([]string, 0, 2)
	if len(allowedRepoIDs) > 0 {
		params["traversal_repo_ids"] = allowedRepoIDs
		predicates = append(predicates, "coalesce(target.repo_id, '') IN $traversal_repo_ids")
	}
	if access.Scoped() {
		params = access.GraphParams(params)
		predicates = append(predicates, access.GraphConditionOnProperty("target", "repo_id"))
	}
	// Both predicates sit in the anchoring MATCH's own WHERE, on the target
	// node's own repo_id. They used to follow the two OPTIONAL MATCH clauses
	// below, where a WHERE constrains the optional pattern rather than the
	// driving row set: #5167 batch 2b measured the shipped statement returning
	// every callee with its real repository id while $traversal_repo_ids named
	// one repository. The coalesce(target.repo_id, targetRepo.id, '') fallback
	// cannot survive the move because targetRepo is not bound yet, and it should
	// not: a target the graph cannot attribute to a repository now fails the
	// predicate and is dropped.
	repoPredicate := ""
	if len(predicates) > 0 {
		repoPredicate = `
		WHERE ` + strings.Join(predicates, " AND ")
	}
	rows, err := h.Neo4j.Run(ctx, `
		MATCH `+sourcePattern+`-[:CALLS]->(target)`+repoPredicate+`
		OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)
		OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		RETURN coalesce(target.id, target.uid) as id,
		       target.name as name,
		       labels(target) as labels,
		       coalesce(target.repo_id, targetRepo.id) as repo_id,
		       coalesce(target.language, target.lang) as language,
		       target.docstring as docstring,
		       target.method_kind as method_kind
	`, params)
	if err != nil {
		return nil, err
	}
	return rows, nil
}
