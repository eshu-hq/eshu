# AGENTS.md — internal/reducer/ec2blockkms

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`, `reducer/cloudjoin`,
`reducer/factdecode`, `reducer/factload`, `reducer/gpphase`,
`reducer/payloadcore`, `reducer/schemadecode`, `internal/facts`,
`internal/telemetry`, `internal/truth`, `pkg/log`, and the factschema SDK
(`sdk/go/factschema/aws/v1`). It must **never** import the parent
`internal/reducer` package, directly or transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a stringer, a payload accessor, a source-uid extractor)
  goes to `reducer/payloadcore`, with a one-line forwarder left in root;
- vocabulary (a domain name, an intent, an outcome value, a ledger) goes to
  `reducer/contract`, with a root alias;
- the CloudResource join index or node uid goes to `reducer/cloudjoin`;
- the `aws_resource`/`aws_relationship`/`ec2_instance_posture` decodes go to
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

- The handler MUST gate on BOTH the EC2 instance-node canonical-nodes phase
  and the EBS/KMS CloudResource canonical-nodes phase before loading facts.
  Do not weaken the dual gate to a subset -- a property write against a node
  set that has not committed is a fabricated posture, not a retry.
- The handler MUST NEVER create a CloudResource node. It matches by uid only;
  a posture whose EC2 instance node does not exist in this scope generation
  is a no-op, not a fabricated node.
- Ambiguous or missing evidence (an unresolved volume, a detached or
  attachment-mismatched volume, an AWS-managed or default KMS key, two
  conflicting volume facts for the same id, two conflicting KMS relationships
  for the same volume) MUST resolve to `state=unknown` with a specific
  reason -- never a guessed `encrypted`/`not_encrypted` value.
- Every decision outcome/reason and skip reason is a bounded metric dimension
  recorded even at zero -- a primitive is never dropped silently.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md` and `AGENTS.md`. It checks only that the files exist, so it is
  not evidence the contents are true; keep them true yourself.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`. If your
  file registers no instrument, use a `No-Observability-Change:` marker
  naming the signals that already cover the stage. Never invent a metric
  absent from `go/internal/telemetry/instruments.go`.
- **`verify-performance-evidence.sh`** — fires on this path. Markers must be
  unbolded and line-initial in a tracked note (`README.md` here carries
  them).
- **`verify-dirgate.sh`** — the `internal/reducer` row in
  `scripts/lib/dirgate-grandfather.tsv` is a monotonic ratchet. If you move
  files, re-derive it with `verify-dirgate.sh --digest internal/reducer` and
  regenerate the mirror with `generate-dirgate-grandfather-go.sh`. Never
  hand-edit either.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose stem equals a sibling package name or starts with
  `ec2blockkms_` -- name a compatibility shim for its subject (the family's
  snake_case name plus `_compat.go`), never for the package directory. This
  trap has bitten this epic repeatedly.
- Do not suppress `dirgate` with `//nolint`.
- Do not export a root test helper to use here. Go test files cannot share
  unexported symbols across a package boundary; copy the helper into
  `ec2_block_device_kms_posture_test_helpers_test.go` instead, the way the
  moved tests already do (`stubFactLoader`, and
  `ec2BlockDeviceKMSDualKeyLookup` -- a local copy of the root's
  `ec2UsesProfileDualKeyLookup`, since this family's dual-key readiness gate
  needed the same shape but the root helper is scoped to a different,
  unmoved family).
