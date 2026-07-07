# Evidence: IAM instance-profile role_arns nested-attributes read (#4633)

Scope: `go/internal/reducer/iam_instance_profile_role_edge_rows.go` changes one
read in `ExtractIAMInstanceProfileRoleEdgeRows` from `payloadStrings(resource.Attributes, "", "role_arns")`
to `payloadStrings(payloadAttributes(resource.Attributes), "", "role_arns")`. The
CI hot-path gate flags any change under `go/internal/reducer/` by location, so
this file records the required evidence.

## What changed

The awscloud IAM scanner emitter (`awscloud.NewResourceEnvelope` ->
`awsPayloadAttributes`) nests every scanner-provided attribute, including
`role_arns`, one level deeper under a single top-level `"attributes"` key. Because
`"attributes"` is not in `resourceKnownKeys`, the typed decode passes it through
at `resource.Attributes["attributes"]["role_arns"]`, not
`resource.Attributes["role_arns"]`. The old top-level read resolved nothing
against a real emitted fact, so every instance-profile -> role `HAS_ROLE` edge was
silently dropped. The fix reads through the shared `payloadAttributes` helper,
matching every other reducer site that reads a service-specific AWS attribute
(for example `ec2_block_device_kms_posture_index.go`).

No new Cypher, no graph writes, no worker/lease/batch/concurrency logic, no query
shape, no runtime/Compose/Helm setting. Edge resolution is still exact ARN
membership in the same bounded in-memory index.

No-Regression Evidence: the change swaps one map lookup for a nested map lookup
on the same decoded struct — both O(1), no added traversal, allocation, lock, or
query work, backend-neutral. This is a correctness fix, so the intended behavior
delta is that `HAS_ROLE` edges that were dropped now materialize. Proven by a
failing-then-green regression against the real nested-attributes emitter shape:
before the fix `ResolvesRoles` produced 0 rows (want 2), `UnscannedRoleSkipped`
counted 0 target_unresolved (want 1), and `DuplicateInputOneEdge` produced 0 rows
(want 1); after the fix all four `IAMInstanceProfileRoleEdgeRows` subtests pass,
the full `./internal/reducer` package passes, and it passes again under `-race`.
The golden-corpus gate does not exercise this path (zero hits for
`aws_iam_instance_profile` / `role_arns` / `HAS_ROLE` across the B-12 snapshot and
the cassettes), so there is no snapshot or cassette delta.

No-Observability-Change: no metrics, spans, or logs are added or altered. The
resolved/skipped tallies the extractor already returned are unchanged in shape;
the fix only makes the resolved tally reflect the edges that were always meant to
resolve.
