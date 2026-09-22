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
// help one caller belong elsewhere.
//
// # The fact-kind registry and schema-version dispatch
//
// FactKindRegistry, FactKindRegistryEntryFor, CoreFactKinds, IsCoreFactKind,
// and SchemaVersion are backed by the generated registry from
// specs/fact-kind-registry.v1.yaml, so schema version, lifecycle owner,
// reducer domain, projection hook, admission hook, read surface, truth
// profile, semantic policy gate, and no-provider posture cannot drift from
// the accepted contracts. SchemaVersion, SupportedSchemaVersions,
// ClassifySchemaVersion, and ValidateSchemaVersion expose the central
// fact-schema-version registry: one dispatch over every per-family schema
// version so reducers, projectors, the component activation path, and
// API/MCP/CLI diagnostics classify a collector's fact version the same way.
// The Compatibility classes (supported, unsupported_major,
// unsupported_minor, unknown_kind) implement the documented compatibility
// contract: a major change is rejected with no silent fallback, a minor or
// patch ahead of the supported version is not yet authoritative, and
// out-of-tree component kinds are unknown to core compatibility.
//
// Every versioned core family is listed in schemaVersionFamilies, which
// flattens each family's <Family>FactKinds and <Family>SchemaVersion pair
// into one registry. Two families claiming one kind is a programming error,
// so the flattening panics rather than picking a winner;
// ValidateFactKindRegistry is what triggers it, and every registry test
// calls that.
//
// # Nested fact families
//
// Issue #6776 nested four fact-family groups into their own packages so this
// directory drops back under the 40-non-test-file cap the dirgate linter
// enforces. Each nested package's doc.go owns its family's contract,
// including its private-data boundary:
//
//   - go/internal/facts/cloud — AWS, Azure, GCP, Kubernetes live-cluster,
//     and Terraform state evidence, plus the derived EC2 instance, RDS
//     instance, S3 bucket, and S3 external-principal-grant posture facts.
//   - go/internal/facts/code — parser-emitted code-intelligence evidence:
//     dataflow scan markers and per-function records, function taint sources
//     and summaries, and resolved intraprocedural and interprocedural taint
//     findings.
//   - go/internal/facts/docs — the documentation fact family: sources,
//     documents, sections, links, entity mentions, non-authoritative claim
//     candidates, findings, evidence packets, and the bounded source-ACL
//     vocabulary. It is named docs, not documentation, because go/build
//     excludes every .go file in a package named documentation.
//   - go/internal/facts/supply/chain — OCI registry, language package
//     registry, SBOM and attestation, vulnerability intelligence, and
//     vulnerability suppression evidence.
//
// Shared payload substrate — StableID and the pointer/JSON-shape helpers a
// family's encoders use — lives in go/internal/facts/encode, because this
// package imports the nested families and a nested family therefore cannot
// import it back.
//
// The compat_cloud.go, compat_cloud_posture.go, compat_code.go,
// compat_docs.go, and compat_supply_chain.go files are this package's
// transitional compatibility surface for those moves: each entry is an alias
// or a thin forwarder with no behavior change, so every caller that spells a
// moved name facts.X keeps the same type and the same value. An entry is
// deleted once its last caller has moved to the nested package directly.
//
// # Families that remain here
//
// Secrets/IAM posture fact kind constants and schema-version helpers live
// here for source facts reported by the secrets_iam_posture family: IAM
// principals, trust policy statements, permission policy statements, policy
// attachments, permissions boundaries, instance profiles, optional Access
// Analyzer findings, Kubernetes ServiceAccount and RBAC metadata, workload
// identity usage, token posture, IRSA annotation evidence, EKS Pod Identity
// association metadata, Vault auth mounts and roles, Vault ACL policy
// summaries, Vault identity aliases and entities, Vault KV metadata, Vault
// secret-engine mounts, and coverage warnings. These facts preserve
// provider-native identity while omitting raw policy JSON, condition values,
// credentials, session tokens, raw Kubernetes subject names, Secret names,
// projected tokens, Vault tokens, AppRole secret IDs, raw Vault paths, key
// names, policy bodies, custom metadata values, and private URLs; reducers
// own all trust-chain, effective RBAC, Vault policy interpretation, and
// graph promotion decisions.
//
// CI/CD run fact kind constants and schema-version helpers live here for
// pipeline definition, run, job, step, artifact, trigger, environment, and
// warning evidence reported by providers. Provider security alert fact kind
// constants and schema-version helpers live here for repository-scoped
// provider alert evidence; reducers reconcile those alerts with owned
// dependency and impact facts.
//
// Incident-context fact kind constants and schema-version helpers live here
// for incident, lifecycle-event, and change-event source evidence reported
// by incident systems. Incident-routing fact kind constants and
// schema-version helpers live here for applied PagerDuty and alert-route
// evidence observed from Terraform state and optional live PagerDuty
// service/integration observations; reducers compare that evidence with
// declared source and applied/live provider facts before presenting routing
// truth.
//
// Jira work-item fact kind constants and schema-version helpers live here
// for issue, transition, external-link, project metadata, issue-type
// metadata, status metadata, workflow metadata, field metadata, and
// metadata-warning source evidence. Metadata facts keep raw names,
// descriptions, private URLs, custom-field IDs, and user identifiers out of
// the payload while preserving fingerprints and bounded categories for
// reader context.
//
// Observability fact kind constants and schema-version helpers live here for
// declared, applied, and observed Grafana-stack evidence, including folders,
// metric scrape config, metric rules, metric routes, log routes, trace
// routes, and coverage warnings; reducers compare those facts before
// presenting coverage or drift truth.
//
// Semantic evidence fact kind constants and schema-version helpers live here
// for optional LLM-assisted documentation observations and code hints. They
// preserve source, chunk, provider-profile, prompt-version, redaction,
// policy, confidence, freshness, and replay metadata while keeping model
// output provenance-only until reducer or query consumers admit it. Semantic
// facts never carry raw provider keys, prompt payloads, private provider
// responses, bearer tokens, or secret values, and they do not directly
// promote service, deployment, runtime, vulnerability, or infrastructure
// truth. ValidateSemanticDocumentationObservationPayload and
// ValidateSemanticCodeHintPayload fail closed when semantic output lacks
// replay provenance or asks for direct canonical promotion. The semantic
// payloads reuse the documentation family's ACLSummary and EvidenceRef
// shapes through go/internal/facts/docs.
//
// Service catalog fact kind constants and schema-version helpers live here
// for provider-native entity, ownership, repository link, dependency, API,
// operational link, scorecard, and warning evidence. Scanner-worker fact
// kind constants and schema-version helpers live here for source facts
// produced by isolated security analyzers, including coverage and
// unsupported analyzer evidence; reducers remain responsible for admitting
// any user-facing findings from that evidence. CODEOWNERS, submodule, and
// reducer-derived fact kind constants and schema-version helpers live here
// for repository ownership declarations, submodule pointers, and the facts
// the reducer itself emits.
package facts
