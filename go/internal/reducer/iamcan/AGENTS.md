# AGENTS.md — internal/reducer/iamcan

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`, `reducer/cloudjoin`,
`reducer/factdecode`, `reducer/factload`, `reducer/gpphase`,
`reducer/iampolicy`, `reducer/payloadcore`, `reducer/schemadecode`,
`internal/facts`, `internal/graph/edgetype`, `internal/telemetry`,
`internal/truth` and the factschema SDK. It must **never** import the parent
`internal/reducer` package, directly or transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a deref, a payload accessor, a tally formatter) goes to
  `reducer/payloadcore`, with a one-line forwarder left in root;
- vocabulary (a domain name, an intent, an outcome value) goes to
  `reducer/contract`, with a root alias;
- IAM statement semantics (a grant shape, an action matcher, a target status)
  go to `reducer/iampolicy`;
- CloudResource node identity or the join index goes to `reducer/cloudjoin`;
- a symbol the root genuinely owns as logic stays in root, and this package does
  not use it.

Read the declaration before deciding. A body of
`return payloadcore.DerefString(v)` is a forwarder and costs nothing to bypass;
a real implementation with consumers on both sides needs a deliberate hoist to a
shared leaf, which is how `cloudjoin` and `iampolicy` came to exist.

## What must stay conservative

Both slices under-approximate on purpose. Before you widen anything:

- The `iamCanPerformCatalog` in `iam_can_perform_catalog.go` is a **security
  boundary**. Adding an entry widens what the graph asserts about real
  permissions in a customer account. It carries its own security review.
- Partial wildcards (`iam:Create*`) grant nothing. That is the invariant, not a
  gap to fill.
- A conservative refusal must increment `eshu_dp_iam_can_perform_skipped_total`
  under a named `skip_reason`. A grant is never dropped silently, and the skip
  reasons are a bounded metric dimension — adding one means adding it to the
  instrument's documented dimension list too.
- A rising `skipped_ambiguous` is not a bug to fix by loosening resolution. It
  means the scope did not scan the target, or the pattern named many nodes.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`, `README.md`
  and `AGENTS.md`. Deleting one fails the build. It checks only that the files
  exist, so it is not evidence the contents are true; keep them true yourself.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree needs
  a row in `docs/public/observability/telemetry-coverage.md`. If your file
  registers no instrument, use a `No-Observability-Change:` marker naming the
  signals that already cover the stage. Do not invent a metric that is absent
  from `go/internal/telemetry/instruments.go`.
- **`verify-performance-evidence.sh`** — fires on this path. It needs
  `No-Regression Evidence:` and `No-Observability-Change:` markers, unbolded and
  at the start of their line, on an added line in a tracked note. `README.md`
  here carries them; keep them unbolded and line-initial or the gate stops
  seeing them.
- **`verify-dirgate.sh`** — this directory counts against the 40-file cap, and
  the `internal/reducer` row in `scripts/lib/dirgate-grandfather.tsv` is a
  monotonic ratchet. If you move files, re-derive the row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the mirror with
  `generate-dirgate-grandfather-go.sh`. Never hand-edit either, and never
  grandfather a count upward.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose name matches a sibling package, so a compatibility shim must be
  named for its subject — `iam_can_compat.go`, not `iamcan_compat.go`.
- Do not suppress `dirgate` with `//nolint`.
- Do not confuse the function-local `assumeEdgeKey` in
  `iam_can_assume_edge_rows.go` with `iampolicy.EdgeKey`. Same shape, different
  identity: one dedupes by (principal, role), the other by (principal, target).
- Do not export a test helper from the root to use here. Go test files cannot
  share unexported symbols across a package boundary; copy the helper into
  `iam_can_test_helpers_test.go` instead, the way the moved tests already do.
