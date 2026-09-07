# AGENTS.md — internal/reducer/s3logsto

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

The bucket-name join index lives in `reducer/cloudjoin` rather than here for
exactly this reason: the s3grant and S3 internet-exposure slices resolve
against the same index, and a shared implementation with consumers on both
sides belongs in a leaf, not in one family. If a new symbol here gains a
second consumer outside this package, hoist it to the owning leaf the way the
index was hoisted — do not export it for lateral import, and do not duplicate
production logic across families. (Test-only envelope builders are the
exception: Go test files cannot share unexported symbols across a package
boundary, so those are duplicated verbatim with a comment, never exported.)

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a stringer, a payload accessor, a source-uid extractor)
  goes to `reducer/payloadcore`, with a one-line forwarder left in root;
- vocabulary (a domain name, an intent, an outcome value, a ledger) goes to
  `reducer/contract`, with a root alias;
- the CloudResource join index, the S3 bucket-name join index, or node uid
  goes to `reducer/cloudjoin`;
- the `aws_resource`/`s3_bucket_posture` decodes go to `reducer/schemadecode`;
- readiness phases and keyspaces go to `reducer/gpphase`;
- a symbol the root genuinely owns as logic stays in root, and this package
  does not use it.

Read the declaration before deciding. A body of
`return factload.LoadFactsForKinds(...)` is a forwarder and costs nothing to
bypass; a real implementation with consumers on both sides needs a deliberate
hoist to a shared leaf, which is how `cloudjoin`, `factdecode`, and `gpphase`
came to exist.

## What must stay conservative

- The handler MUST gate on the `cloud_resource_uid` canonical-nodes phase
  before loading facts. Do not weaken or skip the gate -- an edge write
  against a node set that has not committed is a fabricated edge, not a
  retry.
- The handler MUST NEVER create a CloudResource node. It matches by uid only;
  a posture whose source bucket or log-target bucket was not scanned in this
  scope generation is a conservative skip (`source_unresolved` /
  `target_unresolved`), not a fabricated node. A blank logging target is the
  normal no-edge state, not a skip.
- A self-target (a bucket logging to itself) is a legal, real S3
  configuration and DOES produce an edge — the deliberate divergence from the
  IAM self-assume skip rule. Do not "fix" it into a skip.
- A malformed `aws_resource` or `s3_bucket_posture` fact is quarantined
  per-fact (`input_invalid`) while every valid fact in the same batch still
  projects.

## Telemetry honesty

Both LOGS_TO counters are map-driven: `eshu_dp_s3_logs_to_edges_total`
emits only observed resolution modes and `eshu_dp_s3_logs_to_skipped_total`
emits only nonzero skip reasons. Never write that these series are "recorded
even at zero" — a quiet generation emits no series, and the completion log
is the always-present record. Check the tally code before writing the
sentence.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md` and `AGENTS.md`. It checks only that the files exist, so it is
  not evidence the contents are true; keep them true yourself.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`. This
  family's rows are the `projection (S3 LOGS_TO)` row (repointed to this
  package by the move) and the shared `reducer.s3_logs_to_materialization`
  span row, which names `contract.go` and is unaffected by moves. Do not
  invent a metric that is absent from `go/internal/telemetry/instruments.go`.
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
  `s3logsto_` -- name a compatibility shim for its subject (the family's
  snake_case name plus `_compat.go`), never for the package directory.
- Do not suppress `dirgate` with `//nolint`.
- Do not export a root test helper to use here. Go test files cannot share
  unexported symbols across a package boundary; this package keeps its own
  local `stubFactLoader` and `readyLookup` copies rather than reaching into
  the reducer root's test files.
