# AGENTS — cloudinventory

## Read first

1. `doc.go` — package contract.
2. `identity.go` — provider identity resolution into the shared uid keyspace.
3. `../../../reducer/cloud_inventory_admission.go` — the admission handler that
   consumes this resolver.
4. `docs/public/reference/multi-cloud-collector-contract.md` — Provider
   Identity and Reducer Contract sections.

## Invariants

- `ResolveProviderIdentity` returns a uid ONLY for
  `ResolutionOutcomeAdmitted`. Unresolved, ambiguous, and unsupported outcomes
  return an empty uid and must be counted by the caller, never fabricated.
- The uid is a pure, deterministic function of `(normalized provider, raw
  identity)`. Do not introduce time, randomness, counters, or caller scope into
  the uid.
- AWS and GCP raw identities are case-significant and hashed verbatim. Azure
  ARM ids are lower-cased before hashing because Azure treats them as
  case-insensitive. Do not change this without an identity ADR.
- The uid namespace prefix `cloud_resource:` is shared across aws/gcp/azure and
  must not collide with other reducer keyspaces.

## Common changes

- Add a provider: extend the provider switch and
  `providerIdentityIsKeyable`, add admitted/ambiguous/unresolved fixture cases,
  and keep the keyable shape conservative (a near-miss is ambiguous, not
  admitted).
- Tighten a keyable shape: add a fixture that proves the previously admitted
  malformed identity is now counted ambiguous.

## Anti-patterns

- Do not query Postgres or a graph backend here.
- Do not classify drift or project graph nodes/edges here.
- Do not lower-case or canonicalize AWS/GCP identities in a way that merges two
  distinct resources into one uid.

## What NOT to change without an ADR

- The hashing scheme or uid namespace prefix (changing it re-keys every
  canonical CloudResource row).
- The per-provider case sensitivity rules.
