# AGENTS.md — internal/reducer/ec2instance

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`, `reducer/cloudjoin`,
`reducer/factdecode`, `reducer/factload`, `reducer/gpphase`,
`reducer/payloadcore`, `reducer/schemadecode`, `internal/facts`,
`internal/graph/edgetype`, `internal/telemetry`, `internal/truth`, and
`pkg/log`. It must **never** import the parent `internal/reducer` package or
any sibling family package, directly or transitively.

The node domain publishes the `cloud_resource_uid` / `canonical_nodes_committed`
phase the identity domain gates on, and the USES_PROFILE edge slice
(`reducer/ec2usesprofile`), the block-device KMS posture slice
(`reducer/ec2blockkms`), and the AWS relationship slice (still in the reducer
root) all gate on the node phase too — but none of that is a Go dependency:
every gate resolves through the durable `gpphase` keyspace, never through an
import. If a new symbol here gains a second consumer outside this package,
hoist it to the owning leaf the way `cloudjoin`, `factdecode`, and `gpphase`
were hoisted — do not export it for lateral import, and do not duplicate
production logic across families. (Test-only envelope builders are the
exception: Go test files cannot share unexported symbols across a package
boundary, so those are duplicated verbatim with a comment, never exported.)
`ec2InstanceIdentityUIDForResource` is the deliberate single-family inline
mirror of the root's `cloudResourceUIDForResource`: the root's workload slice
still calls the original, so hoisting would drag a staying caller along for
no multi-family reason.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a stringer, a payload accessor, a source-uid extractor)
  goes to `reducer/payloadcore`, with a one-line forwarder left in root;
- vocabulary (a domain name, an intent, an outcome value, a ledger) goes to
  `reducer/contract`, with a root alias;
- the CloudResource join index or node uid goes to `reducer/cloudjoin`;
- the `aws_resource`/`ec2_instance_posture` decodes go to
  `reducer/schemadecode`;
- readiness phases and keyspaces go to `reducer/gpphase`;
- a symbol the root genuinely owns as logic stays in root, and this package
  does not use it.

Read the declaration before deciding. A body of
`return factload.LoadFactsForKinds(...)` is a forwarder and costs nothing to
bypass; a real implementation with consumers on both sides needs a deliberate
hoist to a shared leaf, which is how `cloudjoin`, `factdecode`, and `gpphase`
came to exist.

## What must stay conservative

- The node handler MUST publish the canonical-nodes-committed phase only
  after the node write succeeds (or is a legitimate no-op for an empty
  generation). Publishing before a successful write lets the edge slices
  resolve against nodes that never committed; not publishing on an empty
  generation blocks them forever.
- The identity handler MUST gate on the EC2 instance node phase before
  loading facts. Do not weaken this to an open gate — a property write
  against a node set that has not committed is a fabricated augmentation,
  not a retry.
- The identity handler MUST NEVER create a CloudResource node and MUST NEVER
  write a base identity/posture field the node domain owns (`arn`,
  `resource_id`, `resource_type`, `name`, `state`, and the posture
  scalars/booleans). Disjointness is what makes the dual-domain MERGE safe;
  the cross-domain test in
  `ec2_instance_identity_materialization_test.go` guards both directions.
- A malformed `aws_resource` or `ec2_instance_posture` fact is quarantined
  per-fact (`input_invalid`) while every valid fact in the same batch still
  projects.
- The node extractor MUST NEVER carry user-data content (only the
  `user_data_present` boolean), the raw public IP, per-volume block devices,
  or any topology field the posture fact does not carry — materializing
  absent data would be fabrication.

## Telemetry honesty

The nodes counter is recorded even at zero for a non-empty generation (a
zero count with facts present means every posture fact lacked an identity —
itself a signal); the skip counter is map-driven and emits only nonzero
skip reasons (`missing_identity` / `tombstone`). Never write that any
skip series here is "recorded even at zero" — a quiet generation emits no
per-reason series, and the completion log is the always-present record.
Check the tally code before writing the sentence.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md` and `AGENTS.md`. It checks only that the files exist, so it is
  not evidence the contents are true; keep them true yourself.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`. This
  family's metric instruments are registered in `internal/telemetry`, and the
  moved materialization files keep their projection rows (repointed to the new
  paths); the node rows file has its own honest No-Observability-Change row.
  Do not invent a metric that is absent from `go/internal/telemetry/instruments.go`.
- **`verify-doc-citations.sh`** — a raw `path.go:NNN` LINE citation's
  "context" is the entire containing markdown line, shared byte-for-byte by
  every other `.go:NNN` citation on that same line; editing ANY part of a
  multi-citation table row invalidates all of them, not just the one being
  fixed, and the gate refuses even `-update` on such a row. Prefer the anchor
  form the gate's own header prefers (no line number). The gate scans `go/`
  prose too, so never write a `file.go:NNN` pattern in a comment, README, or
  AGENTS.md — not even to describe a gate failure. Debt may only decrease. Do
  not reintroduce a line-numbered citation.
- **`verify-dirgate.sh`** — the `internal/reducer` row in
  `scripts/lib/dirgate-grandfather.tsv` is a monotonic ratchet. If you move
  files, re-derive it with `verify-dirgate.sh --digest internal/reducer` and
  regenerate the mirror with `generate-dirgate-grandfather-go.sh`. Never
  hand-edit either.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose stem equals a sibling package name or starts with
  `ec2instance_` -- name a compatibility shim for its subject (the
  family's snake_case name plus `_compat.go`), never for the package
  directory.
- Do not suppress `dirgate` with `//nolint`.
- Do not export a root test helper to use here. Go test files cannot share
  unexported symbols across a package boundary; this package keeps its own
  local `stubFactLoader`, `readyLookup`, and
  `recordingGraphProjectionPhasePublisher` copies rather than reaching into
  the reducer root's test files.
