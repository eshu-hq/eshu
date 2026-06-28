// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
// provider bindings, tag observations, and warnings. FactKindRegistry,
// CoreFactKinds, IsCoreFactKind, and SchemaVersion are backed by the generated
// registry from specs/fact-kind-registry.v1.yaml so schema versions, lifecycle
// owner, reducer domain, projection hook, admission hook, read surface, truth
// profile, semantic policy gate, and no-provider posture cannot drift from the
// facts package's accepted contracts. Package registry fact kind
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
// edges. Azure cloud fact kind constants and schema-version helpers live here
// for resource, relationship, tag, identity, resource-change, DNS,
// image-reference, and collection-warning evidence observed through Azure
// Resource Graph and bounded ARM fallback reads; the first slice emits the
// resource and collection-warning kinds. The S3 bucket posture fact kind constant and schema-version helpers
// live here for the derived, metadata-only per-bucket security posture and
// bounded external-principal grant evidence. S3 posture covers
// block-public-access, default-encryption detail, versioning and MFA-delete,
// object-ownership / ACL-disabled, access-logging target, replication presence,
// and policy-derived public/cross-account booleans. External-principal grant
// evidence covers public, cross-account, AWS service, or unsupported-principal
// metadata. Neither S3 fact carries the raw bucket policy document, statement
// bodies, actions, resources, conditions, ACL grants, object keys, or object
// data, and reducers project them separately. The derived IAM permission fact
// is metadata-only: it captures a normalized policy statement (effect, action
// set, resource pattern, condition-key summary) and never the raw policy JSON
// body or condition values. Secrets/IAM posture fact kind constants and
// schema-version helpers live here for source facts reported by the
// secrets_iam_posture family: IAM principals, trust policy statements,
// permission policy statements, policy attachments, permissions boundaries,
// instance profiles, optional Access Analyzer findings, Kubernetes
// ServiceAccount and RBAC metadata, workload identity usage, token posture,
// IRSA annotation evidence, EKS Pod Identity association metadata, Vault auth
// mounts and roles, Vault ACL policy summaries, Vault identity aliases and
// entities, Vault KV metadata, Vault secret-engine mounts, and coverage
// warnings. These facts preserve provider-native identity while omitting raw
// policy JSON, condition values, credentials, session tokens, raw Kubernetes
// subject names, Secret names, projected tokens, Vault tokens, AppRole secret
// IDs, raw Vault paths, key names, policy bodies, custom metadata values, and
// private URLs; reducers own all trust-chain, effective RBAC, Vault policy
// interpretation, and graph promotion decisions. CI/CD run fact kind constants
// and schema-version helpers live here for pipeline definition, run, job, step,
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
// incident systems. Jira work-item fact kind constants and schema-version
// helpers live here for issue, transition, external-link, project metadata,
// issue-type metadata, status metadata, workflow metadata, field metadata, and
// metadata-warning source evidence. Metadata facts keep raw names,
// descriptions, private URLs, custom-field IDs, and user identifiers out of the
// payload while preserving fingerprints and bounded categories for reader
// context. Incident-routing fact kind constants and schema-version
// helpers live here for applied PagerDuty and alert-route evidence observed
// from Terraform state and optional live PagerDuty service/integration
// observations; reducers compare that evidence with declared source and
// applied/live provider facts before presenting routing truth. Observability
// fact kind constants and schema-version helpers live here for declared,
// applied, and observed Grafana-stack evidence, including folders, metric
// scrape config, metric rules, metric routes, log routes, trace routes, and
// coverage warnings; reducers compare those facts before presenting coverage
// or drift truth. Semantic evidence fact kind constants and schema-version
// helpers live here for optional LLM-assisted documentation observations and
// code hints. They preserve source, chunk, provider-profile, prompt-version,
// redaction, policy, confidence, freshness, and replay metadata while keeping
// model output provenance-only until reducer or query consumers admit it.
// Semantic facts never carry raw provider keys, prompt payloads, private
// provider responses, bearer tokens, or secret values, and they do not directly
// promote service, deployment, runtime, vulnerability, or infrastructure truth.
// ValidateSemanticDocumentationObservationPayload and
// ValidateSemanticCodeHintPayload fail closed when semantic output lacks replay
// provenance or asks for direct canonical promotion.
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
// dismissals stay evidence rather than automatic suppressions. GCP cloud fact
// kind constants and schema-version helpers live here for Cloud Asset Inventory
// resource, relationship, tag, IAM policy observation, DNS record, image
// reference, and collection-warning evidence. GCP source facts never carry raw
// IAM policy JSON, secret values, object contents, startup scripts, public or
// private network addresses, provider response bodies, raw DNS names, or raw
// IAM member identities. Reducers own canonical CloudResource identity, tag
// evidence admission, relationship edges, image identity joins, drift, and
// query truth; raw IAM policy observations, DNS records, and collection warnings
// remain provenance-only or audit evidence until a later reducer/read-model
// contract admits them. SchemaVersion, SupportedSchemaVersions,
// ClassifySchemaVersion, and ValidateSchemaVersion expose the central
// fact-schema-version registry: one dispatch over every per-family schema
// version so reducers, projectors, the component activation path, and
// API/MCP/CLI diagnostics classify a collector's fact version the same way.
// The Compatibility classes (supported, unsupported_major, unsupported_minor,
// unknown_kind) implement the documented compatibility contract: a major
// change is rejected with no silent fallback, a minor or patch ahead of the
// supported version is not yet authoritative, and out-of-tree component kinds
// are unknown to core compatibility.
package facts
