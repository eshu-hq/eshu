// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

const (
	// SpanAttrGraphReadOutcome carries the closed graph-read result vocabulary:
	// success, slow, recovered, deadline, caller_deadline, unavailable,
	// canceled, or error.
	SpanAttrGraphReadOutcome = "eshu.graph_read.outcome"
	// SpanAttrGraphReadAttempts reports the bounded total attempt count (1-2).
	SpanAttrGraphReadAttempts = "eshu.graph_read.attempts"
	// SpanAttrGraphReadConfiguredDeadlineMS reports the configured client safety
	// deadline; an earlier parent deadline remains authoritative at execution.
	SpanAttrGraphReadConfiguredDeadlineMS = "eshu.graph_read.configured_deadline_ms"
	// SpanAttrGraphReadStatementFingerprint reports the first 12 hex characters
	// of the sha256 of the redacted, whitespace-collapsed Cypher statement
	// shape, computed on every bounded read regardless of outcome. Every
	// numeric and string literal is replaced before hashing (booleans and null
	// are kept), so it identifies the statement shape without leaking a bound
	// parameter or an inline literal value.
	SpanAttrGraphReadStatementFingerprint = "eshu.graph_read.statement_fingerprint"

	// LogKeyGraphReadStatementFingerprint carries the same bounded fingerprint
	// as SpanAttrGraphReadStatementFingerprint on the query.graph_read.warning
	// log line.
	LogKeyGraphReadStatementFingerprint = "graph_read.statement_fingerprint"
	// LogKeyGraphReadStatementHead carries the redacted, whitespace-collapsed
	// Cypher statement shape truncated to a bounded length, with a truncation
	// marker appended when the statement exceeds it. Every numeric and string
	// literal is replaced with <REDACTED> (booleans and null are kept); it never
	// carries a parameter or an inline literal value.
	LogKeyGraphReadStatementHead = "graph_read.statement_head"

	// SpanAttrGraphReadQueryName reports the bounded, low-cardinality caller
	// name for the query (e.g. "code_quality.complexity_list"), threaded
	// through the request context by querycontract.WithGraphQueryName.
	// Defaults to "unnamed" when no caller set one, so this attribute is
	// always present rather than sometimes absent (issue #7006). It names the
	// route/handler, never raw Cypher text or entity identifiers.
	SpanAttrGraphReadQueryName = "eshu.graph_read.query_name"
	// LogKeyGraphReadQueryName is the query.graph_read.warning structured log
	// field carrying the same bounded query name as SpanAttrGraphReadQueryName,
	// for a slow/deadline/unavailable read (issue #7006 review F2).
	LogKeyGraphReadQueryName = "graph_query_name"
)
