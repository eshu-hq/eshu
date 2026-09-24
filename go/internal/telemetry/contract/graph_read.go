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
)
