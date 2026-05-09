// Package facts defines the durable fact models and queue contracts that Eshu
// writes before graph projection.
//
// Facts are the contract between collection, parsing, queueing, projection,
// and reducer-owned materialization. Types in this package describe source
// truth in a form that survives retries, repair, and replay; consumers must
// treat fact identifiers and shapes as stable on-disk records. New fields
// must be additive and back-compatible, and convenience fields that only
// help one caller belong elsewhere. Terraform state fact kind constants and
// schema-version helpers live here so collectors, storage, and replay code use
// one accepted contract for state snapshots, resources, outputs, modules,
// provider bindings, tag observations, and warnings.
package facts
