// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fulltext

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Broad-class bound for issue #7245. A short free-text wildcard matches a
// large share of the corpus, so the full-text read would evaluate the
// twelve-field CONTAINS recheck, DISTINCT, and ORDER BY sort over the whole
// candidate set before the late LIMIT (measured: selective full-text warm
// p95 at most 171 ms against 1,342 ms warm for a broad single character).
// For the broad class the read first counts label-matching index candidates
// up to K+1 (ProbeStatement, no recheck, no sort), then evaluates at most K
// of them: the unchanged statement when the count fits in K, so rows equal
// the uncapped read, or a capped statement that reports the cap hit.
const (
	// BroadQueryMaxRunes is the longest trimmed free-text query that counts
	// as broad. One- and two-rune wildcards match corpus-wide; the pinned
	// "svc" shapes stay selective.
	BroadQueryMaxRunes = 2
	// BroadCandidateCap (K) is the most post-index candidates the broad
	// class evaluates. It sits above the largest servable page
	// (infraSearchMaxLimit 200, over-fetched by one), so a page is complete
	// whenever the probe fits in K. The rows-equal-or-truncated proof for
	// this K on the corpus is still required; see README.md.
	BroadCandidateCap = 1000
	// BroadProbeLimit over-fetches the probe by one so a K+1 count proves
	// the cap would cut rows, instead of returning exactly K with no
	// truncation signal.
	BroadProbeLimit = BroadCandidateCap + 1
	// BroadProbeCountColumn is the probe's count column.
	BroadProbeCountColumn = "candidate_count"
	// SpanAttrBroadCapped marks a search whose broad-class cap hit on the
	// request span. It is set only then; selective and uncapped reads carry
	// no such attribute.
	SpanAttrBroadCapped = "eshu.infra_search_broad_capped"
)

// Broad reports whether query is a broad free-text query: short enough to
// match corpus-wide. Empty queries (structured search) and the "::" equality
// branch, which seeks exact values instead of running the CONTAINS recheck,
// are never broad.
func Broad(query string) bool {
	q := strings.TrimSpace(query)
	if q == "" || strings.Contains(q, "::") {
		return false
	}
	return utf8.RuneCountInString(q) <= BroadQueryMaxRunes
}

// broadPrefix is the index call and label filter the probe and both capped
// statements start with. It matches candidates() in statement.go through the
// label filter; the cap or the count follows before the expensive recheck.
func broadPrefix(labels []string) string {
	terms := make([]string, len(labels))
	for i, label := range labels {
		terms[i] = "n:" + label
	}
	return "CALL db.index.fulltext.queryNodes('" + graph.InfraSearchKeyIndexName + "', $" + LuceneParam + ") YIELD node AS n\n" +
		"WITH n\nWHERE (" + strings.Join(terms, " OR ") + ")\n"
}

// ProbeStatement counts label-matching index candidates, stopping after
// BroadProbeLimit rows. It evaluates no recheck, no DISTINCT, and no sort,
// so it stays cheap even when the candidate set is corpus-wide. whereExtra
// is deliberately absent: the recheck is part of the cost being bounded.
func ProbeStatement(labels []string) string {
	return broadPrefix(labels) +
		"WITH n LIMIT " + strconv.Itoa(BroadProbeLimit) + "\n" +
		"RETURN count(n) AS " + BroadProbeCountColumn
}

// cappedCandidates keeps the caller's label set and whereExtra (the free-text
// or "::" equality predicate plus the structured filters) unchanged, but
// evaluates at most BroadCandidateCap post-index candidates. The cap sits
// between the label filter and the recheck, before DISTINCT and ORDER BY, so
// only K rows ever reach them. A capped read is explicitly incomplete: the
// caller must surface truncated with it, never silently short rows. This is
// the one sanctioned exception to the no-inner-top-k invariant: the silent
// 13-rows-where-17-are-correct incident stays forbidden, and a capped page
// always carries its marker.
func cappedCandidates(labels []string, whereExtra string) string {
	return broadPrefix(labels) +
		"WITH n LIMIT " + strconv.Itoa(BroadCandidateCap) + "\n" +
		"WITH n\nWHERE true" + whereExtra + "\n"
}

// CappedUnscopedStatement is the broad-class form of UnscopedStatement. It
// keeps DISTINCT and the ordered paging over the capped candidates.
func CappedUnscopedStatement(labels []string, whereExtra string, returnExprs []string) string {
	return cappedCandidates(labels, whereExtra) +
		"RETURN DISTINCT " + strings.Join(returnExprs, ", ") + "\n" +
		"ORDER BY name\nLIMIT $limit"
}

// CappedScopedStatement is the broad-class form of ScopedStatement. It keeps
// per-node rows and the hoisted grant predicate over the capped candidates.
func CappedScopedStatement(labels []string, whereExtra, scopePredicate string, returnExprs []string) string {
	return cappedCandidates(labels, whereExtra) +
		"WITH n\nWHERE " + scopePredicate + "\n" +
		"RETURN " + strings.Join(returnExprs, ", ") + "\n" +
		"ORDER BY name\nLIMIT $limit"
}

