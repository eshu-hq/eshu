# Fact Envelope Reference

This page defines the fact contract that collectors, storage, projectors, and
reducers share. Use it when adding a collector family, changing fact identity,
or deciding whether a source observation is allowed to become graph truth.

For schema-version compatibility rules, use
[Fact Schema Versioning](fact-schema-versioning.md). For optional component
trust policy, use [Plugin Trust Model](plugin-trust-model.md) and
[Component Package Manager](component-package-manager.md).

## Contract

Collectors observe source truth and emit versioned facts. They do not write the
canonical graph, run durable-store DDL, or synthesize truth across sources.
Projection and reducer code own those later steps.

The Go source of truth is `go/internal/facts`. The durable envelope fields are:

| Field | Meaning |
| --- | --- |
| `FactID` | Durable row identity for one emitted fact. |
| `ScopeID` | Source-local scope, such as a repository, state snapshot, account/region/service, registry target, or documentation source. |
| `GenerationID` | Source observation generation. Consumers use it to separate current facts from stale rows. |
| `FactKind` | Domain kind, such as `terraform_state_resource` or `oci_registry.image_manifest`. |
| `StableFactKey` | Idempotency key inside a scope/generation. Re-emitting the same source fact must converge to the same stable key. |
| `SchemaVersion` | Semantic version for the fact payload contract. |
| `CollectorKind` | Collector family that emitted the fact, such as `git`, `terraform_state`, `aws`, `oci_registry`, `package_registry`, `ci_cd_run`, or `documentation`. |
| `FencingToken` | Claim or ownership token used by hosted collector paths to reject stale writers. |
| `SourceConfidence` | How Eshu learned the fact: `observed`, `reported`, `inferred`, `derived`, or compatibility-only `unknown`. |
| `ObservedAt` | Source observation time. |
| `Payload` | Fact-family payload. Sensitive source values must be redacted before the fact is emitted. |
| `IsTombstone` | Marks a deletion or retraction fact. |
| `SourceRef` | Source-local record reference with source system, scope, generation, fact key, URI, and source record ID. |

Facts must be idempotent under at-least-once delivery. A retry, duplicate
delivery, or collector restart must not create conflicting truth for the same
source observation.

## Source Confidence

`source_confidence` is required for new fact output.

| Value | Use |
| --- | --- |
| `observed` | Read directly from a source artifact, repository, document, state object, or fixture. |
| `reported` | Returned by an external system or provider API. |
| `inferred` | Concluded by correlating other evidence. |
| `derived` | Materialized from existing Eshu facts. |
| `unknown` | Legacy or system fallback only. New collectors and components must not emit it intentionally. |

Documentation claim candidates can be `observed` and still non-authoritative.
They prove a document says something; they do not prove the claim is
operationally true.

## Core Fact Families

The facts package owns the accepted core kinds and schema-version helpers. The
current families are:

| Family | Collector kind | Current fact kinds |
| --- | --- | --- |
| Documentation | `documentation` | `documentation_source`, `documentation_document`, `documentation_section`, `documentation_link`, `documentation_entity_mention`, `documentation_claim_candidate`, `documentation_finding`, `documentation_evidence_packet` |
| Terraform state | `terraform_state` for collected state, `git` for safe repo-local candidates | `terraform_state_candidate`, `terraform_state_snapshot`, `terraform_state_resource`, `terraform_state_output`, `terraform_state_module`, `terraform_state_provider_binding`, `terraform_state_tag_observation`, `terraform_state_warning` |
| AWS cloud | `aws` | `aws_resource`, `aws_relationship`, `aws_tag_observation`, `aws_dns_record`, `aws_image_reference`, `aws_warning` |
| OCI registry | `oci_registry` | `oci_registry.repository`, `oci_registry.image_tag_observation`, `oci_registry.image_manifest`, `oci_registry.image_index`, `oci_registry.image_descriptor`, `oci_registry.image_referrer`, `oci_registry.warning` |
| Package registry | `package_registry` | `package_registry.package`, `package_registry.package_version`, `package_registry.package_dependency`, `package_registry.package_artifact`, `package_registry.source_hint`, `package_registry.vulnerability_hint`, `package_registry.registry_event`, `package_registry.repository_hosting`, `package_registry.warning` |
| CI/CD runs | `ci_cd_run` | `ci.pipeline_definition`, `ci.run`, `ci.job`, `ci.step`, `ci.artifact`, `ci.trigger_edge`, `ci.environment_observation`, `ci.warning` |
| SBOM and attestations | collector-specific SBOM or attestation source | `sbom.document`, `sbom.component`, `sbom.dependency_relationship`, `sbom.external_reference`, `attestation.statement`, `attestation.slsa_provenance`, `attestation.signature_verification`, `sbom.warning` |
| Vulnerability intelligence | collector-specific vulnerability source | `vulnerability.source_snapshot`, `vulnerability.cve`, `vulnerability.affected_product`, `vulnerability.affected_package`, `vulnerability.os_package`, `vulnerability.epss_score`, `vulnerability.known_exploited`, `vulnerability.reference`, `vulnerability.warning`, `vulnerability.go_module_evidence`, `vulnerability.go_call_reachability` |
| Provider security alerts | `security_alert` | `security_alert.repository_alert` |
| Incident context | `pagerduty` for PagerDuty source collection | `incident.record`, `incident.lifecycle_event`, `change.record` |
| Jira work items | `jira` | `work_item.record`, `work_item.transition`, `work_item.external_link` |

