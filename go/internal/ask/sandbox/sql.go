// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sandbox

import "strings"

// sqlDenylist is the set of SQL write/DDL/dangerous-function keywords that are
// forbidden in read-only sandboxed queries. Each key is uppercase.
//
// Matching is whole-word only (via tokenizeMasked): column names such as
// update_time or created_at produce the single tokens UPDATE_TIME and
// CREATED_AT respectively, which do NOT equal UPDATE or CREATE.
//
// Dangerous function names are included because they can be called inside an
// otherwise SELECT-shaped query:
//
//	pg_sleep           — trivial DoS vector (arbitrary sleep)
//	pg_terminate_backend — kills backend connections
//	dblink             — cross-server query; data exfiltration vector
//	pg_read_file       — arbitrary file read from the server filesystem
//	lo_import          — imports a server-side file as a large object
//	lo_export          — exports a large object to the server filesystem
//	nextval            — advances a sequence (write side-effect)
//	setval             — mutates a sequence (write side-effect)
//	lo_unlink          — deletes a large object
//	lo_create          — creates a large object
//	into               — SELECT * INTO newtable creates a table (write side-effect);
//	                     whole-word matching means identifiers such as into_account
//	                     or info_table are NOT affected
//	pg_advisory_lock / pg_advisory_xact_lock / pg_try_advisory_lock /
//	pg_try_advisory_xact_lock — session or transaction advisory locks; DoS vector
//
// COPY … TO PROGRAM is covered by the COPY token; there is no need to special-
// case the PROGRAM sub-keyword.
var sqlDenylist = map[string]struct{}{
	// DML write operations
	"INSERT":   {},
	"UPDATE":   {},
	"DELETE":   {},
	"TRUNCATE": {},
	"MERGE":    {},
	// DDL operations
	"CREATE": {},
	"ALTER":  {},
	"DROP":   {},
	"GRANT":  {},
	"REVOKE": {},
	// Utility / admin operations that must not execute in the read-only sandbox
	"COPY":     {},
	"CALL":     {},
	"DO":       {},
	"SET":      {},
	"VACUUM":   {},
	"ANALYZE":  {},
	"REINDEX":  {},
	"LOCK":     {},
	"BEGIN":    {},
	"COMMIT":   {},
	"ROLLBACK": {},
	// SELECT … INTO creates a table — write side-effect hidden inside SELECT syntax.
	// No read-only query uses a bare INTO keyword (INSERT INTO is already denied via
	// INSERT; subquery aliases use AS, not INTO).
	"INTO": {},
	// Dangerous built-in functions
	"PG_SLEEP":             {},
	"PG_TERMINATE_BACKEND": {},
	"DBLINK":               {},
	"PG_READ_FILE":         {},
	"LO_IMPORT":            {},
	"LO_EXPORT":            {},
	// Sequence-mutation functions (write side-effects callable from SELECT)
	"NEXTVAL": {},
	"SETVAL":  {},
	// Large-object mutation functions
	"LO_UNLINK": {},
	"LO_CREATE": {},
	// Advisory lock functions — session/txn locks are a DoS vector
	"PG_ADVISORY_LOCK":          {},
	"PG_ADVISORY_XACT_LOCK":     {},
	"PG_TRY_ADVISORY_LOCK":      {},
	"PG_TRY_ADVISORY_XACT_LOCK": {},
}

// Bounded deny-reason strings for validateSQL. These constants must never echo
// the query body; they are fixed prefixes plus at most one keyword token.
const (
	reasonSQLReadOnly = "read-only queries must start with SELECT or WITH"
	reasonSQLWrite    = "write or unsafe clause not permitted: "
)