// probeCandidateCount reads the probe's candidate count. It accepts the
// integer shapes graph drivers return; anything else is not a count.
func probeCandidateCount(rows []map[string]any) (int64, bool) {
	if len(rows) != 1 {
		return 0, false
	}
	switch v := rows[0][BroadProbeCountColumn].(type) {
	case int64:
		return v, v >= 0
	case int:
		return int64(v), v >= 0
	case int32:
		return int64(v), v >= 0
	case float64:
		return int64(v), v == float64(int64(v)) && v >= 0
	default:
		return 0, false
	}
}

// RowsBounded runs one search like Rows and additionally reports whether the
// broad-class cap hit. capped is true only when a broad query's probe found
// more than BroadCandidateCap candidates and the capped statement served the
// read; the caller must surface truncated with such a page. Every other
// path returns capped false with rows identical to Rows: selective reads run
// the byte-identical full-text statement, and legacy paths run the legacy
// statement. A nil *Reader always runs the legacy statement.
func (r *Reader) RowsBounded(ctx context.Context, run Runner, req Request) ([]map[string]any, bool, error) {
	enabled := r != nil && r.Enabled
	var state State
	if r != nil {
		state = r.Readiness.Current()
	}
	path, reason := Select(enabled, req.Neo4j, req.Query, state)
	if path == PathFulltext && Broad(req.Query) {
		return r.rowsBroad(ctx, run, req)
	}
	rows, err := r.rowsPlain(ctx, run, req, path, reason)
	return rows, false, err
}

// rowsBroad serves a broad query on the full-text path: probe the candidate
// count, then run the unchanged statement when it fits in K or the capped
// statement with the cap marker when it does not. Probe and read failures
// keep the Rows contract: a canceled context returns the error with no
// fallback, otherwise the legacy statement answers the request once.
func (r *Reader) rowsBroad(ctx context.Context, run Runner, req Request) ([]map[string]any, bool, error) {
	params := r.params(req)
	probeRows, err := run.Run(ctx, ProbeStatement(req.Labels), params)
	if err != nil {
		if ctx.Err() != nil {
			r.observe(ctx, PathFulltext, ReasonFTError)
			return nil, false, err
		}
		rows, err := r.serveLegacyAfterFTError(ctx, run, req)
		return rows, false, err
	}
	if count, ok := probeCandidateCount(probeRows); !ok || count > BroadCandidateCap {
		rows, direct, err := r.runFulltextOrFallback(ctx, run, req, r.cappedStatement(req), params)
		if err != nil {
			return nil, false, err
		}
		if !direct {
			return rows, false, nil
		}
		r.observe(ctx, PathFulltext, ReasonReady)
		trace.SpanFromContext(ctx).SetAttributes(attribute.Bool(SpanAttrBroadCapped, true))
		return rows, true, nil
	}
	rows, direct, err := r.runFulltextOrFallback(ctx, run, req, r.statement(req), params)
	if err != nil {
		return nil, false, err
	}
	if direct {
		r.observe(ctx, PathFulltext, ReasonReady)
	}
	return rows, false, nil
}

// runFulltextOrFallback runs one full-text statement with the Rows failure
// contract. It reports direct true when the statement served the read; a
// live-context failure serves the legacy statement once (observed inside),
// and a canceled context returns the error with no fallback.
func (r *Reader) runFulltextOrFallback(ctx context.Context, run Runner, req Request, cypher string, params map[string]any) (rows []map[string]any, direct bool, err error) {
	rows, err = run.Run(ctx, cypher, params)
	if err == nil {
		return rows, true, nil
	}
	if ctx.Err() != nil {
		r.observe(ctx, PathFulltext, ReasonFTError)
		return nil, false, err
	}
	rows, err = r.serveLegacyAfterFTError(ctx, run, req)
	return rows, false, err
}

// serveLegacyAfterFTError serves the legacy statement once after a
// live-context full-text failure, so a caller never gets partial rows or a
// full-text error. No raw driver error is logged here: it can echo statement
// text. The graph reader already logged the failed read with its bounded
// class.
func (r *Reader) serveLegacyAfterFTError(ctx context.Context, run Runner, req Request) ([]map[string]any, error) {
	if r.Logger != nil {
		r.Logger.WarnContext(ctx, "infra search full-text read failed; serving the legacy statement",
			telemetry.EventAttr("query.infra_search_key.fulltext_fallback"),
			"search_path", string(PathLegacy),
			"reason", string(ReasonFTError),
		)
	}
	r.observe(ctx, PathLegacy, ReasonFTError)
	return run.Run(ctx, req.Legacy, req.Params)
}

// cappedStatement builds the capped full-text statement for req.
func (*Reader) cappedStatement(req Request) string {
	if req.Scoped {
		return CappedScopedStatement(req.Labels, req.WhereExtra, req.ScopePredicate, req.ReturnExprs)
	}
	return CappedUnscopedStatement(req.Labels, req.WhereExtra, req.ReturnExprs)
}
