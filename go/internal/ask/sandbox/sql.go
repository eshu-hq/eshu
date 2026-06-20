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
	// Dangerous built-in functions
	"PG_SLEEP":             {},
	"PG_TERMINATE_BACKEND": {},
	"DBLINK":               {},
	"PG_READ_FILE":         {},
	"LO_IMPORT":            {},
	"LO_EXPORT":            {},
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

	return Decision{Allowed: true}
}
