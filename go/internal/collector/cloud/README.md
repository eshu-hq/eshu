# Collector cloud namespace

## Purpose

`collector/cloud` groups the collector-side public-cloud provider collectors
that read provider control-plane metadata for inventory and drift evidence.
It is a documentation-only namespace; implementation belongs in its leaf
packages and collection behavior stays in the owning provider packages.

## Ownership boundary

The namespace owns no runtime declarations, provider access, fact emission,
storage calls, graph writes, or telemetry. Its `azure` leaf was removed by the
#8853 POC-retirement removal: the fixture-testable Azure fact engine and its
runtime wiring now live in the `eshu-collector-azure-cloud` satellite, and this
namespace currently holds no leaf packages. Reducers own canonical
CloudResource identity, drift, relationship graph writes, and API truth.

AWS (`awscloud`) and GCP (`gcpcloud`) collectors join this namespace in later
leaves of the same issue; nothing about this nesting grants extracted
collectors database access or moves code out of the repository.

## Exported surface

None. This parent is documentation-only. The retired `azure` leaf's
`Collector.Collect`, Resource Graph and resourcechanges page parsers,
`ParseARMIdentity`, `RedactExtension`, and fact-envelope constructors now live
in the `eshu-collector-azure-cloud` satellite.

## Dependencies

None. The parent `doc.go` contains only package documentation and its package
clause. Dependencies belong to leaf packages.

## Telemetry

None. This namespace executes no runtime code and changes no observability
contract. The retired `azure` leaf's bounded-label instruments left with the
#8853 extraction; the satellite emits its own copies.
