// Package sandbox provides a Tier 2 read-only query sandbox for executing
// Cypher and SQL queries with strict authorization controls and resource
// constraints.
//
// # Security Posture
//
// The sandbox is STRICTLY READ-ONLY and operates under a DEFAULT-DENY model:
// any uncertainty in authorization results in rejection. The sandbox is
// DEFAULT-OFF and refuses to execute queries unless explicitly enabled. This
// default-off design requires a dedicated security review per issues #1755,
// #1900, and #1902 before enabling in production.
//
// # Authorization
//
// Authorization decisions are made via Decision, which provides a bounded,
// low-cardinality reason when access is denied. Reasons NEVER echo the query
// string or reveal secrets, preventing information leakage through the
// authorization response. Empty Reason strings are used only when a query is
// allowed.
//
// # Resource Constraints
//
// Query execution is bounded by Caps: maximum row count, result byte size,
// execution time, and query string length. These constraints prevent resource
// exhaustion and long-running queries from impacting the graph backend.
//
// # Dialects
//
// The sandbox supports two query dialects: Cypher (for property graph queries
// against NornicDB) and SQL (for schema queries against the relational fact
// store). Each dialect has its own authorization and execution policy.
package sandbox
