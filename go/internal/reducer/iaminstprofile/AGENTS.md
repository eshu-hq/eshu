# AGENTS.md — internal/reducer/iaminstprofile

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`, `reducer/cloudjoin`,
`reducer/factdecode`, `reducer/factload`, `reducer/gpphase`,
`reducer/iampolicy`, `reducer/payloadcore`, `reducer/schemadecode`,
`internal/facts`, `internal/graph/edgetype`, `internal/telemetry`,
`internal/truth`, and `pkg/log`. It must **never** import the parent
`internal/reducer` package, directly or transitively. The one sibling-shaped
import, `reducer/iampolicy`, is the shared IAM-vocabulary leaf (the `iamcan`
precedent: `ResourceTypeRole` is a pure alias of the factschema SDK
constant), not family logic — do not widen it into handler, catalog, or
matcher imports from any sibling.

The dual-domain shim `ec2_profile_domains.go` stays in the reducer root for
exactly this reason: it re-exports both this family's
`DomainIAMInstanceProfileRoleMaterialization` and the `ec2usesprofile`
slice's domain, so it belongs to neither family alone. If a new symbol here
gains a second consumer outside this package, hoist it to the owning leaf
the way `cloudjoin`, `factdecode`, and `gpphase` were hoisted — do not export
it for lateral import, and do not duplicate production logic across families.
(Test-only envelope builders are the exception: Go test files cannot share
unexported symbols across a package boundary, so those are duplicated
verbatim with a comment, never exported.)

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a stringer, a payload accessor, a source-uid extractor)
  goes to `reducer/payloadcore`, with a one-line forwarder left in root;
- vocabulary (a domain name, an intent, an outcome value, a ledger) goes to
  `reducer/contract`, with a root alias;
- IAM vocabulary (a resource-type token) goes to `reducer/iampolicy`;
- the CloudResource join index or node uid goes to `reducer/cloudjoin`;
- the `aws_resource` decodes go to `reducer/schemadecode`;
- readiness phases and keyspaces go to `reducer/gpphase`;
- a symbol the root genuinely owns as logic stays in root, and this package
  does not use it.

Read the declaration before deciding. A body of
`return factload.LoadFactsForKinds(...)` is a forwarder and costs nothing to
bypass; a real implementation with consumers on both sides needs a deliberate
hoist to a shared leaf, which is how `cloudjoin`, `factdecode`, and `gpphase`
came to exist.

## What must stay conservative

- The handler MUST gate on the `cloud_resource_uid`
  `canonical-nodes-committed` phase before loading facts. Do not weaken or
  drop this gate — an edge write against an endpoint set that has not
  committed is a fabricated edge, not a retry.
- The handler MUST NEVER create a CloudResource node. It resolves each
  `role_arn` against scanned role nodes only; an unscanned role is a
  conservative skip (`target_unresolved`), not a fabricated node. A profile
  with neither ARN nor resource id is `source_unresolved`. A profile with no
  roles produces no edge and is not counted as a skip.
- A malformed `aws_resource` fact is quarantined per-fact (`input_invalid`)
  while every valid fact in the same batch still projects. A
  present-but-malformed `role_arns` entry MUST quarantine (#4631) — it must
  never silently fail to resolve and look like an ordinary
  `target_unresolved` skip.
- The written rows MUST keep the byte-identical key set `profile_uid` /
  `role_uid` / `relationship_type` / `resolution_mode`: the Cypher writer
  and the Ifá offline guard read those keys, and renaming one orphans both.
- The evidence source (`reducer/iam-instance-profile-role`) MUST stay one
  constant with an exported alias, never two literals: the retract path
  matches on scope id AND this source, and the offline guard stamps it onto
  its derived edges (#6309). Two literals can drift; an alias cannot.

## Telemetry honesty

Both tally maps are map-driven: `resolved` emits only observed resolution
modes and `skipped` emits only nonzero skip reasons. Never write that any
series here is "recorded even at zero" — a quiet generation emits no
per-mode series, and the completion log is the always-present record. Check
the tally code before writing the sentence.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md` and `AGENTS.md`. It checks only that the files exist, so it is
  not evidence the contents are true; keep them true yourself.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`. This
  family's metric instruments are registered in `internal/telemetry`, and the
  moved materialization file keeps its projection row (repointed to the new
  path, rows-file coverage merged into the one row per the 1142 line-cap
  pin); the shared `reducer.iam_instance_profile_role_materialization` span
  names `contract.go` and needs no repoint. Do not invent a metric that is
  absent from `go/internal/telemetry/instruments.go`.
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
  `iaminstprofile_` -- name a compatibility shim for its subject (the
  family's snake_case name plus `_compat.go`), never for the package
  directory.
- Do not suppress `dirgate` with `//nolint`.
- Do not export a root test helper to use here. Go test files cannot share
  unexported symbols across a package boundary; this package keeps its own
  local `stubFactLoader` copy rather than reaching into the reducer root's
  test files.
