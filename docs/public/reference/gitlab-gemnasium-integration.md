# GitLab Gemnasium (GLAD) Integration

Eshu ingests records from the GitLab Advisory Database (GLAD), the open
advisory dataset produced by GitLab's Gemnasium dependency-scanning project.
GLAD records are package-scoped: each record describes exactly one package and
the version range it considers affected. Eshu converts each record into
reported-confidence vulnerability facts and feeds them into the same
supply-chain vulnerability reducer that handles OSV, GHSA, NVD, and KEV
evidence.

GLAD sits at the source-evidence layer of the security intelligence chain. The
integration does not decide impact. It emits source facts only; reducers own
admission and conflict resolution across advisory sources. For the architecture
that surrounds this source, see [Security Intelligence](security-intelligence.md)
and the [Vulnerability Scanner Confidence Matrix](vulnerability-scanner-confidence.md).

## What is parsed

The source-contract input is one GLAD package advisory record. The field names
mirror the upstream Gemnasium YAML schema. The fields the integration reads
include:

| Field | Meaning |
| --- | --- |
| `PackageSlug` | `<ecosystem>/<name>` identity of the affected package (for example `npm/example-pkg` or `maven/com.example/artifact`). |
| `Identifier` / `Identifiers` | Primary advisory id plus every known alias (CVE, GHSA, GMS). |
| `AffectedRange` | Compact GLAD range expression describing affected versions. |
| `FixedVersions` | Source-reported fixed versions. |
| `AffectedVersions` / `NotImpacted` / `Solution` | Human-readable affected, not-impacted, and remediation statements kept as source truth. |
| `URLs` | Upstream advisory and reference URLs. |
| `CVSSv2` / `CVSSv3` / `CVSSv4` | Source CVSS vectors when present. |
| `CWEIDs` | Source-reported CWE identifiers. |
| `UUID` | Source advisory UUID. |
| `Title` / `Description` / `Date` / `PubDate` | Source metadata. |

### Affected-range grammar

`AffectedRange` is a compact expression parsed by `ParseGitLabAffectedRange`
into disjunctive normal form:

- Branches are separated by `||`. The overall range matches when any one branch
  matches.
- Within a branch, constraints are separated by whitespace and all must hold.
- Each constraint is an operator followed by a version. The accepted operators
  are `<=`, `>=`, `!=`, `<`, `>`, and `=`. The two-character operators are
  matched before the single-character ones.
- A constraint with no operator prefix is treated as `=`, provided the token
  begins with a digit or `v`/`V`. Unsupported operator prefixes such as `~>`,
  `^`, and `~` are rejected.
- An empty or whitespace-only expression parses to an open range with zero
  branches.

The parser preserves version strings verbatim, including prerelease (for
example `-rc.1`) and build-metadata (for example `+build.42`) suffixes. It does
not evaluate the range against a candidate version; reducers and
version-comparison helpers own that decision.

Example expression:

```text
<8.6.77||>=9.0.0 <9.9.1
```

This parses to two branches: one matching versions below `8.6.77`, and one
matching versions that are both `>=9.0.0` and `<9.9.1`.

## Ecosystem normalization

GLAD's ecosystem prefix is mapped onto Eshu's canonical ecosystem vocabulary by
`gitlabEcosystemToEshu`, which delegates to the shared `NormalizeEcosystem`
helper. Unknown values fall through as their lower-case form so the package
registry can decide whether to accept them.

| GLAD / source ecosystem inputs | Canonical Eshu ecosystem |
| --- | --- |
| `npm`, `node`, `nodejs`, `javascript`, `typescript` | `npm` |
| `pypi`, `pip`, `python` | `pypi` |
| `go`, `golang`, `gomod`, `go-module`, `go_module` | `gomod` |
| `maven`, `gradle`, `java` | `maven` |
| `nuget`, `dotnet`, `.net` | `nuget` |
| `composer`, `packagist`, `php` | `composer` |
| `rubygems`, `gem`, `ruby` | `rubygems` |
| `cargo`, `crate`, `crates`, `crates.io`, `rust` | `cargo` |
| `swift`, `swifturl`, `swiftpm`, `spm`, `swift-package-manager` | `swift` |
| `hex`, `hexpm`, `hex.pm` | `hex` |
| `pub`, `pub.dev`, `dart`, `dart-pub` | `pub` |
| `os`, `apk`, `alpine`, `deb`, `debian`, `rpm`, `rhel`, `ubuntu` | `os` |
| `generic` | `generic` |
| anything else | lower-cased source value (passed through) |

The package name within the slug is split by ecosystem-specific rules: npm and
Go modules pass the remainder through unchanged (so a scoped npm name or a full
Go module path stays intact), while Maven and other ecosystems split on the
first `/` into namespace and name.

