# AGENTS.md — internal/reducer/cloudjoin

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## This is a leaf, and it must stay one

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a shared-core tier. It may import `reducer/factdecode`,
`reducer/payloadcore`, `reducer/schemadecode` and `internal/facts`. It must
**never** import the parent `internal/reducer` package or any family package,
directly or transitively. Reaching upward would recreate the exact import cycle
this package exists to break: the reducer root's AWS relationship,
security-group and IAM-escalation slices and the `reducer/iamcan` family all
need this index, and the root imports the families.

## What must not change casually

- **`CloudResourceUID` is a durable identity.** It is recomputed independently
  by the edge projections from a relationship fact's resolved target, and it
  matches the `aws_resource` fact's stable-key inputs. Changing which fields it
  folds, or their order, orphans every node already written under the old uid
  and silently unresolves every edge that pointed at it. If you touch it, pin
  the produced uid literal in a test.
- **The trust boundary is the point.** Every index entry derives from an
  `aws_resource` fact that carried its own `account_id` and `region`, so a
  cross-account ARN resolves only if that account was scanned in the same scope.
  Do not add a lookup path that constructs a uid from a relationship fact alone.
- **Per-fact quarantine isolation is load-bearing.** A malformed resource fact
  is skipped and returned in the quarantined slice while every valid resource is
  still indexed. Turning that into a fatal return would empty the index for the
  whole scope and stall every edge domain gating on the
  canonical-nodes-committed readiness phase.

## Adding a lookup

The four maps are exported, so callers can augment a built index — two slices
already add instance-profile and IAM role/user ARNs to `ByARN`. First writer
wins there. If you add a code path that writes into an existing key, you are
changing another slice's resolution; add, never overwrite, and say so in a test.

If you add a method, it belongs here only if it is domain-neutral. A lookup that
returns a domain's metric vocabulary (the way the root's `join_mode` resolvers
do) stays at its caller as a free function taking the index.

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
  so the root's compatibility shim is `cloud_resource_join_index_compat.go`, not
  `cloudjoin_compat.go`.

## Do not

- Do not attach a domain-specific method to `CloudResourceJoinIndex`.
- Do not suppress `dirgate` with `//nolint`.
- Do not make `ARNForUID` return only a string. A resource indexed without an
  ARN is a real case, and collapsing it into `""` loses the distinction.
