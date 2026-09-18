# Collector cloud namespace

## Purpose

`collector/cloud` groups the collector-side public-cloud provider collectors
that read provider control-plane metadata for inventory and drift evidence.
It is a documentation-only namespace; implementation belongs in its leaf
packages and collection behavior stays in the owning provider packages.

## Ownership boundary

The namespace owns no runtime declarations, provider access, fact emission,
storage calls, graph writes, or telemetry. Its `azure` child owns only the
fixture-testable Azure fact engine (`azure` package: Resource Graph page
parsing, ARM identity normalization, extension redaction, bounded fact
emission) and its runtime wiring (`azureruntime` package: declarative scope
targets, fixture/live page-provider seam, claimed-source resolution). The
`azure` child makes no live Azure calls by default, commits no facts, writes
no graph truth, and answers no queries. Reducers own canonical CloudResource
identity, drift, relationship graph writes, and API truth.

AWS (`awscloud`) and GCP (`gcpcloud`) collectors join this namespace in later
leaves of the same issue; nothing about this nesting grants extracted
collectors database access or moves code out of the repository.

## Exported surface

None. This parent is documentation-only. See the `azure` child for
`Collector.Collect`, the Resource Graph and resourcechanges page parsers,
`ParseARMIdentity`, `RedactExtension`, and the fact-envelope constructors.

## Dependencies

None. The parent `doc.go` contains only package documentation and its package
clause. Dependencies belong to leaf packages. The `azure` child retains its
existing dependencies on the collector root, `facts`, `redact`, `scope`,
`telemetry`, and the SDK factschema contracts; a future extraction of this
subtree must address that coupling first.

## Telemetry

None. This namespace executes no runtime code and changes no observability
contract. The `azure` child's bounded-label instruments are documented in
`azure/README.md` and `azure/AGENTS.md`.
