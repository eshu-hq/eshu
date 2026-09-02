# AGENTS.md — internal/reducer/iampolicy

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## This is a leaf, and it must stay one

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a shared-core tier. It may import the standard library and the
factschema SDK, and nothing else. It must **never** import the parent
`internal/reducer` package or any family package. It exists because the IAM
privilege-escalation slice at the root and the `reducer/iamcan` family evaluate
the same decoded `aws_iam_permission` statements, and a family may not import
the root.

Keep it pure: plain data and functions with no I/O, no telemetry and no context.
Every refusal it classifies is counted by the caller, against that caller's own
skip counter. That is what lets one matcher serve two domains with different
skip taxonomies.

## What must stay conservative

This package decides what the graph is allowed to assert about real permissions
in a customer account. Widening it is a security change.

- **Only `*` and `service:*` are honoured.** A partial wildcard such as
  `iam:Create*` grants nothing. Expanding it over-approximates the grant, and an
  over-approximated grant becomes an edge asserting access that may not exist.
- **A Deny wins.** `Denied` is checked before `Allows` by every caller; do not
  reorder that in a caller and do not add a path where an Allow overrides a Deny.
- **`GlobMatch` is greedy across `/` on purpose, and that is only safe because
  every caller additionally requires the matched node be a scanned node of the
  expected `resource_type`.** If you add a caller, add that check too, or a
  single-segment over-match will fabricate a cross-type edge.
- **`TargetAmbiguous` and `TargetUnresolved` are distinct answers.** Many-or-all
  is not the same as none. They are counted under different skip reasons;
  collapsing them hides which half of the resolution ladder failed.

## Keep the wrapper reads in this package

`Classify` in `statement_shape.go` is the only place a wrapped
`iamv1.Permission`'s trust fields are read. That is load-bearing beyond tidiness:
`internal/payloadusage` derives wrapper-mediated field attribution per package
directory, so a read of `statement.Permission.Actions` from another package is
invisible to it and `actions` silently drops out of the used-field manifest. If
you need another field off a wrapped statement from outside this package, add it
to `StatementShape` rather than reaching through `Statement.Permission` there.

## Adding to this package

A symbol belongs here only when it has consumers on **both** sides of the family
boundary and is genuinely IAM statement semantics. A helper only one side uses
belongs on that side. A generic helper (a deref, a payload accessor) belongs in
`reducer/payloadcore`. Node identity belongs in `reducer/cloudjoin`.

Because the shapes here are exported, the reducer root can no longer attach
methods to them — that is why the escalation-specific `armStatus` became the
free function `grantArmStatus` at the root. Keep domain-specific behavior at its
domain; only the shared half comes here.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`, `README.md`
  and `AGENTS.md`. It checks only that the files exist, not that they are true.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree needs
  a row in `docs/public/observability/telemetry-coverage.md`. This package
  registers no instrument, so its rows carry a `No-Observability-Change:`
  marker. Do not invent a metric that is absent from
  `go/internal/telemetry/instruments.go`.
- **`verify-performance-evidence.sh`** — fires on this path. `README.md` carries
  the `No-Regression Evidence:` and `No-Observability-Change:` markers; keep
  them unbolded and at the start of their line or the gate stops seeing them.
- **`verify-dirgate.sh`** — a root file may not be named after this directory,
  so the root's compatibility shim is `iam_permission_grant_compat.go`, not
  `iampolicy_compat.go`.

## Do not

- Do not add I/O, a context parameter, or a telemetry instrument here.
- Do not suppress `dirgate` with `//nolint`.
- Do not treat a blank `Statement.FactID` as a dedup key. It is intentionally
  never deduped, so blank-id statements do not collide with each other.
