// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// cypherQueryTimeout caps the whole route. Neo4jReader's tighter ten-second
	// backend-read budget remains authoritative for graph execution.
	cypherQueryTimeout = 30 * time.Second

	// cypherMaxQueryLength rejects excessively long query strings.
	cypherMaxQueryLength = 4096

	// cypherDefaultResultRows is the default returned row window.
	cypherDefaultResultRows = 100

	// cypherMaxResultRows caps the number of rows returned to prevent memory exhaustion.
	cypherMaxResultRows = 1000
)

// handleCypherQuery executes a user-submitted read-only Cypher query.
//
// Safety measures:
//   - Keyword validation rejects mutation keywords before the query reaches Neo4j
//   - Neo4jReader uses AccessModeRead sessions (driver-enforced read-only)
//   - Context timeout prevents runaway queries from holding resources
//   - Result rows are capped to prevent memory exhaustion
//   - Query length is bounded to reject payload abuse
func (h *CodeHandler) handleCypherQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CypherQuery string `json:"cypher_query"`
		Limit       int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		writeCypherQueryError(w, r, readOnlyCypherCapability, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}

	if req.CypherQuery == "" {
		writeCypherQueryError(w, r, readOnlyCypherCapability, http.StatusBadRequest, ErrorCodeInvalidArgument, "cypher_query is required")
		return
	}

	if capabilityUnsupported(h.profile(), readOnlyCypherCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"read-only Cypher queries require a graph-backed authoritative profile",
			ErrorCodeUnsupportedCapability,
			readOnlyCypherCapability,
			h.profile(),
			requiredProfile(readOnlyCypherCapability),
		)
		return
	}

	cypher, limit, err := boundedReadOnlyCypher(req.CypherQuery, req.Limit)
	if err != nil {
		writeCypherQueryError(w, r, readOnlyCypherCapability, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cypherQueryTimeout)
	defer cancel()

	rows, err := h.Neo4j.Run(ctx, cypher, nil)
	if err != nil {
		if WriteGraphReadError(w, r, err, readOnlyCypherCapability) {
			return
		}
		writeCypherQueryError(w, r, readOnlyCypherCapability, http.StatusInternalServerError, ErrorCodeInternalError, err.Error())
		return
	}

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"results":   rows,
		"limit":     limit,
		"truncated": truncated,
	}, BuildTruthEnvelope(h.profile(), readOnlyCypherCapability, TruthBasisAuthoritativeGraph, "resolved from bounded read-only graph query"))
}

// writeCypherQueryError writes a Cypher-query error under the given capability,
// as an error envelope when the caller accepts one and a plain error otherwise.
// The capability is parameterized so the read-only-cypher and graph-query
// visualization tools each report failures under their own capability rather
// than a shared hardcoded one.
func writeCypherQueryError(w http.ResponseWriter, r *http.Request, capability string, status int, code ErrorCode, message string) {
	if acceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{
			Data: nil,
			Error: &ErrorEnvelope{
				Code:       code,
				Message:    message,
				Capability: capability,
			},
		})
		return
	}
	WriteError(w, status, message)
}

func boundedReadOnlyCypher(query string, requestedLimit int) (string, int, error) {
	if err := validateReadOnlyCypher(query); err != nil {
		return "", 0, err
	}
	limit := normalizeCypherResultLimit(requestedLimit)
	query = strings.TrimSpace(query)
	query = strings.TrimSuffix(query, ";")
	queryLimit, hasLimit, err := explicitCypherLimit(query)
	if err != nil {
		return "", 0, err
	}
	if hasLimit {
		if queryLimit > limit {
			return "", 0, fmt.Errorf("query LIMIT %d exceeds requested limit %d", queryLimit, limit)
		}
		return query, limit, nil
	}
	return fmt.Sprintf("%s\nLIMIT %d", query, limit+1), limit, nil
}

