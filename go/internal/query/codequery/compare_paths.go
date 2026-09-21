// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/chain"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// CompareCodePathsCapability is the contract capability for path
// comparison, declared by the family that implements the route. It gates
// the handler and the matrix row together so one id cannot drift from the
// other.
const CompareCodePathsCapability = "call_graph.compare_code_paths"

// compareHop is one outgoing CALLS edge as the BFS sees it.
type compareHop struct {
	ID         string
	Name       string
	Confidence float64
}

// compareNode is one shaped path member.
type compareNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// comparePath is one distinct simple path with its weakest-edge confidence.
type comparePath struct {
	Nodes      []compareNode `json:"nodes"`
	Depth      int           `json:"depth"`
	Confidence float64       `json:"confidence"`
}

// compareEnds carries the endpoint names for shaping; ids travel separately.
type compareEnds struct {
	startName string
	endName   string
}

// compareResult is one BFS enumeration: the distinct simple paths,
// whether work was left behind, and how many node expansions the bounds
// allowed. Visited rides the response as the operator signal for the
// traversal budget.
type compareResult struct {
	Paths     []comparePath
	Truncated bool
	Visited   int
}

// bfsComparePaths enumerates up to maxPaths distinct simple paths from
// startID to endID over expand, shortest first. The visited set rides each
// frontier state, so no emitted path repeats a node (simple, stronger than
// the trail semantics a Cypher variable-length read promises). Neighbors
// expand in id order, so the enumeration is deterministic for one graph.
// It stops at maxDepth hops, maxPaths emissions, or visitBudget expansions;
// truncated reports work left behind (false means exhaustive under caps).
func bfsComparePaths(
	ctx context.Context,
	startID, endID string,
	ends compareEnds,
	depth, maxPaths, visitBudget int,
	expand func(context.Context, string) ([]compareHop, error),
) (compareResult, error) {
	if strings.TrimSpace(startID) == "" || strings.TrimSpace(endID) == "" {
		return compareResult{}, fmt.Errorf("compare requires resolved start and end entity ids")
	}
	if startID == endID {
		return compareResult{
			Paths: []comparePath{{
				Nodes:      []compareNode{{ID: startID, Name: ends.startName}},
				Depth:      0,
				Confidence: 1,
			}},
		}, nil
	}
	type state struct {
		node    string
		path    []compareNode
		visited map[string]struct{}
		minConf float64
	}
	paths := []comparePath{}
	frontier := []state{{
		node:    startID,
		path:    []compareNode{{ID: startID, Name: ends.startName}},
		visited: map[string]struct{}{startID: {}},
		minConf: 1,
	}}
	expansions := 0
	for len(frontier) > 0 && len(paths) < maxPaths && expansions < visitBudget {
		current := frontier[0]
		frontier = frontier[1:]
		expansions++
		if len(current.path)-1 >= depth {
			continue
		}
		hops, err := expand(ctx, current.node)
		if err != nil {
			return compareResult{}, err
		}
		ordered := append([]compareHop(nil), hops...)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
		for _, hop := range ordered {
			if _, seen := current.visited[hop.ID]; seen {
				continue
			}
			minConf := current.minConf
			if hop.Confidence < minConf {
				minConf = hop.Confidence
			}
			next := append(append([]compareNode(nil), current.path...),
				compareNode{ID: hop.ID, Name: hop.Name})
			if hop.ID == endID {
				paths = append(paths, comparePath{
					Nodes:      next,
					Depth:      len(next) - 1,
					Confidence: minConf,
				})
				if len(paths) >= maxPaths {
					break
				}
				continue
			}
			visited := make(map[string]struct{}, len(current.visited)+1)
			for id := range current.visited {
				visited[id] = struct{}{}
			}
			visited[hop.ID] = struct{}{}
			frontier = append(frontier, state{
				node:    hop.ID,
				path:    next,
				visited: visited,
				minConf: minConf,
			})
		}
	}
	return compareResult{Paths: paths, Truncated: len(frontier) > 0, Visited: expansions}, nil
}

