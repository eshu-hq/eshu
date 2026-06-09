# Community Extension Index And Publication Workflow

Status: **PROPOSED - SECURITY REVIEW REQUIRED BEFORE IMPLEMENTATION.**

Refs #1830. Refs #1817, #1818, #1819, #1820, #1821, #1824, #1825, #1826,
#1828, #1829, #1898.

## Decision

The first community extension index should be a signed, repo-tracked manifest
index published from the Eshu project, not a live registry crawler and not a
GitHub-topic scrape. The index lists reviewed component manifests and points at
digest-pinned OCI artifacts. It does not grant runtime trust by itself.

Operators still choose their local trust mode. The public index is an
availability and review signal. Local install, enable, and claim-capable
execution remain separate steps enforced by the component manager, trust
policy, hosted operator policy, and workflow coordinator.

This preserves the existing extension boundary:

- extensions observe source systems and emit declared, versioned facts;
- reducers and query surfaces own canonical graph truth;
- unknown facts, unsafe provenance, disabled policy, incompatible core ranges,
  and revoked identities fail closed.

## Threat Model

### Assets

- operator trust policy: allowlists, revocation entries, and hosted enablement
  policy;
- extension artifacts: OCI image digest, manifest, signatures, provenance, and
  source repository identity;
- Eshu graph truth: reducer-owned canonical nodes, edges, read models, and
  query answers;
- credentials and secret references used by hosted collectors;
- index publication credentials and release automation;
- audit evidence that operators use to diagnose install, enable, and runtime
  failures.

### Trust Boundaries

| Boundary | Risk | Required control |
| --- | --- | --- |
| Contributor PR to Eshu index | Malicious metadata, typo-squatted IDs, unsafe fact claims | Maintainer review checklist, namespace ownership check, schema compatibility gate |
| Index metadata to local CLI | Index entry implies trust | CLI treats index entries as candidates only; local trust policy still verifies |
| Manifest to OCI artifact | Digest swap, unsigned artifact, wrong publisher | Digest-pinned artifacts plus strict-mode provenance verification from #1819 |
| Install to enable | Installed package starts work unexpectedly | Installed, enabled, and claim-capable states remain distinct |
| Hosted enablement to claims | Revoked or untrusted extension receives work | Hosted policy gate and idempotent coordinator handoff from #1820 and #1826 |
| Extension facts to graph truth | Extension writes or invents canonical graph state | Components emit facts only; reducers own graph materialization |
| Failure reporting to operators | Secrets leak through diagnostics | Low-cardinality failure classes and safe handles only |

### Attacker Capabilities

The design assumes an attacker may open public PRs, publish packages under a
lookalike name, compromise an extension maintainer account, submit a component
manifest with over-broad fact kinds, or try to exploit automatic install flows.
The design does not assume Eshu can safely execute arbitrary community code
because it appears in the index.

### Abuse Paths And Mitigations

| Abuse path | Impact | Mitigation |
| --- | --- | --- |
| Typosquatted component ID appears in the index | Operator installs the wrong package | Reverse-DNS or publisher-owned ID policy, review checklist, verified publisher metadata |
| Signed but unsafe component claims core fact kinds | Graph/query truth corruption | Fact-kind registry and collision checks from #1824 |
| Revoked publisher remains installable | Known-bad code continues to run | Revocation list wins over allowlist and index membership |
| Index automation publishes unreviewed entries | Supply-chain compromise | Manual maintainer approval for v1 and protected release credentials |
| Hosted activation reads raw credentials | Secret disclosure | Credential references only; no credentials in manifests, facts, logs, metrics, or issue bodies |
| Extension failure exposes private config | Operational data leak | Bounded failure classes and safe identifiers through #1825 |

## Index Shape

The v1 index is a versioned document with these fields per entry:

- component ID;
- publisher identity;
- version;
- lifecycle channel: `experimental`, `community-maintained`, `verified`, or
  `first-party`;
- manifest digest;
- OCI artifact reference and digest;
- compatible Eshu core range;
- component type and collector kinds;
- emitted fact kinds, schema versions, and source-confidence values;
- declared reducer or query consumer contracts;
- telemetry prefix;
- source repository and review PR;
- signature and provenance requirements;
- revocation status and replacement guidance when applicable.

The index may live in the Eshu repository at first. Publication can later move
to a generated static site or OCI index once the same metadata, signature, and
review rules are enforced. A GitHub topic search can be an advisory discovery
input, but it must not become the authoritative trust source.

## Lifecycle Channels

