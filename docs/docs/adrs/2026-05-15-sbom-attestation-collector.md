# ADR: SBOM And Attestation Collector

**Date:** 2026-05-15
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

**Related:**

- Issue: `#16`
- Issue: `#12`
- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-05-10-oci-container-registry-collector.md`
- `2026-05-12-package-registry-collector.md`
- `2026-05-15-vulnerability-intelligence-collector.md`
- `2026-05-10-component-package-manager-and-optional-collector-activation.md`

---

## Context

Eshu's OCI registry collector already observes digest-addressed image truth and
preserves OCI referrers. It intentionally does not parse SBOMs, verify
signatures, interpret attestations, or decide vulnerability meaning.

Issue `#16` asks for a separate SBOM and attestation collector. That collector
should parse SBOM documents and provenance attestations, validate artifact
subjects, and emit component and provenance facts. It should not replace the
OCI registry collector. The OCI collector proves that an artifact exists and is
attached to a subject digest; the SBOM collector interprets the attached
document.

This ADR is design-only. Runtime implementation should wait until the OCI
registry collector has been proven in EKS or on the remote test machine. The
first implementation can still use local fixtures, but live registry and
signature behavior needs operator proof before the collector is treated as
production-ready.

## Source References

This ADR was checked against the current public contracts for the first source
set:

- NTIA SBOM minimum elements:
  <https://www.ntia.gov/report/2021/minimum-elements-software-bill-materials-sbom>
- SPDX 2.3 package information:
  <https://spdx.github.io/spdx-spec/v2.3/package-information/>
- SPDX 3.0.1 specification:
  <https://spdx.github.io/spdx-spec/v3.0.1/front/introduction/>
- CycloneDX specification overview:
  <https://cyclonedx.org/specification/overview/>
- in-toto attestation framework:
  <https://github.com/in-toto/attestation/blob/main/spec/README.md>
- SLSA 1.2 specification and provenance model:
  <https://slsa.dev/spec/v1.2/>
- Sigstore bundle format:
  <https://docs.sigstore.dev/about/bundle/>
- Cosign attestation verification:
  <https://docs.sigstore.dev/cosign/verifying/attestation/>
- OCI Distribution referrers API:
  <https://oci-playground.github.io/specs-latest/specs/distribution/v1.1.0-rc2/oci-distribution-spec.html>

## Source Contracts

The first implementation must preserve source-native document semantics.

| Source | Source truth | Contract notes |
| --- | --- | --- |
| OCI registry referrers | Digest-addressed SBOM, signature, attestation, and vulnerability-scan artifact descriptors attached to an OCI subject digest | The OCI registry collector owns discovery. Missing Referrers API support is a warning, not proof that a digest has no SBOM or attestation. |
| SPDX 2.3 | Package-centric software bill of materials with package, file, relationship, license, checksum, and external-reference metadata | SPDX 2.3 remains common in current SBOM tooling and should be fixture-supported. |
| SPDX 3.0.1 | Expanded System Package Data Exchange model covering software, build, security, AI, data, and other profiles | Initial parsing should focus on software package/component facts and preserve unsupported profiles as warnings. |
| CycloneDX 1.6/1.7 | BOM format for components, services, dependencies, vulnerabilities, external references, properties, and metadata | CycloneDX is the preferred first format for PURL-rich component evidence and in-toto predicates. |
| in-toto Statement | Attestation envelope binding a subject to a predicate type and predicate payload | Subject digest is the attachment anchor. Predicate parsing must be type-specific and fail closed on unknown critical predicates. |
| SLSA provenance | Build provenance predicate for where, when, how, and from what materials an artifact was produced | SLSA provenance is evidence, not proof of source ownership by itself. |
| Sigstore bundle / Cosign | Signature verification material, DSSE attestations, transparency log entries, and timestamp material | Verification status must be modeled separately from document parse validity. |

## Decision

Add a future collector family named `sbom_attestation`.

The collector owns:

- fetching configured SBOM and attestation documents
- consuming OCI referrer descriptors that the OCI collector already observed
- parsing SPDX and CycloneDX SBOM documents
- parsing in-toto, SLSA, and Sigstore/Cosign attestation material
- validating subject digest alignment
- recording signature and bundle verification status
- emitting typed source facts and warning facts

The collector does not own:

- OCI tag, manifest, index, or referrer discovery
- canonical graph writes
- vulnerability matching or prioritization
- package ownership admission
- workload, deployment, or runtime reachability correlation
- policy enforcement such as "only run signed images"

