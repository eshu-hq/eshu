// Package main wires the Confluence documentation collector binary.
//
// The binary reads bounded Confluence documentation evidence through
// read-only credentials, emits source-neutral documentation facts through
// collector.Service, and commits those facts through the shared Postgres
// ingestion store.
package main
