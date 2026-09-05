# AGENTS.md — internal/reducer/iamescalation

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`, `reducer/cloudjoin`,
`reducer/factdecode`, `reducer/factload`, `reducer/gpphase`,
`reducer/iampolicy`, `reducer/payloadcore`, `reducer/schemadecode`,
`internal/facts`, `internal/telemetry`, `internal/truth` and `pkg/log`. It must
**never** import the parent `internal/reducer` package, directly or
transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a deref, a payload accessor, a tally formatter) goes to
  `reducer/payloadcore`, with a one-line forwarder left in root;
- vocabulary (a domain name, an intent, an outcome value) goes to
  `reducer/contract`, with a root alias;
- IAM statement semantics (a grant shape, an action matcher, a target status)
  go to `reducer/iampolicy`;
- CloudResource node identity or the join index goes to `reducer/cloudjoin`;
- a symbol the root genuinely owns as logic stays in root, and this package
  does not use it.

Read the declaration before deciding. A body of
`return payloadcore.DerefString(v)` is a forwarder and costs nothing to
bypass; a real implementation with consumers on both sides needs a
deliberate hoist to a shared leaf, which is how `cloudjoin` and `iampolicy`
came to exist.

## What must stay conservative

- `iamEscalationCatalog` in `iam_escalation_catalog.go` is a **security
  boundary**. Adding an entry widens what the graph asserts about real
  permissions in a customer account. It carries its own security review.
- Partial wildcards grant nothing. That is the invariant, not a gap to fill.
- Deny beats Allow, always.
- Every refusal must increment `eshu_dp_iam_escalation_skipped_total` under a
  named `skip_reason` — a primitive is never dropped silently, and the reason
  set is a bounded metric dimension, so adding one means documenting the
  dimension too.
- A rising `skipped_ambiguous` is not fixed by loosening resolution. It means
  the scope did not scan the target, or the pattern named many nodes.

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
  `iamescalation_` — a compatibility shim must be named for its subject,
  `iam_escalation_compat.go`, never `iamescalation_compat.go`. This trap has
  bitten this epic three times.
- Do not suppress `dirgate` with `//nolint`.
- Do not export a root test helper to use here. Go test files cannot share
  unexported symbols across a package boundary; copy the helper into
  `iam_escalation_test_helpers_test.go` instead, the way the moved tests
  already do.
- Do not confuse `iampolicy.EdgeKey` (dedupes by principal->target) with
  `iamcan`'s function-local `assumeEdgeKey` (dedupes by principal->role).
  Same shape, different identity.
