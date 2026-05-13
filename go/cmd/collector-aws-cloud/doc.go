// Command collector-aws-cloud runs the claim-aware AWS cloud collector.
//
// The command reads declarative AWS collector instances, claims bounded
// `(account, region, service_kind)` work items from the workflow store, obtains
// claim-scoped AWS credentials, and commits reported AWS facts through the
// shared ingestion store.
package main
