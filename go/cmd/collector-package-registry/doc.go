// Command collector-package-registry runs the claim-aware package-registry
// collector.
//
// The command selects one enabled, claim-capable `package_registry` collector
// instance from ESHU_COLLECTOR_INSTANCES_JSON, resolves runtime-only credential
// environment references, preserves the target's parser document format, wires
// packageruntime.ClaimedSource through collector.ClaimedService, and commits
// emitted facts through the shared Postgres ingestion store with workflow claim
// fencing.
package main