Reducer-owned correlation decides whether SBOM and attestation evidence can be
attached to an image, workload, repo, or vulnerability finding.

## Scope And Generation Model

The subject digest is the primary boundary. Tags are not acceptable anchors for
SBOM attachment.

Suggested scope IDs:

```text
sbom://oci/<subject-digest>/<document-digest>
attestation://oci/<subject-digest>/<statement-digest>
sigstore-bundle://oci/<subject-digest>/<bundle-digest>
```

Generation IDs should be immutable when possible:

- SBOM from OCI referrer: subject digest plus SBOM descriptor digest.
- SBOM from configured URL: content digest plus normalized source locator hash.
- in-toto/SLSA statement: statement digest or DSSE payload digest.
- Sigstore bundle: bundle digest plus verified artifact subject digest.

If the source is mutable, the generation must include a content digest and the
collector must emit a warning when the same source locator changes digest.

## Fact Families

Initial fact kinds should use `collector_kind=sbom_attestation`.

| Fact kind | Purpose |
| --- | --- |
| `sbom.document` | One SBOM document with format, spec version, document ID/serial, creation time, author/tool, source locator, subject digest, document digest, and parse status. |
| `sbom.component` | One package/component with component ID, name, version, type, PURL, CPE, SPDX ID, supplier, hashes, licenses, and source document reference. |
| `sbom.dependency_relationship` | One dependency or contains relationship from SPDX or CycloneDX, preserving relationship type and source document evidence. |
| `sbom.external_reference` | One external reference such as PURL, CPE, VCS URL, distribution URL, homepage, issue tracker, or advisory URL. |
| `attestation.statement` | One in-toto statement with statement type, predicate type, subject digests, payload digest, source locator, and parse status. |
| `attestation.slsa_provenance` | One SLSA provenance predicate with builder ID, build type, invocation/config digest, materials, source URI/digest, and build timestamps when present. |
| `attestation.signature_verification` | One verification result with verifier, policy reference, certificate identity, issuer, bundle digest, transparency/timestamp evidence flags, and result. |
| `sbom.warning` | Unsupported format/version, malformed document, oversized document skipped, subject mismatch, unverifiable signature, unknown predicate, incomplete composition, or redacted unsafe field. |

`source_confidence` should reflect how the collector learned the claim:

- `observed` for SBOM or attestation documents read directly from a configured
  source or local fixture.
- `reported` for registry-reported referrer descriptors consumed as inputs.
- `derived` only for normalized helper rows that are pure transformations of
  already stored SBOM or attestation facts.

## Identity And Subject Rules

Subject matching is the hard correctness boundary.

Rules:

1. SBOMs attached through OCI referrers must match the referrer subject digest.
2. in-toto statements must include a subject whose digest matches the target
   artifact digest before any attachment edge is admitted.
3. Sigstore or Cosign verification must verify against the same subject digest
   that Eshu will attach to the image or artifact.
4. SBOM package components without a document subject remain parse evidence,
   not image evidence.
5. Tags, repository names, image names, or homepage/source URLs are never
   enough to attach an SBOM to a container image.

Subject mismatch should emit warning facts and suppress canonical attachment.
It should not be silently ignored.

## Reducer Correlation Contract

The reducer should produce canonical attachment only when the evidence path is
explicit:

```text
OCI image digest
  -> oci_registry.image_referrer or configured subject source
  -> SBOM/attestation document subject digest
  -> parsed components or provenance facts
  -> optional package/vulnerability/workload joins
```

Candidate attachment statuses:

| Status | Meaning |
| --- | --- |
| `attached_verified` | Subject digest matched and signature/bundle verification passed under a named policy. |
| `attached_unverified` | Subject digest matched, but the document was unsigned or verification was not configured. |
| `attached_parse_only` | Subject digest matched and parsing succeeded, but signature material was absent or unsupported. |
| `subject_mismatch` | Document or attestation subject did not match the target digest; no canonical attachment. |
| `unknown_subject` | Document parsed but cannot be tied to an artifact digest. |
| `unparseable` | Source document could not be parsed into stable facts. |

Vulnerability impact joins must use `sbom.component` facts as component
evidence and then join through the vulnerability-intelligence reducer. The
SBOM collector must not emit `affected_by` or priority findings.

## Verification Model

Parse validity and trust verification are separate.