## What facts it emits

The durable source name is `glad`. A dedicated source namespace keeps GLAD
observations from overwriting OSV, NVD, or GHSA observations of the same CVE.
Each GLAD advisory is converted into the following reported-confidence
envelopes by `GitLabAdvisoryEnvelopes`:

| Fact kind | Contents |
| --- | --- |
| `vulnerability.cve` | CVE/GHSA identity, alias list, CVSS v2/v3/v4 vectors, CWE ids, title, description, dates, the source advisory UUID, and correlation anchors. |
| `vulnerability.affected_package` | Source `package_slug`, ecosystem, package name, normalized package id, PURL, raw `affected_range`, the structured `parsed_affected_range`, human-readable affected/not-impacted/solution text, and source-reported fixed versions. |
| `vulnerability.reference` | One envelope per non-blank source URL, after credential sanitization. Blank or unparseable URLs are dropped. |

GLAD is package-scoped, so the stable fact keys embed `package_id` alongside
`source` and `advisory_id`. This prevents two GLAD records that describe the
same CVE for different packages from colliding on one fact id within a single
scope and generation. The CVE-level identity is still carried in the payload so
a reducer can join the records back together at admission time.

## Reducer path it joins

GLAD facts flow into the same supply-chain vulnerability ingestion and impact
reducer as OSV, GHSA, NVD, and KEV. The collector emits source facts at
reported confidence; it does not publish user-facing impact truth. The reducer
owns admission and conflict resolution across the advisory sources, and the
`glad` source is recognized in the reducer's impact provenance selection
alongside `osv`, `ghsa`, and `nvd`.

Owned impact findings require owned evidence: a provider-alert-only or
source-only advisory observation is not promoted into owned vulnerability
truth. This mirrors the posture described in
[Security Intelligence](security-intelligence.md) — source facts and owned
findings are distinct, and a zero-finding result is meaningful only alongside
coverage and readiness.

## MCP tools that expose the findings

GLAD-derived advisory evidence and any impact findings the reducer admits from
it are read through the supply-chain MCP tools. Each accepts a `glad` advisory
identifier where an advisory id is allowed. See the
[MCP Reference](mcp-reference.md) for full schemas.

| Tool | Purpose |
| --- | --- |
| `list_advisory_evidence` | List source-only advisory evidence by CVE, advisory, package, repository, service, or workload. Accepts a `source` filter such as `glad`. |
| `list_supply_chain_impact_findings` | List reducer-owned impact findings by CVE, package, repository, image digest, or status. Accepts a `glad` value in `advisory_id`. |
| `explain_supply_chain_impact` | Explain one reducer-owned finding or bounded advisory/package/repository path with evidence, anchors, remediation, and missing-evidence reasons. Accepts a `glad` `advisory_id`. |

## Example

An illustrative (anonymized) GLAD record:

```yaml
identifier: "CVE-2026-0001"
identifiers:
  - "CVE-2026-0001"
  - "GHSA-aaaa-bbbb-cccc"
  - "GMS-2026-99"
package_slug: "npm/example-pkg"
title: "Example denial-of-service in example-pkg"
affected_range: "<8.6.77||>=9.0.0 <9.9.1"
fixed_versions:
  - "8.6.77"
  - "9.9.1"
urls:
  - "https://advisories.example/CVE-2026-0001"
cvss_v3: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H"
cwe_ids:
  - "CWE-400"
uuid: "00000000-0000-0000-0000-000000000000"
```

Conceptually, this record produces:

- one `vulnerability.cve` fact carrying the CVE/GHSA identity, the alias list,
  the CVSS v3 vector, and `CWE-400`;
- one `vulnerability.affected_package` fact for the normalized `npm` package id
  with the raw `affected_range` and its parsed two-branch form; and
- one `vulnerability.reference` fact for the sanitized advisory URL.

There is no end-to-end fixture corpus under `testdata` for GLAD today, so this
page does not provide a runnable ingestion command. Instead, the parsing,
ecosystem normalization, and envelope emission are exercised by the unit tests
in the `go/internal/collector/vulnerabilityintelligence` package
(`gitlab_gemnasium_range_test.go`, `gitlab_gemnasium_envelope_test.go`, and
`gitlab_gemnasium_conflict_test.go`), which build `GitLabAdvisory` values
directly and assert the parsed range structure, normalized identity, fact
kinds, stable fact keys, and payloads.

## Related

- [Security Intelligence](security-intelligence.md)
- [Vulnerability Scanner Confidence Matrix](vulnerability-scanner-confidence.md)
- [Supply Chain Traceability](../supply-chain-traceability.md)
- [MCP Reference](mcp-reference.md)
