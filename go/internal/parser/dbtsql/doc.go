// Package dbtsql extracts bounded column lineage from compiled dbt SQL.
//
// ExtractCompiledModelLineage parses select projections, CTEs, relation
// aliases, simple transforms, and unresolved-expression reasons from one
// compiled model. Expression helpers keep the supported transform set explicit
// and keep unsupported shapes on the unresolved path.
// The package has no parent parser dependency; JSON dbt manifest parsing
// receives this behavior through a callback supplied by the parent parser
// wrapper.
package dbtsql
