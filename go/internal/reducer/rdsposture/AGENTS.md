# AGENTS.md — internal/reducer/rdsposture

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`, `reducer/cloudjoin`,
`reducer/factdecode`, `reducer/factload`, `reducer/gpphase`,
`reducer/payloadcore`, `reducer/schemadecode`, `internal/facts`,
`internal/telemetry`, `internal/truth`, and `pkg/log`. It must **never** import
the parent `internal/reducer` package, directly or transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a stringer, a payload accessor, a source-uid extractor)
  goes to `reducer/payloadcore`, with a one-line forwarder left in root;
- vocabulary (a domain name, an intent, an outcome value, a ledger) goes to
  `reducer/contract`, with a root alias;
- the CloudResource join index or node uid goes to `reducer/cloudjoin`;
- the `aws_resource`/`rds_instance_posture` decodes go to `reducer/schemadecode`;
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
  before loading facts. Do not weaken or skip the gate -- a property write
  against a node set that has not committed is a fabricated posture, not a
  retry.
- The handler MUST NEVER create a CloudResource node. It matches by uid only;
  a posture whose RDS DB instance or Aurora cluster does not exist in this
  scope generation is a conservative skip (`source_unresolved`), not a
  fabricated node.
- A malformed `aws_resource` or `rds_instance_posture` fact is quarantined
  per-fact (`input_invalid`) while every valid fact in the same batch still
  projects.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md` and `AGENTS.md`. It checks only that the files exist, so it is
  not evidence the contents are true; keep them true yourself.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`. This
  family has no metric instrument of its own; the only row naming it is the
  shared span row (`reducer.rds_posture_materialization`), which carries the
  generic `eshu_dp_postgres_query_duration_seconds` metric every reducer
  handler span carries, not an RDS-specific series. Do not invent one.
- **`verify-doc-citations.sh`** — a raw `path.go:NNN` LINE citation's
  "context" is the entire containing markdown line, shared byte-for-byte by
  every other `.go:NNN` citation on that same line; editing ANY part of a
  multi-citation table row invalidates all of them, not just the one being
  fixed, and the gate refuses even `-update` on such a row. This package's
  move therefore converted EVERY `go/internal/**/*.go:NNN` citation on its
  row in `docs/internal/design/4786-contract-integration-matrix.md` to the
  anchor form the gate's own header prefers (no line number): the reducer
  cell now names `rdsposture.RDSPostureMaterializationHandler` plus the new
  file path, and the sibling posture-envelope collector cell dropped its
  line number to a file anchor in the same edit (#6061, #6585 precedent).
  The two matching `LINE` baseline rows were removed with the conversion;
  debt may only decrease. Do not reintroduce a line-numbered citation on
  that row.
- **`verify-dirgate.sh`** — the `internal/reducer` row in
  `scripts/lib/dirgate-grandfather.tsv` is a monotonic ratchet. If you move
  files, re-derive it with `verify-dirgate.sh --digest internal/reducer` and
  regenerate the mirror with `generate-dirgate-grandfather-go.sh`. Never
  hand-edit either.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose stem equals a sibling package name or starts with
  `rdsposture_` -- name a compatibility shim for its subject (the family's
  snake_case name plus `_compat.go`), never for the package directory.
- Do not suppress `dirgate` with `//nolint`.
- Do not export a root test helper to use here. Go test files cannot share
  unexported symbols across a package boundary; this package keeps its own
  local `stubFactLoader` and `readyLookup` copies rather than reaching into
  the reducer root's test files.
