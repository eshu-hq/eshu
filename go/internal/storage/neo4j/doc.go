// Package neo4j is reserved for Neo4j-specific graph storage adapters.
//
// Backend-neutral Cypher write contracts and planners live in
// go/internal/storage/cypher. The runtime command wiring still owns the
// current Bolt driver session adapters while the storage boundary is being
// narrowed.
package neo4j
