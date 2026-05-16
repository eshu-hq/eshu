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
// provider bindings, tag observations, and warnings. Package registry fact kind
// constants and schema-version helpers live here for package, version,
// dependency, artifact, source-hint, vulnerability-hint, event, hosting, and
// warning evidence reported by package feeds. OCI registry fact kind constants
// and schema-version helpers live here for repository, tag, manifest, index,
// descriptor, referrer, and warning evidence reported by OCI-compatible
// registries. AWS cloud fact kind constants and schema-version helpers live
// here for resource, relationship, tag, DNS, image-reference, and warning
// evidence reported by AWS service APIs. CI/CD run fact kind constants and
// schema-version helpers live here for pipeline definition, run, job, step,
// artifact, trigger, environment, and warning evidence reported by providers.
// SBOM and attestation fact kind constants and schema-version helpers live here
// for document, component, dependency, external reference, statement, SLSA
// provenance, signature verification, and warning evidence.
package facts
