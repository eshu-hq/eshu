# cloudinventory

`cloudinventory` resolves a provider's raw cloud-inventory identity into the
shared canonical `cloud_resource_uid` keyspace. It is the deterministic identity
core behind the reducer's multi-cloud inventory admission path (issues #1997,
#1998), and the AWS counterpart of the same shared keyspace.

## What it does

`ResolveProviderIdentity(provider, raw)` takes a normalized provider token and
the provider raw identity a collector preserved:

| Provider | Raw identity | Keyable shape |
| --- | --- | --- |
| `aws` | ARN or provider-native id | `arn:` prefix or a non-trivial token |
| `gcp` | Cloud Asset Inventory full resource name | `//service.host/...` |
| `azure` | ARM resource id | rooted at `/subscriptions/` |

It returns a `Resolution` whose `Outcome` is one of:

- `admitted` — carries a stable `CloudResourceUID`.
- `unresolved` — raw identity was blank.
- `ambiguous` — raw identity did not match the provider's expected shape.
- `unsupported` — provider has no canonical keyspace yet.

Only `admitted` carries a uid. Every other outcome is counted by the caller and
surfaced as evidence; the package never fabricates a uid for an identity it
cannot key.

## Why it is deterministic

The uid is a namespaced `SHA-256` over the normalized provider plus the raw
identity (`cloud_resource:<hex>`). Azure ARM ids are case-insensitive per Azure
and are lower-cased before hashing so two casings of the same resource converge;
AWS and GCP identities are case-significant and hashed verbatim. Because the uid
is a pure function of its inputs, two reducer workers — or a retried worker —
that admit the same resource derive the same uid and converge on one canonical
row instead of racing.

## Boundaries

This package does identity resolution only. It does not read Postgres or a graph
backend, classify drift, or project graph nodes or edges. The reducer
`cloud_inventory_admission` handler owns loading, persistence, the evidence-layer
contract (declared/applied/observed), and telemetry.
