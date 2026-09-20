# Collector document contracts

## Purpose

`collector/document` groups the collector-side packages that turn offline
documentation exports into source-neutral documentation facts. It is a
documentation-only namespace; implementation belongs in its leaf packages
and collection behavior stays in the owning collector packages.

## Ownership boundary

The namespace owns no runtime declarations, extraction, provider access,
fact emission, storage calls, graph writes, or telemetry. Its `export`
child owns only offline issue, ticket, and chat export parsing gated by
manifest preflight. Future `media` and `ocr` children own their format
slices under the same offline-only posture.

## Exported surface

None. This parent is documentation-only. See the `export` child for its
exported surface.