// boundedVisualizationCypher bounds a read-only Cypher query for the live
// visualization path, guaranteeing the final result set is terminally capped.
//
// Unlike boundedReadOnlyCypher, which trusts any LIMIT it finds, this helper
// distinguishes a terminal LIMIT (one that bounds the final RETURN) from a
// non-terminal LIMIT (for example a `WITH ... LIMIT n` mid-query). A query such
// as `MATCH (n) WITH n LIMIT 1 MATCH (m) RETURN m` carries only a non-terminal
// LIMIT, so its final result is unbounded; this path appends a terminal
// `LIMIT limit+1` so the graph reader never Collects an unbounded result set
// before in-memory slicing. The +1 lets the caller detect truncation. A
// terminal LIMIT already present is honored and enforced against the requested
// limit, matching boundedReadOnlyCypher's contract.
func boundedVisualizationCypher(query string, requestedLimit int) (string, int, error) {
	if err := validateReadOnlyCypher(query); err != nil {
		return "", 0, err
	}
	limit := normalizeCypherResultLimit(requestedLimit)
	query = strings.TrimSpace(query)
	query = strings.TrimSuffix(query, ";")
	terminalLimit, hasTerminalLimit, err := terminalCypherLimit(query)
	if err != nil {
		return "", 0, err
	}
	if hasTerminalLimit {
		if terminalLimit > limit {
			return "", 0, fmt.Errorf("query LIMIT %d exceeds requested limit %d", terminalLimit, limit)
		}
		return query, limit, nil
	}
	return fmt.Sprintf("%s\nLIMIT %d", query, limit+1), limit, nil
}

// terminalCypherLimit reports the integer value of the query's terminal LIMIT
// clause, if any. A LIMIT is terminal only when nothing but trivia (whitespace,
// comments) follows its integer value, so it bounds the final result set rather
// than an intermediate `WITH ... LIMIT`. Inner, non-terminal LIMITs are ignored
// (hasTerminalLimit is false) so the caller can append its own terminal cap.
// Strings and comments are skipped so a LIMIT-like token inside a literal does
// not register.
func terminalCypherLimit(query string) (int, bool, error) {
	for i := 0; i < len(query); {
		switch {
		case isCypherSpace(query[i]):
			i++
		case startsCypherLineComment(query, i):
			i = skipCypherLineComment(query, i)
		case startsCypherBlockComment(query, i):
			i = skipCypherBlockComment(query, i)
		case query[i] == '\'' || query[i] == '"' || query[i] == '`':
			i = skipCypherQuoted(query, i)
		case isCypherIdentifierChar(query[i]):
			start := i
			for i < len(query) && isCypherIdentifierChar(query[i]) {
				i++
			}
			if !strings.EqualFold(query[start:i], "LIMIT") {
				continue
			}
			valueStart := skipCypherTrivia(query, i)
			valueEnd := valueStart
			for valueEnd < len(query) &&
				!isCypherSpace(query[valueEnd]) &&
				!startsCypherLineComment(query, valueEnd) &&
				!startsCypherBlockComment(query, valueEnd) {
				valueEnd++
			}
			if valueEnd == valueStart {
				return 0, false, fmt.Errorf("query LIMIT must include an integer row cap")
			}
			raw := strings.TrimRight(query[valueStart:valueEnd], ";")
			value, convErr := strconv.Atoi(raw)
			if convErr != nil || value <= 0 {
				return 0, false, fmt.Errorf("query LIMIT must be a positive integer")
			}
			// Only treat this LIMIT as terminal when nothing significant
			// follows it; otherwise keep scanning for a later, terminal LIMIT.
			if rest := skipCypherTrivia(query, valueEnd); rest >= len(query) {
				return value, true, nil
			}
			i = valueEnd
		default:
			i++
		}
	}
	return 0, false, nil
}

func normalizeCypherResultLimit(limit int) int {
	if limit <= 0 {
		return cypherDefaultResultRows
	}
	if limit > cypherMaxResultRows {
		return cypherMaxResultRows
	}
	return limit
}

func explicitCypherLimit(query string) (int, bool, error) {
	for i := 0; i < len(query); {
		switch {
		case isCypherSpace(query[i]):
			i++
		case startsCypherLineComment(query, i):
			i = skipCypherLineComment(query, i)
		case startsCypherBlockComment(query, i):
			i = skipCypherBlockComment(query, i)
		case query[i] == '\'' || query[i] == '"' || query[i] == '`':
			i = skipCypherQuoted(query, i)
		case isCypherIdentifierChar(query[i]):
			start := i
			for i < len(query) && isCypherIdentifierChar(query[i]) {
				i++
			}
			if !strings.EqualFold(query[start:i], "LIMIT") {
				continue
			}
			valueStart := skipCypherTrivia(query, i)
			valueEnd := valueStart
			for valueEnd < len(query) &&
				!isCypherSpace(query[valueEnd]) &&
				!startsCypherLineComment(query, valueEnd) &&
				!startsCypherBlockComment(query, valueEnd) {
				valueEnd++
			}
			if valueEnd == valueStart {
				return 0, true, fmt.Errorf("query LIMIT must include an integer row cap")
			}
			raw := strings.TrimRight(query[valueStart:valueEnd], ";")
			limit, err := strconv.Atoi(raw)
			if err != nil || limit <= 0 {
				return 0, true, fmt.Errorf("query LIMIT must be a positive integer")
			}
			return limit, true, nil
		default:
			i++
		}
	}
	return 0, false, nil
}

