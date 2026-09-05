# Reducer payload core

## Purpose

`payloadcore` holds the generic payload readers and string helpers that both the
parent `internal/reducer` package and its domain-family subpackages need.

It exists because of an import-direction constraint, not because the helpers
wanted a home. As families move out of the reducer root, the root imports them
to construct their handlers, so a family cannot import the root back without
creating a cycle — and an unexported helper in the root is unreachable from a
subpackage in either direction. Hoisting the generic helpers below both sides
resolves it. This package is the bottom of that stack (issue #6061, epic #6053).

## Ownership boundary

This package owns generic payload access and string normalization. It owns no
domain knowledge. It must remain a leaf: it imports `internal/facts` and the
standard library, and nothing else from the reducer tree.

The parent reducer package continues to own registry composition, runtime and
queue execution, projection, and graph writes. Domain families own their
handlers, writers, decisions, and lookups — those are the family's product and
do not belong here even when more than one family reads them.

## Exported surface

- Payload access: `PayloadStr`, `PayloadString`, `SemanticPayloadString`,
  `PayloadMap`, `PayloadOrderedStrings`, `SemanticPayloadStringSlice`,
  `PayloadBool`, `PayloadBoolPointerValue`, `BoolPayload`, `CopyPayload`,
  `MapSlice`, `ToStringSlice`, `AnyToString`, `PayloadInt`, `PayloadStrings`.
- String normalization: `UniqueSortedStrings`, `AppendUniqueString`,
  `CompactStringSlice`, `CleanFactFilterValues`, `NonNilStrings`,
  `FirstNonBlank`, `DerefString`, `DerefBool`, `SortedKeys`, `FormatTally`,
  `MissingStrings`.
- Identity derivation: `RepositoryIDFromReducerScope`,
  `SupplyChainWorkloadIDsFromPayload`, `OCIRepositoryID`.
- Source ordering: `SourceOrderKey`, `PreferMaxSourceOrderKey`,
  `SourceOrderKeyField`, `SourceOrderKeySeparator`,
  `SourceOrderKeyTimestampLayout`.

## Near-duplicate accessors

Three helpers read a string out of a payload and they are not interchangeable:

| helper | non-string values | absent / nil | value rendering as `"<nil>"` |
|---|---|---|---|
| `PayloadStr` | rendered | `""` | `""` |
| `PayloadString` | rendered | `""` | `"<nil>"` |
| `SemanticPayloadString` | `""` | `""` | `"<nil>"` when the value is the string, `""` when it is a typed nil |

Two read a boolean: `PayloadBool` accepts a bool or a case-insensitive
`"true"`/`"false"` string; `BoolPayload` accepts only a real bool. Collapsing
either pair changes projected truth.

## Compatibility

The parent package keeps an unexported forwarder for most symbols the reducer
root still references, so existing call sites are unchanged. There are two
exceptions. `FirstNonBlank` has no forwarder at all: its 46 call sites across 19
root files call this package directly, because a forwarder around it exceeded
the inline budget (see below). And two remaining root call sites were
repointed directly at this package so that the function containing them keeps
its own inlinability — in `shared_projection_worker_refresh_fence.go` and
`supply_chain_impact_match.go`. Three earlier bypass sites —
`container_image_identity_ref_parsing.go`, `crossplane_satisfied_by_edge_rows.go`,
and `observability_coverage_metadata.go` — have since moved into their own
family subpackages (`containerimage`, `crossplane`, and `obscoverage`
respectively), where a direct `payloadcore` import is unremarkable rather than
a bypass. The forwarder for `payloadBoolPointerValue` was dropped outright once
that last caller stopped using it. Those forwarders are transitional: each
one is deleted once its last root caller has moved into a family subpackage.
They are function statements rather than function-valued variables because this
code sits on the reducer write path and a function-valued variable cannot be
inlined.

One forwarder could not be kept. `firstNonBlank` is inlinable in the reducer
root at cost 78; wrapping it pushes the wrapper to cost 82, over Go's budget of
80, because `FirstNonBlank` inlines into its own forwarder. Its call sites
therefore call `payloadcore.FirstNonBlank` directly, which holds inlining where
it was: `go build -gcflags=-m ./internal/reducer` on go1.27.1 reports an
inlined call at every one of those 46 sites. Keeping the forwarder would have
made it zero.

Forwarders are not free elsewhere. Three functions lost inlinability, one call
site each, because calling a forwarder rather than the original raises the
caller past the budget; sixteen one-line root forwarders gained it, and
package-wide inlined call sites rose from 12291 to 13985 on go1.27.0
(the figures shift by about 60 on go1.26.6, so the toolchain belongs with the
number). The design doc's
No-Regression Evidence note names all of them.
