// Package sql extracts SQL parser payloads without depending on the parent
// parser dispatch package.
//
// Parse reads one SQL source file and emits the payload buckets consumed by the
// parent parser and content materializer: schema objects, columns, routines,
// triggers, indexes, relationships, and migration metadata. The package keeps
// detection deterministic by sorting emitted buckets and by deduplicating
// entity and relationship keys before returning.
package sql