Most current core families use schema version `1.0.0`.
`documentation_section` uses `1.1.0` because section payloads can carry
source-native content for updater diff generation. Check the fact-family helper
before emitting rows.

`vulnerability.source_snapshot` may include cache lifecycle metadata such as
cache artifact version, snapshot digest, cache update time, expiration,
freshness, and mode. These fields describe source observation state only; they
do not prove vulnerability impact without reducer-owned package, image, or
deployment evidence.

`vulnerability.os_package` carries one installed OS package observation from
an Alpine apk, Debian dpkg, or RPM-family queryformat snapshot. The payload
preserves distro, distro version, package manager, name, epoch, upstream
version, distro release (including Alpine `-rN`, Debian `~debNuM` /
`+debNuM`, and RPM `el9_2`-style release suffixes), arch, source package,
source version, repository URL, repository class (`vendor`, `third_party`,
`unknown`), vendor advisory source (`alpine`, `debian`, `redhat`, `fedora`,
`amazonlinux`, `rocky`, `alma`, `centos`, or empty), PURL, BOMRef identity,
and the verbatim `installed_version_raw`.
The `fixed_version_source` field is left empty by the collector; reducers
populate it after joining a matching vendor advisory. Reducers MUST NOT
compare `installed_version_raw` against an upstream OSV/NVD advisory's
fixed version: vendor backports (for example
`openssl 3.0.11-1~deb12u2`) often patch the upstream fix without changing
the upstream version number, and using upstream evidence would produce
false positives against the vendor-published build.

`security_alert.repository_alert` preserves repository-scoped provider alert
state, Dependabot alert ID/number, dependency ecosystem/name, manifest path,
dependency scope, relationship, GHSA/CVE IDs, vulnerable range, patched version,
severity, CVSS, EPSS, CWE summary, timestamps, and sanitized source URL. It is
reported provider evidence only. Reducers compare it with owned dependency and
impact facts through `security_alert_reconciliation`; they do not turn provider
state into `supply_chain_impact` truth.

`incident.record`, `incident.lifecycle_event`, and `change.record` preserve
provider-reported incident state, incident timeline entries, and related
change-event evidence. PagerDuty emits these as reported source evidence.
Reducers and read models must correlate them with runtime artifact, image,
commit, pull-request, and work-item evidence before presenting an incident
context path. Missing Jira links are valid incident evidence state and must not
block incident collection.

`work_item.record`, `work_item.transition`, and `work_item.external_link`
preserve Jira work-item state, changelog IDs, and remote-link IDs as provider
evidence. They do not imply incident ownership, deployment cause, code change,
or pull-request truth unless a reducer or query later proves that path through
separate source evidence.

## Promotion Rules

Facts are source evidence, not automatic graph truth.

- Documentation facts do not override code, deployment, runtime, or graph
  truth.
- Terraform-state facts must be redacted before emission. Terraform-state
  resource rows do not create cloud ownership by themselves.
- AWS facts are reported provider evidence. Reducers must corroborate workload,
  deployment, ownership, and environment truth before promotion.
- OCI registry tag observations are mutable evidence. Digest-addressed
  manifest, index, descriptor, and referrer facts are stronger identity anchors.
- Package registry source hints are not repository ownership or publication
  truth until reducer correlation admits them.
- CI success, environment names, SBOM metadata, vulnerability feeds, and
  provider alert state remain provenance until consumers correlate them with
  stronger runtime, package, image, or repository evidence.
- Incident and change facts remain provenance until consumers correlate them
  with stronger runtime, deployment, image, commit, pull-request, or work-item
  evidence.

ACL summaries and source-native documentation bodies are sensitive source
evidence. Do not emit them through logs or metrics. Evidence packet APIs must
fail closed unless a permission decision says the viewer can read the source.

## Change Checklist

When adding or changing a fact family:

1. Add or update fact kind constants and schema-version helpers in
   `go/internal/facts`.
2. Set `CollectorKind`, `SourceConfidence`, `ScopeID`, `GenerationID`, and
   `StableFactKey` deliberately in the emitter.
3. Redact sensitive payload values before emission.
4. Prove idempotency for duplicate delivery, retries, and stale generations.
5. Add the reducer, projector, or query consumer contract before presenting the
   fact as active platform truth.
6. Update this page and [Fact Schema Versioning](fact-schema-versioning.md) if
   the compatibility contract changes.
7. If an optional component emits the fact family, update its manifest contract
   in [Component Package Manager](component-package-manager.md).

## Related

- [Fact Schema Versioning](fact-schema-versioning.md)
- [Collector Authoring](../guides/collector-authoring.md)
- [Collector And Reducer Readiness](collector-reducer-readiness.md)
- [Component Package Manager](component-package-manager.md)
- [Plugin Trust Model](plugin-trust-model.md)
