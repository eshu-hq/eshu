// Package scope defines durable identity and generation lifecycle for source
// scopes ingested by Eshu collectors.
//
// IngestionScope captures the bounded source-local identity (repository,
// account, region, cluster, snapshot, or event trigger). ScopeGeneration
// captures one observed snapshot and tracks the pending -> active ->
// (superseded | completed | failed) lifecycle through an explicit transition
// table. Validation rejects unknown statuses, blank identifiers, zero
// timestamps, and forbidden transitions.
package scope
