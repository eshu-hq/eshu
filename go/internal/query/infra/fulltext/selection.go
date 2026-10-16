// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fulltext

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ReadEnv is the operator flag for the full-text read: off (the default) or
// on. It is honored by the API and the MCP server.
const ReadEnv = "ESHU_INFRA_SEARCH_KEY_READ"

// Path names the statement that served a search. It is the search_path
// metric label and the eshu.infra_search_path span attribute.
type Path string

const (
	// PathFulltext is the infra_search_key_ft read.
	PathFulltext Path = "fulltext"
	// PathLegacy is the per-label scan.
	PathLegacy Path = "legacy"
)

// Reason says why a path was chosen. It is the reason metric label and, on
// the legacy path, the eshu.infra_search_fallback_reason span attribute.
type Reason string

const (
	// ReasonReady means the full-text read was selected.
	ReasonReady Reason = "ready"
	// ReasonFlagOff means ESHU_INFRA_SEARCH_KEY_READ is off.
	ReasonFlagOff Reason = "flag_off"
	// ReasonNotReady means readiness was never probed or its probe failed.
	ReasonNotReady Reason = "not_ready"
	// ReasonIndexNotOnline means the index is missing or not ONLINE.
	ReasonIndexNotOnline Reason = "index_not_online"
	// ReasonUnkeyed means at least one infra node that holds a searched
	// field has no search key flag.
	ReasonUnkeyed Reason = "unkeyed"
	// ReasonFTError means the full-text read failed and the legacy
	// statement answered the request.
	ReasonFTError Reason = "ft_error"
	// ReasonBackend means the graph backend is not Neo4j.
	ReasonBackend Reason = "backend"
	// ReasonNoQuery means the search has no free-text query.
	ReasonNoQuery Reason = "no_query"
	// ReasonQueryTooLong means the query has more than MaxQueryRunes code
	// points, so Lucene would refuse to build its wildcard automaton.
	ReasonQueryTooLong Reason = "query_too_long"
)

// MaxQueryRunes is the longest query, in code points, the full-text read
// serves. Lucene determinizes the *<query>* wildcard with a fixed effort
// budget of 10,000. On the pinned Neo4j 2026.08.1 the worst measured input,
// one repeated character, fails at 315 code points
// (TooComplexToDeterminizeException); other shapes fail later (ab-repeat
// 444, random text 981, ARN-like text 1,801). 256 keeps a margin under the
// worst case, and a longer query runs the legacy statement instead of
// failing and falling back on every request.
const MaxQueryRunes = 256

// ParseReadMode parses ESHU_INFRA_SEARCH_KEY_READ. Empty and "off" are off,
// "on" is on, case-insensitively; any other value is an error so a typo never
// silently changes the read path.
func ParseReadMode(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "off":
		return false, nil
	case "on":
		return true, nil
	default:
		return false, fmt.Errorf("%s=%q: want off or on", ReadEnv, v)
	}
}

// Select decides the statement for one search. The order is fixed: a
// non-Neo4j backend and an empty query never use the index, then the flag,
// then the query length, then readiness.
func Select(enabled, neo4j bool, query string, state State) (Path, Reason) {
	switch {
	case !neo4j:
		return PathLegacy, ReasonBackend
	case query == "":
		return PathLegacy, ReasonNoQuery
	case !enabled:
		return PathLegacy, ReasonFlagOff
	case utf8.RuneCountInString(query) > MaxQueryRunes:
		return PathLegacy, ReasonQueryTooLong
	case !state.Ready:
		if state.Reason == "" || state.Reason == ReasonReady {
			return PathLegacy, ReasonNotReady
		}
		return PathLegacy, state.Reason
	default:
		return PathFulltext, ReasonReady
	}
}