// comparePathsRequest extends the call-chain request with the path cap.
// Embedding keeps the chain leaf's resolution, validation, and grant
// helpers working on the inner request untouched.
type comparePathsRequest struct {
	chain.Request
	MaxPaths int `json:"max_paths"`
}

// handleCompareCodePaths serves POST /api/v0/code/call-chain/compare: up
// to K distinct simple paths, depth at most N, between two named entities
// in one repository. Single-repo v1: repo_id is required and cross_repo
// is refused, so every hop binds the same repository scope as the
// endpoints.
func (h *CodeHandler) handleCompareCodePaths(w http.ResponseWriter, r *http.Request) {
	// No handler span: the sibling call-chain route carries none, so no
	// new telemetry signal ships with this read; the traversal budget
	// rides the response visited count instead.
	var req comparePathsRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if querycontract.CapabilityUnsupported(h.profile(), CompareCodePathsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"code path comparison requires authoritative graph mode",
			ErrorCodeUnsupportedCapability,
			CompareCodePathsCapability,
			h.profile(),
			querycontract.RequiredProfile(CompareCodePathsCapability),
		)
		return
	}
	if strings.TrimSpace(req.RepoID) == "" {
		WriteError(w, http.StatusBadRequest, "repo_id is required")
		return
	}
	if req.CrossRepo {
		WriteError(w, http.StatusBadRequest, "cross_repo compare is not supported; scope both entities to repo_id")
		return
	}
	if err := req.Validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.resolveCallChainEntityIDs(r.Context(), &req.Request); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, CompareCodePathsCapability) {
		return
	}
	depth := normalizeCompareDepth(req.MaxDepth)
	maxPaths := normalizeCompareMaxPaths(req.MaxPaths)
	backend := h.graphBackend()
	access := codeGrantAccessFilter(r.Context())
	expand := func(ctx context.Context, nodeID string) ([]compareHop, error) {
		cypher, params := BuildComparePathsHopCypher(nodeID, req.RepoID, backend, access)
		rows, err := h.runWrapperGraphRows(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		hops := make([]compareHop, 0, len(rows))
		for _, row := range rows {
			id := StringVal(row, "id")
			if id == "" {
				continue
			}
			hops = append(hops, compareHop{
				ID:         id,
				Name:       StringVal(row, "name"),
				Confidence: querycontract.FloatVal(row, "edge_confidence"),
			})
		}
		return hops, nil
	}
	result, err := bfsComparePaths(
		r.Context(), req.StartEntityID, req.EndEntityID,
		compareEnds{startName: req.Start, endName: req.End},
		depth, maxPaths, CompareVisitBudget, expand,
	)
	if err != nil {
		if WriteGraphReadError(w, r, err, CompareCodePathsCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	shaped := make([]map[string]any, 0, len(result.Paths))
	for _, path := range result.Paths {
		nodes := make([]map[string]any, 0, len(path.Nodes))
		for _, node := range path.Nodes {
			nodes = append(nodes, map[string]any{"id": node.ID, "name": node.Name})
		}
		shaped = append(shaped, map[string]any{
			"nodes":      nodes,
			"depth":      path.Depth,
			"confidence": path.Confidence,
		})
	}
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		map[string]any{
			"start":           req.Start,
			"end":             req.End,
			"start_entity_id": req.StartEntityID,
			"end_entity_id":   req.EndEntityID,
			"repo_id":         req.RepoID,
			"paths":           shaped,
			"truncated":       result.Truncated,
			"max_depth":       depth,
			"max_paths":       maxPaths,
			"source_backend":  "graph",
			"visited":         result.Visited,
		},
		BuildTruthEnvelope(h.profile(), CompareCodePathsCapability, TruthBasisAuthoritativeGraph, "resolved from bounded breadth-first traversal over one-hop graph reads"),
	)
}