| Channel | Meaning | Runtime effect |
| --- | --- | --- |
| `experimental` | Reviewed only for index shape and obvious safety issues | Never trusted automatically |
| `community-maintained` | Maintainer owns updates; Eshu review accepted metadata and tests | Requires local allowlist or strict policy |
| `verified` | Eshu maintainers reviewed metadata, tests, provenance, and compatibility | Still requires operator trust policy |
| `first-party` | Built and released by the Eshu project | Subject to the same install, enable, and claim-capable gates |

No channel bypasses local disabled, allowlist, strict, revocation, compatible
core, or hosted policy checks.

## Publication Flow

1. Contributor builds an extension using the SDK/design contract from #1821.
2. Contributor emits a manifest with namespaced fact kinds, compatible core
   range, digest-pinned artifacts, source confidence, consumer contract, and
   telemetry prefix.
3. Contributor runs the conformance harness from #1823 and links proof in the
   publication PR.
4. Maintainer review checks package metadata, fact/schema ownership, trust and
   provenance posture, privacy, hosted safety, tests, and docs.
5. Publication automation verifies index schema, duplicate IDs, duplicate fact
   kinds, required links, digest format, and revocation state.
6. The reviewed entry lands in the index. Operators can inspect it, but install
   still runs local policy checks.

## Review Flow

Maintainers review three separate questions:

1. **Is the extension eligible for the index?** Check metadata completeness,
   namespace ownership, public source, deterministic tests, docs, and support
   owner.
2. **Is the artifact eligible for trust?** Check digest pinning, signature
   posture, publisher identity, compatible core range, emitted facts, and
   revocation policy.
3. **Is it safe to run in hosted deployments?** Check credential references,
   network/resource isolation, tenant visibility, failure surfacing, and
   claim-capable scheduling. #1826 owns this policy before hosted activation.

Security review is required before any `verified` or hosted-ready signal means
more than metadata acceptance.

## Revocation Flow

Revocation is explicit and wins over index membership, allowlists, and prior
installation. A revocation record names:

- revoked component ID, publisher identity, artifact digest, or version range;
- reason class such as `compromised_publisher`, `bad_signature`,
  `unsafe_fact_contract`, `credential_risk`, or `operator_requested`;
- replacement version or mitigation when one exists;
- effective timestamp and review source.

Local CLI and hosted runtimes must reject new install, enable, and claim-capable
work for revoked entries. Existing activations stop receiving new claims once
#1820 wires component activation to the workflow coordinator. Failure surfaces
must report the revocation reason class without leaking private configuration.

## Operator Install Flow

1. Operator searches or downloads the index entry.
2. Operator inspects the manifest, digest, compatibility, emitted facts,
   publisher, lifecycle channel, and review link.
3. Operator runs `eshu component verify` using `disabled`, `allowlist`, or
   `strict` mode. Strict mode remains fail-closed until #1819 lands.
4. Operator installs the package into the local component home only after local
   verification passes.
5. Operator enables a named instance only when config and credential references
   are ready.
6. Hosted operators opt into claim-capable execution through the policy model
   from #1826 and scheduler bridge from #1820.

## Minimal Implementation

The first minimal implementation should land in this order:

1. Harden the local component manager lifecycle and JSON readback in #1818.
2. Define the SDK boundary in #1821 and conformance harness in #1823.
3. Add fact-kind ownership and collision checks in #1824.
4. Document authoring and maintainer review in #1829.
5. Add the static index schema and verifier from #1898.
6. Wire API/MCP/CLI inventory and diagnostics in #1825.

The v1 index can remain manually curated. Automatic marketplace submission,
automatic artifact pull, GitHub topic discovery, central policy distribution,
and hosted claim-capable scheduling stay out of scope until their owning issues
land.

## Deliberately Manual For V1

- maintainer approval for every indexed extension;
- publisher identity transfer and key rotation;
- revocation approval and replacement guidance;
- hosted-ready designation;
- security review for strict provenance and hosted enablement;
- promotion from `experimental` to `verified`;
- removal of stale or unsupported packages.

## Proof Plan

- Static schema verifier rejects missing metadata, duplicate component IDs,
  duplicate fact-kind claims, mutable artifact tags, malformed digests, missing
  review links, unsupported channels, and revoked entries marked installable.
- Component CLI verification proves index membership does not bypass local
  trust mode.
- Conformance fixtures prove extension facts remain declared and namespaced.
- Docs build proves public authoring and operator guidance stays linked.
- Hosted proof waits for #1820 and #1826 because claim-capable execution is not
  an index concern.

## No-Observability-Change

This design adds no runtime, worker, queue, graph query, credential read,
metric, span, or log path. Future implementation issues must add
operator-facing diagnostics through the component inventory/status surfaces in
#1825 before hosted execution depends on index state.
