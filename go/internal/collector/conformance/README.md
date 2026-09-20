# Collector conformance contracts

## Purpose

`collector/conformance` groups the collector-side test harnesses that prove
collector behavior against shared contracts without live-provider
credentials. It is a documentation-only namespace; implementation belongs
in its leaf packages and collection behavior stays in the owning collector
packages.

## Ownership boundary

The namespace owns no runtime declarations, extraction, provider access,
fact emission, storage calls, graph writes, or telemetry. Its `parity`
child owns only the fixture-to-runtime parity harness for claim-driven
collectors. The future `contract` child owns fact-contract checks.

## Exported surface

None. This parent is documentation-only. See the `parity` child for its
exported surface.
