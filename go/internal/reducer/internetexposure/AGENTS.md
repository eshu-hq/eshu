# AGENTS.md — internal/reducer/internetexposure

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`, `reducer/cloudjoin`,
`reducer/factdecode`, `reducer/factload`, `reducer/gpphase`,
`reducer/payloadcore`, `reducer/schemadecode`, `internal/facts`,
`internal/telemetry`, `internal/truth`, and `pkg/log`. It must **never**
import the parent `internal/reducer` package or any sibling family package,
directly or transitively.

Both handlers gate on the durable `cloud_resource_uid` /
`canonical_nodes_committed` phase — the EC2 slice on the EC2 instance node
phase, the S3 slice on the CloudResource node phase — but none of that is a
Go dependency: every gate resolves through the durable `gpphase` keyspace,
never through an import. If a new symbol here gains a second consumer outside
this package, hoist it to the owning leaf the way `cloudjoin`, `factdecode`,
and `gpphase` were hoisted — do not export it for lateral import, and do not
duplicate production logic across families. (Test-only envelope builders are
the exception: Go test files cannot share unexported symbols across a package
boundary, so those are duplicated verbatim with a comment, never exported.)
`addToSet` and `boolPtrValue` are the deliberate single-family helpers that
moved here with the slices: each has exactly one consumer in this package and
none elsewhere, so hoisting either would drag a staying caller along for no
multi-family reason.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a stringer, a payload accessor, a source-uid extractor)
  goes to `reducer/payloadcore`, with a one-line forwarder left in root;
- vocabulary (a domain name, an intent, an outcome value, a ledger) goes to
  `reducer/contract`, with a root alias;
- the CloudResource join index, the S3 bucket join index, or a node uid goes
  to `reducer/cloudjoin`;
- the `aws_resource` / `ec2_instance_posture` / `s3_bucket_posture` /
  `aws_relationship` / `aws_security_group_rule` decodes go to
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

- Each handler MUST gate on its canonical-nodes-committed phase before
  loading facts. Do not weaken either gate to an open lookup — a property
  write against a node set that has not committed is a fabricated
  augmentation, not a retry.
- Both handlers MUST NEVER create a CloudResource node and MUST NEVER write a
  base identity/posture field the node domains own. Disjointness is what makes
  the property MERGE safe.
- The EC2 extractor MUST NEVER persist a raw public IP address (only the
  derived `state` / `internet_exposed` / `reason`), and a public IP alone
  MUST NEVER mean exposed: exposure requires an observed internet-ingress
  rule on an attached security group. Missing ENI, SG, or rule evidence stays
  `state=unknown`, never a safe false.
- The S3 extractor MUST NEVER read or persist raw bucket policy, ACL grants,
  object keys, or object data, and posture whose source bucket did not scan
  as an S3 CloudResource MUST produce no row (`source_unresolved`), never a
  fabricated node. Unknown or partial posture stays `state=unknown` with a
  nil boolean exposure property.
- A malformed `aws_resource`, `ec2_instance_posture`, `s3_bucket_posture`,
  `aws_relationship`, or `aws_security_group_rule` fact is quarantined
  per-fact (`input_invalid`) while every valid fact in the same batch still
  projects.
- Both failure-class constants MUST stay declared in the same file as the
  `FailureClass()` method that returns them: the postgres go/ast enrollment
  guard resolves per-file, and a constant declared away from its method
  silently exempts the class.

## Telemetry honesty

- The four decision/skip counters are map-driven and record observed keys
  only: `missing_identity` / `tombstone` (EC2) and `source_unresolved`
  (S3). Never record a zero count to "prove" a quiet generation — the
  completion log is the always-present record, and a zero series is a
  cardinality lie.
- The completion logs keep the pre-move key set (`fact counts`,
  `row_count`, `decisions`, `reasons`, `skipped_by_reason`, per-stage
  durations). Renaming a key orphans every dashboard and alert that reads it.
- Every metric name cited here and in `telemetry-coverage.md` must exist in
  `internal/telemetry/instruments.go`. A name that does not resolve is a
  fabrication, not a gap.

## House rules for this directory

- Keep this package at two slices (EC2 + S3 internet exposure) plus the
  `doc.go` / `README.md` / `AGENTS.md` triad and one `_test_helpers` file.
  A third exposure source is a new family proposal, not an addition.
- Name a compatibility shim for its subject (the family's snake_case name
  plus `_compat.go`), never for the package directory. A file whose stem
  equals a sibling package name or starts with `internetexposure_` is a shim
  candidate on sight.
- Do not suppress `dirgate` with `//nolint`.
- Do not export a root test helper to use here. Go test files cannot share
  unexported symbols across a package boundary; this package keeps its own
  local `stubFactLoader` and `readyLookup` copies plus the `s3logsto`-mirrored
  envelope builders rather than reaching into the reducer root's test files.
