# AGENTS.md — internal/reducer/awscloud

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract` (aliased
`reducercontract`), `reducer/cloudjoin`, `reducer/containerimage`,
`reducer/factdecode`, `reducer/factload`, `reducer/factwrite`,
`reducer/gpphase`, `reducer/payloadcore`, `reducer/schemadecode`,
`internal/correlation/drift/cloudruntime` and its `engine`/`model`/`rules`
siblings, `internal/facts`, `internal/telemetry`, `internal/truth`, and the
factschema SDK. It must **never** import the parent `internal/reducer`
package, directly or transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a deref, a payload accessor, a tally formatter) goes to
  `reducer/payloadcore`, with a one-line forwarder left in root;
- vocabulary (a domain name, an intent, an outcome value) goes to
  `reducer/contract`, with a root alias;
- CloudResource node identity, the join index, or endpoint-resolution logic a
  still-in-root family also calls goes to `reducer/cloudjoin`;
- a symbol the root genuinely owns as logic with no other family consumer
  stays in root, and this package does not use it.

Read the declaration before deciding. A body of
`return payloadcore.DerefString(v)` is a forwarder and costs nothing to
bypass; a real implementation with consumers on both sides needs a deliberate
hoist to a shared leaf, which is how `cloudjoin.ResolveSource` and
`cloudjoin.SplitAWSFactEnvelopes` came to live there instead of here (issue
#6061): the reducer root's `aws_relationship_join.go` and
`aws_relationship_materialization.go` — which have NOT moved — call the
identical logic, and a family package may never import the reducer root.

## What must stay conservative

- **The fencing token is a database-issued value, never a host clock.**
  `AWSCloudRuntimeDriftFencingTokenIssuer` exists specifically to close a
  clock-skew ordering bug (#5875 P1). Do not add a fallback to
  `time.Now()`-derived ordering anywhere in the write path — see
  `awsCloudRuntimeDriftAdmissionQuery`'s doc comment
  (`aws_cloud_runtime_drift_admission.go`).
- **Readiness checks run before the evidence load, not after.** See
  `checkAWSCloudRuntimeDriftReadinessBeforeLoad`'s doc comment
  (`aws_cloud_runtime_drift_readiness.go`) for the TOCTOU window a
  post-load check would reopen.
- **A ContainerImage target existence check runs AFTER extraction, before any
  metric read.** `ExtractAWSCloudImageEdgeRows` has no graph access and counts
  a row "resolved" from ref-parseability alone;
  `filterRowsToExistingContainerImageTargets` reclassifies an unmaterialized
  target as skipped before `eshu_dp_aws_cloud_image_edges_total` or
  `CanonicalWrites` reads the tally (issue #5450 P1 follow-up). Do not read
  `tally.resolved` for a metric or evidence surface before that filter runs.
- **`splitAWSFactEnvelopes`/`resolveCloudResourceSource` are hoisted to
  `cloudjoin`, not duplicated here.** If you need one, import
  `cloudjoin.SplitAWSFactEnvelopes` / `cloudjoin.ResolveSource` directly; do
  not re-add a local copy that could drift from the root's still-in-use
  forwarder.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md` and `AGENTS.md`. Deleting one fails the build. It checks only
  that the files exist, so it is not evidence the contents are true; keep
  them true yourself.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`. If your
  file registers no instrument, use a `No-Observability-Change:` marker naming
  the signals that already cover the stage. Do not invent a metric that is
  absent from `go/internal/telemetry/instruments.go`.
- **`verify-doc-citations.sh`** — a `path:NNN` citation to a file in this
  package must stay accurate. If you move a line inside a moved file, repoint
  or drop the `:NNN` suffix in the citing doc, or the gate refuses the drift.
- **`verify-dirgate.sh`** — this directory counts against the per-directory
  file cap, and the `internal/reducer` row in
  `scripts/lib/dirgate-grandfather.tsv` is a monotonic ratchet. If you move
  files into or out of this package, re-derive the root row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the mirror with
  `generate-dirgate-grandfather-go.sh`. Never hand-edit either, and never
  grandfather a count upward.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose stem is `awscloud` or starts with `awscloud_`, so a compatibility
  shim must be named for its subject —
  `aws_cloud_family_compat.go`, not `awscloud_compat.go`.
- Do not suppress `dirgate` with `//nolint`.
- Do not export a test helper from the root to use here. Go test files cannot
  share unexported symbols across a package boundary; copy the helper into
  one of this package's `*_test_helpers_test.go` files instead, the way the
  moved tests already do (`aws_cloud_family_test_helpers_test.go`,
  `aws_cloud_runtime_drift_batch_test_helpers_test.go`,
  `aws_cloud_runtime_drift_writer_test_helpers_test.go`).
- Do not add a method to a type this package does not own from within a test
  file. `fakeAWSCloudRuntimeDriftExecer` in
  `aws_cloud_runtime_drift_writer_test_helpers_test.go` is a package-local
  fake precisely because the reducer root's equivalent
  (`fakeWorkloadIdentityExecer`) cannot have a method added to it from here.
