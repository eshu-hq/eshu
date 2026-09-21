# AGENTS.md - services/servicecatalogappregistry/awssdk guidance

## Read First

1. `README.md` - adapter purpose, read surface, and telemetry contract.
2. `client.go` - the `apiClient` read interface, pagination, and type mapping.
3. `exclusion_test.go` - the metadata-only reflection guard.
4. `../README.md` - AppRegistry scanner contract.

## Invariants

- The `apiClient` interface lists ONLY List read operations. Never add a
  Get/Describe content read (`GetAttributeGroup`, `GetConfiguration`,
  `GetAssociatedResource`) or any Create/Update/Delete/Associate/Disassociate/
  Put/Tag mutation. The exclusion test enforces this at build time.
- Never read or map the attribute-group content body or an associated-resource
  tag value (`ResourceDetails.TagValue`). Map only the resource identity, name,
  and AppRegistry resource type.
- Page every list API to exhaustion through `NextToken`.
- Do not retry inside the adapter. Surface throttle errors to the shared
  classifier via `recordAPICall`.
- Wrap every API call in `recordAPICall` so the shared span and
  API-call/throttle counters stay consistent across scanners.
- Trim whitespace on every string used as an id, ARN, or tag key.

## Common Changes

- Add a new metadata field by extending the scanner-owned type in the parent
  package and mapping it here from the SDK summary, with a focused adapter test
  first. If the field can carry a content body or tag value, do not map it.

## What Not To Change Without An ADR

- Do not add a content-read, mutation, or association-write method to the
  `apiClient` interface.
- Do not add internal retry, backoff, or credential loading here.
- Do not add bespoke metrics; reuse the shared API-call/throttle counters.