func skipCypherTrivia(query string, index int) int {
	for index < len(query) {
		switch {
		case isCypherSpace(query[index]):
			index++
		case startsCypherLineComment(query, index):
			index = skipCypherLineComment(query, index)
		case startsCypherBlockComment(query, index):
			index = skipCypherBlockComment(query, index)
		default:
			return index
		}
	}
	return index
}

func skipCypherQuoted(query string, index int) int {
	quote := query[index]
	index++
	for index < len(query) {
		if query[index] == '\\' && quote != '`' {
			index += 2
			continue
		}
		if query[index] == quote {
			if quote != '`' && index+1 < len(query) && query[index+1] == quote {
				index += 2
				continue
			}
			return index + 1
		}
		index++
	}
	return index
}

func startsCypherLineComment(query string, index int) bool {
	return index+1 < len(query) && query[index] == '/' && query[index+1] == '/'
}

func skipCypherLineComment(query string, index int) int {
	index += 2
	for index < len(query) && query[index] != '\n' && query[index] != '\r' {
		index++
	}
	return index
}

func startsCypherBlockComment(query string, index int) bool {
	return index+1 < len(query) && query[index] == '/' && query[index+1] == '*'
}

func skipCypherBlockComment(query string, index int) int {
	index += 2
	for index+1 < len(query) {
		if query[index] == '*' && query[index+1] == '/' {
			return index + 2
		}
		index++
	}
	return len(query)
}

func isCypherIdentifierChar(ch byte) bool {
	return ch == '_' ||
		(ch >= '0' && ch <= '9') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= 'a' && ch <= 'z')
}

func isCypherSpace(ch byte) bool {
	return ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t' || ch == '\f'
}

// handleVisualizeQuery executes a caller-supplied read-only Cypher query and
// returns a bounded, renderable visualization packet (nodes and edges) projected
// from the graph entities in the result, instead of a hardcoded browser URL.
//
// It shares the read-only safety path of handleCypherQuery: mutation keywords
// are rejected, the query is bounded with an injected LIMIT, the read runs under
// a timeout against an AccessModeRead session, and the row window is capped. The
// projection is a pure transformation of the returned rows; only graph nodes,
// relationships, and paths contribute to the subgraph.
func (h *CodeHandler) handleVisualizeQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CypherQuery string `json:"cypher_query"`
		Limit       int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		writeCypherQueryError(w, r, visualizationGraphQueryCapability, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}

	if req.CypherQuery == "" {
		writeCypherQueryError(w, r, visualizationGraphQueryCapability, http.StatusBadRequest, ErrorCodeInvalidArgument, "cypher_query is required")
		return
	}

	if capabilityUnsupported(h.profile(), visualizationGraphQueryCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"graph query visualization requires a graph-backed authoritative profile",
			ErrorCodeUnsupportedCapability,
			visualizationGraphQueryCapability,
			h.profile(),
			requiredProfile(visualizationGraphQueryCapability),
		)
		return
	}

	cypher, limit, err := boundedVisualizationCypher(req.CypherQuery, req.Limit)
	if err != nil {
		writeCypherQueryError(w, r, visualizationGraphQueryCapability, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cypherQueryTimeout)
	defer cancel()

	rows, err := h.Neo4j.Run(ctx, cypher, nil)
	if err != nil {
		if WriteGraphReadError(w, r, err, visualizationGraphQueryCapability) {
			return
		}
		writeCypherQueryError(w, r, visualizationGraphQueryCapability, http.StatusInternalServerError, ErrorCodeInternalError, err.Error())
		return
	}

	truncatedRows := len(rows) > limit
	if truncatedRows {
		rows = rows[:limit]
	}

	truth := h.visualizationGraphQueryTruth()
	packet := BuildGraphQueryVisualizationPacket(rows, truth)
	if truncatedRows {
		packet.Truncation.Truncated = true
		packet.Limitations = appendReason(packet.Limitations,
			"result row window was truncated to the row limit before projection; the subgraph is a bounded subset")
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"visualization_packet": packet,
		"limit":                limit,
		"truncated":            truncatedRows,
	}, truth)
}

func (h *CodeHandler) visualizationGraphQueryTruth() *TruthEnvelope {
	return BuildTruthEnvelope(
		h.profile(),
		visualizationGraphQueryCapability,
		TruthBasisAuthoritativeGraph,
		"projected renderable subgraph from a bounded read-only graph query result",
	)
}

// searchBundlesDefaultLimit and searchBundlesMaxLimit bound the registry
// bundle catalog read so a single call cannot scan the whole package graph.