- A valid SBOM can be unsigned.
- A signed bundle can fail verification.
- A verified attestation can carry an unsupported predicate.
- A subject-matched SBOM can still be incomplete.

The read model must expose all of those states instead of collapsing them into
a boolean.

The first implementation should support verification-result facts even if
actual Cosign/Sigstore verification starts as a fixture-only parser. Live
verification requires an explicit trust-root and policy configuration before it
is enabled in a hosted runtime.

## Query And MCP Contract

Future read surfaces should be bounded and digest-first:

- list SBOM documents for an image digest
- list components for an image digest
- show SBOM verification status for an image digest
- show provenance materials for an artifact digest
- show image digests missing SBOM evidence
- show component evidence that can feed vulnerability impact matching

Responses must include:

- subject digest
- document digest or statement digest
- format and spec version
- verification status
- source freshness
- warning summaries
- `limit`, deterministic ordering, and `truncated`

Normal use must not require raw Cypher.

## Observability Requirements

The runtime must expose:

- source fetch duration by source kind
- document parse duration by format and version
- documents parsed by format and version
- components emitted by format
- dependency relationships emitted by format
- subject mismatch counts
- signature verification counts by result
- unsupported format, unsupported predicate, and malformed document counts
- oversized document skip counts
- fact emission counts by fact kind
- reducer attachment backlog and admitted/suppressed attachment counts

Metric labels must not include image digests, package names, PURLs, CPEs,
repository paths, URLs, certificate subjects, issuer URLs, or raw SBOM content.
Those values belong in facts, spans, or structured logs with redaction.

## Security And Privacy

SBOMs and attestations can reveal private package inventory, build systems,
source repositories, materials, internal service names, and deployment
structure. Treat them as sensitive customer evidence.

Rules:

- strip credentials and query secrets from source locators
- do not log raw SBOM or attestation payloads
- do not put component names, PURLs, CPEs, image digests, or certificate
  identities in metric labels
- keep signature verification policy explicit and auditable
- fail closed when trust roots or verification policies are missing
- expose missing or incomplete evidence instead of implying a digest is safe
- keep private CA, Fulcio, Rekor, or timestamp trust material out of repo docs

## Implementation Gate

Do not start the hosted runtime until there is recorded proof that the OCI
registry collector can discover image referrers in the target environment.

Do not start vulnerability-impact reducer work until these evidence paths are
proven together:

- OCI registry collector proof
- SBOM/attestation fixture proof
- package registry proof
- vulnerability intelligence source proof
- admin/status and telemetry proof for the involved runtimes

Acceptable live proof can come from EKS or the remote test machine.

## Implementation Plan

1. Land this ADR and update `#16` with the design gate.
2. Add fact schema tests for `sbom.*` and `attestation.*` facts with fixtures.
3. Add fixture parsers for CycloneDX 1.6/1.7 and SPDX 2.3 first.
4. Add SPDX 3.0.1 fixture parsing for the software profile subset.
5. Add in-toto Statement and SLSA provenance fixture parsing.
6. Add Sigstore bundle and Cosign verification-result fixtures.
7. Add reducer attachment tests for positive, negative, and ambiguous subject
   matching.
8. Add runtime wiring only after OCI referrer proof exists.
9. Add bounded API/MCP read surfaces after reducer attachment facts exist.

## Rejected Alternatives

### Parse SBOMs In The OCI Registry Collector

Rejected. The OCI registry collector already has the right boundary: discover
digest-addressed registry truth and preserve referrer descriptors. SBOM and
attestation parsing is a separate source-truth boundary.

### Attach SBOMs By Image Tag

Rejected. Tags are mutable observations. SBOM and attestation attachment must
be digest anchored.

### Treat Signature Verification As Parse Success

Rejected. A document can parse successfully while verification fails, and a
verified bundle can carry unsupported or incomplete predicates. The states must
remain separate.

### Start Vulnerability Matching In The SBOM Collector

Rejected. SBOMs provide component evidence. Vulnerability intelligence and
impact correlation belong to the vulnerability-intelligence and reducer lanes.

## Acceptance

This ADR is accepted when:

- source contracts are documented
- initial SBOM and attestation fact families are documented
- subject-digest attachment rules are documented
- verification status is separated from parse validity
- reducer ownership and non-promotion cases are explicit
- implementation is gated on OCI referrer proof
- issue `#16` references this ADR as the design baseline