// validateSQL returns a Decision for a SQL query under the read-only sandbox
// policy. It is the single entry point for SQL query authorization.
//
// Precondition: callers must enforce Caps.MaxQueryLen before calling;
// validateSQL does not length-check. Control-character rejection (bytes < 0x20
// other than TAB/LF/CR, and DEL 0x7F) is handled inside normalize and
// propagated as a deny reason — callers do not need to pre-screen for those.
//
// Policy (evaluated in order):
//  1. normalize(DialectSQL, query) is called. A non-empty normalize error
//     (unterminated literal, unterminated comment, control character) → deny
//     with a bounded reason prefixed by "query rejected: ".
//  2. statementCount == 0 → deny "empty query".
//     statementCount != 1 → deny "multiple statements not permitted".
//  3. tokenizeMasked is applied to the masked form. The FIRST token
//     (uppercased) must be SELECT or WITH — anything else → deny with
//     "read-only queries must start with SELECT or WITH".
//     (WITH enables CTEs; the denylist in step 4 still catches CTE-wrapped
//     writes such as WITH x AS (DELETE …) SELECT ….)
//  4. Every token (uppercased) is checked against sqlDenylist as an exact
//     whole-word match. If any token matches → deny naming the keyword.
//     Because tokenizeMasked splits on non-identifier bytes, column names
//     such as update_time and created_at are single tokens that do NOT equal
//     UPDATE or CREATE.
//  5. All checks passed → Decision{Allowed: true}.
//
// The reason string on deny is always bounded: it never echoes the query body
// and uses only fixed prefixes plus a single keyword or normalizer error string.
func validateSQL(query string) Decision {
	n := normalize(DialectSQL, query)
	if n.err != "" {
		return Decision{Allowed: false, Reason: reasonQueryRejected + n.err}
	}
	if n.statementCount == 0 {
		return Decision{Allowed: false, Reason: reasonEmptyQuery}
	}
	if n.statementCount != 1 {
		return Decision{Allowed: false, Reason: reasonMultipleStatements}
	}

	tokens := tokenizeMasked(n.masked)

	// Step 3: the first code token must be SELECT or WITH.
	if len(tokens) == 0 {
		return Decision{Allowed: false, Reason: reasonEmptyQuery}
	}
	first := strings.ToUpper(tokens[0])
	if first != "SELECT" && first != "WITH" {
		return Decision{Allowed: false, Reason: reasonSQLReadOnly}
	}

	// Step 4: scan ALL tokens against the denylist.
	for _, tok := range tokens {
		upper := strings.ToUpper(tok)
		if _, denied := sqlDenylist[upper]; denied {
			return Decision{Allowed: false, Reason: reasonSQLWrite + upper}
		}
	}

	// Step 5: scan double-quoted identifiers in the RAW query.
	// The normalizer masks double-quoted identifier content, so a query like
	//   SELECT "pg_sleep"(10)
	// passes the token scan above (pg_sleep is hidden). We extract every
	// double-quoted segment from the raw query and deny any whose content
	// (case-insensitive, trimmed) exactly matches a sqlDenylist keyword.
	// This is a safe-side check: a quoted identifier whose name happens to
	// be a denylist word is denied rather than allowed.
	for _, seg := range doubleQuotedIdentifiers(query) {
		upper := strings.ToUpper(strings.TrimSpace(seg))
		if _, denied := sqlDenylist[upper]; denied {
			return Decision{Allowed: false, Reason: reasonSQLWrite + upper}
		}
	}

	return Decision{Allowed: true}
}

// doubleQuotedIdentifiers returns the content of every double-quoted
// identifier segment in raw. It handles the SQL "" escape (two consecutive
// double-quote characters inside a quoted identifier represent a single
// literal double-quote) and stops at the end of the string without panicking
// on unterminated identifiers.
//
// Only SQL double-quoted identifiers are handled; single-quoted string
// literals and dollar-quoted strings are intentionally not extracted here
// because they are string VALUES, not callable identifiers.
func doubleQuotedIdentifiers(raw string) []string {
	var result []string
	i := 0
	n := len(raw)
	for i < n {
		if raw[i] != '"' {
			i++
			continue
		}
		// Found opening double-quote; accumulate content until closing quote.
		i++ // skip opening "
		var seg []byte
		for i < n {
			if raw[i] == '"' {
				if i+1 < n && raw[i+1] == '"' {
					// "" escape: represents a literal " inside the identifier.
					seg = append(seg, '"')
					i += 2
				} else {
					// Closing quote.
					i++
					break
				}
			} else {
				seg = append(seg, raw[i])
				i++
			}
		}
		result = append(result, string(seg))
	}
	return result
}
