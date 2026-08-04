# #5699 cloud image-reference reopen ordering

## Disposition

The production reopen required by #5699 is already on `main`. PR #5700 added
`container_image_identity` to the maintenance replay shared by the ingester and
bootstrap index. PR #5706 then added the typed ECS `aws_image_reference`, its
matching ECR OCI manifest, and the non-vacuous B-12 MCP assertion requiring an
`explicit_digest` result for that digest.

The remaining gap was deterministic composition proof. The ordinary golden
pipeline starts collectors concurrently, so a green run did not prove that a
cloud image reference which succeeded empty before OCI activation would be
reopened and repaired. This branch adds that forced ordering as a required B-7
phase. It does not change production code, payload contracts, cassettes, query
shapes, SQL, indexes, worker counts, or runtime configuration.

## Forced lifecycle

`TestContainerImageIdentityCloudReferenceReopenOrderingPostgresLive` uses a
fresh schema on real Postgres for each ordering and exercises the production
fact loader, reducer queue, container-image handler, digest-v3 support writer,
and `ReopenSucceededReducerWorkItems` maintenance path.

The cloud-before-OCI case proves this sequence:

1. An active, typed AWS image-reference fact is visible while the matching
   typed OCI manifest belongs to a pending generation.
2. The identity intent is claimed, handled, and acknowledged successfully with
   zero canonical writes.
3. The OCI generation activates.
4. The production maintenance domain list reopens the exact succeeded work
   item, setting it to `pending` with `reopened_at` populated.
5. The same item is claimed and acknowledged again, producing one current
   support with the exact registry digest, repository ID,
   `outcome=exact_digest`, `identity_strength=explicit_digest`, and both AWS and
   OCI evidence fact IDs.
6. A second maintenance replay leaves the terminal current truth unchanged.

An isolated OCI-before-cloud control resolves on its first pass. The test
requires both orderings to finish with identical identity fields and
evidence IDs. Separate schemas prevent one ordering's active facts or durable
history from satisfying the other.

The existing B-12 `get_container_image_identity_inventory` assertion remains
the query-surface half of the proof. Its AWS/ECR cassette pair requires at
least one `explicit_digest` bucket for the ECS task digest. The new B-7 phase
proves the maintenance ordering that makes that same typed cloud-reference
classification converge; the snapshot proves the result remains visible
through MCP.

## BITES mutation and restored proof

The structural gate test was written first and failed before the B-7 phase was
wired:

```text
missing cloud image reopen ordering proof invocation: unindented call to
run_container_image_identity_cloud_reopen_ordering_proof()
shell_red_exit=1
```

For the behavioral mutation, only `DomainContainerImageIdentity` was removed
temporarily from `crossScopeCorrelationReopenDomains`. The live test then
observed zero identity reopen events and failed at the intended boundary:

```text
reducer_work_items_reopened domain=container_image_identity count=0
reopened work item status/reopened_at = "succeeded"/{... false}, want pending/non-null
--- FAIL: TestContainerImageIdentityCloudReferenceReopenOrderingPostgresLive
hostile_test_exit=1
```

Root-Cause Evidence: with only `DomainContainerImageIdentity` removed from the
maintenance replay list, the maintenance pass reported zero identity reopens
and the exact cloud work item remained `succeeded` with no `reopened_at` value.
Restoring that domain made the same test reopen the item and converge to the
explicit AWS/ECR digest identity. This isolates missing maintenance membership
as the cause of durable empty truth under the losing activation order.

The production domain entry was restored before the clean proof. The exact live
test then passed with two identity reopen events, one for the repair and one for
the idempotent replay:

```text
reducer_work_items_reopened domain=container_image_identity count=1
reducer_work_items_reopened domain=container_image_identity count=1
--- PASS: TestContainerImageIdentityCloudReferenceReopenOrderingPostgresLive (1.59s)
PASS
ok github.com/eshu-hq/eshu/go/internal/storage/postgres 4.854s
focused_test_exit=0
```

The B-7 helper runs that exact test once with `-count=1`, a 120-second timeout,
separate structured JSON and diagnostic streams, and explicit one-run,
one-pass, zero-skip validation. A skipped test or Go's successful zero-match
behavior cannot satisfy the phase.

The restored full gate passed after the final edits:

```text
container image identity cloud reopen ordering proof completed in 13s
mcp:get_container_image_identity_inventory: "buckets" has 1 results
summary: 521 pass, 0 required-fail, 1 advisory-warn
PASS: B-7 golden corpus gate green (elapsed 130s, budget ceiling 1800s)
```

The advisory was the existing maintenance-drain timing check: 26 seconds
observed against a 19-second advisory ceiling. Pipeline wall time and every
required drain, graph, HTTP, MCP, and demo assertion passed.

## Performance and observability

No-Regression Evidence: this branch adds proof only. The exact live test took
1.61 seconds inside a 4.885-second package invocation against local Postgres 18;
inside B-7 it took 1.24 seconds and the required phase completed in 13 seconds.
The complete gate finished in 130 seconds. This branch changes no production
query, write, queue, lock, lease, retry, or worker path, so it makes no
performance-improvement claim.

No-Observability-Change: production instrumentation is unchanged. Operators
retain the maintenance `reducer_work_items_reopened` structured log and its
per-domain count, plus the existing reducer queue depth, age, retry, and outcome
telemetry. The proof asserts the durable `pending` status and `reopened_at`
marker that those signals summarize.
