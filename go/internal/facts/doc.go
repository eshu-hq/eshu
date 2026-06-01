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
// here for resource, relationship, tag, DNS, image-reference,
// security-group-rule, derived IAM permission, and warning evidence reported by
// AWS service APIs. The security-group-rule kind is a derived posture fact: one
// normalized ingress/egress rule the reducer projects into network-reachability
// edges. The S3 bucket posture fact kind constant and schema-version helpers
// live here for the derived, metadata-only per-bucket security posture
// (block-public-access, default-encryption detail, versioning and MFA-delete,
// object-ownership / ACL-disabled, access-logging target, replication presence,
// and policy-derived public/cross-account booleans); it never carries the raw
// bucket policy document, and reducers project it separately. The derived IAM
// permission fact is metadata-only: it captures a normalized policy statement
// (effect, action set, resource pattern, condition-key summary) and never the
// raw policy JSON body or condition values. CI/CD run fact kind constants and
// schema-version helpers live here for pipeline definition, run, job, step,
// artifact, trigger, environment, and warning evidence reported by providers.
// SBOM and attestation fact kind constants and schema-version helpers live here
// for document, component, dependency, external reference, statement, SLSA
// provenance, signature verification, and warning evidence. Vulnerability
// intelligence fact kind constants and schema-version helpers live here for
// CVE, affected package/product, EPSS, KEV, reference, snapshot, and warning
// evidence. Provider security alert fact kind constants and schema-version
// helpers live here for repository-scoped provider alert evidence; reducers
// reconcile those alerts with owned dependency and impact facts.
// Incident-context fact kind constants and schema-version helpers live here for
// incident, lifecycle-event, and change-event source evidence reported by
// incident systems. Incident-routing fact kind constants and schema-version
// helpers live here for applied PagerDuty and alert-route evidence observed
// from Terraform state and optional live PagerDuty service/integration
// observations; reducers compare that evidence with declared source and
// applied/live provider facts before presenting routing truth. Observability
// fact kind constants and schema-version helpers live here for declared,
// applied, and observed Grafana-stack evidence, including folders, metric
// scrape config, metric rules, metric routes, log routes, trace routes, and
// coverage warnings; reducers compare those facts before presenting coverage
// or drift truth.
// Service catalog fact kind constants and schema-version helpers live here for
// provider-native entity, ownership, repository link, dependency, API,
// operational link, scorecard, and warning evidence. Scanner-worker fact kind
// constants and schema-version helpers live here for source facts produced by
// isolated security analyzers, including coverage and unsupported analyzer
// evidence; reducers remain responsible for admitting any user-facing findings
// from that evidence. Vulnerability suppression fact kind constants and
// schema-version helpers live here for VEX statements, operator-policy
// suppressions, and provider-dismissal pointers; reducers apply suppressions
// only when scope matches the finding identity and evidence path, and provider
// dismissals stay evidence rather than automatic suppressions.
package facts
