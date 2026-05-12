// Package scope defines durable identity and generation lifecycle for source
// scopes ingested by Eshu collectors.
//
// IngestionScope captures the bounded source-local identity (repository,
// account, region, cluster, snapshot, or event trigger). ScopeGeneration
// captures one observed snapshot and tracks the pending -> active ->
// (superseded | completed | failed) lifecycle through an explicit transition
// table. Validation rejects unknown statuses, blank identifiers, zero
// timestamps, and forbidden transitions. Terraform state helpers create stable
// state-snapshot scopes from backend kind plus locator hash, while generation
// identity carries state serial and lineage so serial changes do not rewrite
// the scope boundary. The state-snapshot scope hash MUST agree with
// terraformstate.ScopeLocatorHash byte-for-byte; the drift resolver join
// breaks if the two diverge (issue #203).
package scope
