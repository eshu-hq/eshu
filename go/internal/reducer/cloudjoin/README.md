# cloudjoin

Owns the in-memory identity join from an AWS endpoint identity to the uid of a
materialized `CloudResource` node.

This package was carved out of the flat `internal/reducer` root under issue
#6061. It is a shared-core leaf, not a domain family: it registers no handler
and owns no domain, it just answers "which scanned node is this identity?".

## What it owns

| piece | file | what it does |
|---|---|---|
| `CloudResourceJoinIndex` | `cloud_resource_join_index.go` | four maps into one uid space: by ARN, by uid, by bare resource id, by correlation anchor |
| `BuildCloudResourceJoinIndex` | `cloud_resource_join_index.go` | folds a scope generation's `aws_resource` facts into the index |
| `CloudResourceUID` | `cloud_resource_join_index.go` | computes the stable node identity from account, region, type and id |
| `CloudResourceJoinIndex.ARNForUID` | `cloud_resource_join_index.go` | reverses a uid back to the ARN it was keyed on |

The index never fabricates a uid. Every entry comes from an `aws_resource` fact
that carried its own `account_id` and `region`, so a cross-account or
cross-region ARN resolves only if that account+region resource was scanned in
the same scope. That is the trust boundary: resolution is index membership, not
string construction.

`CloudResourceUID` takes the same inputs as the `aws_resource` fact's stable
key. That is what lets a relationship fact's resolved target recompute the
identical uid without a graph round trip.

## Why this is a shared leaf

The AWS relationship, security-group reachability and IAM privilege-escalation
slices at the reducer root all resolve endpoints against this index, and so does
the `reducer/iamcan` family. A family package may never import the reducer root,
so the index has to live below both. The root keeps `cloudResourceJoinIndex`,
`buildCloudResourceJoinIndex` and `cloudResourceUID` as an alias and two
forwarders in `cloud_resource_join_index_compat.go`, so its own callers compile
unchanged.

Because the type now lives here, the root cannot attach methods to it. The three
root-only lookups became free functions in `aws_relationship_join.go`
(`resolveCloudResourceSource`, `resolveCloudResourceTarget`) rather than moving,
since they return the root's `join_mode` metric vocabulary.

Imports point strictly downward. This package reaches `reducer/factdecode`,
`reducer/payloadcore`, `reducer/schemadecode` and `internal/facts`, and it never
imports the parent `internal/reducer` package.

## Telemetry

This package registers no instrument and opens no connection.

A fact whose identity payload cannot be decoded is returned to the calling
handler as a quarantine value; the handler is what increments
`eshu_dp_reducer_input_invalid_facts_total`. That isolation is deliberate and
load-bearing: one malformed resource fact is skipped and dead-lettered
individually, so it never empties the join index for the whole scope, which
would stall every edge domain gating on the canonical-nodes-committed phase.

No-Regression Evidence: #6061 relocates this code from
`aws_relationship_join.go` and `aws_resource_materialization.go` without
changing it. The bodies are unchanged; the diff is the package clause, the four
index fields becoming exported, and root forwarders replacing the original
declarations. Behavior is covered by the existing root suites that exercise the
index (`aws_relationship_join_test.go`, `aws_resource_materialization_test.go`,
the security-group and IAM-escalation suites) plus `internal/reducer/iamcan`.
Measured on this branch: `go build ./...` exits 0, `go vet
./internal/reducer/...` exits 0, and `go test ./internal/reducer/... -count=1`
passes. Binary output was not compared and no such claim is made here.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, or
log field. This package emitted no signal before the move and emits none after.

## Gotchas / invariants

- **Do not import the reducer root from here.** This is a leaf; reaching upward
  would recreate the cycle the package exists to break.
- **The maps are exported, so they are mutable by callers.** Two slices
  deliberately augment a built index with extra ARN keys (instance profiles,
  IAM role/user nodes). First writer wins there, and a caller that re-points an
  existing ARN silently changes another slice's resolution. Add, never
  overwrite.
- **`ARNForUID` reporting false is not the same as an empty ARN.** A resource
  identified only by a bare resource id is indexed without an ARN; callers that
  collapse the two lose that distinction.
- **`CloudResourceUID` is a durable identity.** It is recomputed independently
  by the edge projections, so changing its inputs orphans every node already
  written under the old uid.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
