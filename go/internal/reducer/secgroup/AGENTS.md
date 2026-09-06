# AGENTS.md — internal/reducer/secgroup

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`, `reducer/cloudjoin`,
`reducer/factdecode`, `reducer/factload`, `reducer/gpphase`,
`reducer/payloadcore`, `reducer/schemadecode`, `internal/facts`,
`internal/graph/edgetype`, `internal/telemetry`, `internal/truth`,
`pkg/log`, and the factschema SDK. It must **never** import the parent
`internal/reducer` package, directly or transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a stringer, a payload accessor, a source-uid extractor)
  goes to `reducer/payloadcore`, with a one-line forwarder left in root;
- vocabulary (a domain name, an intent, an outcome value, a ledger) goes to
  `reducer/contract`, with a root alias;
- the CloudResource join index or node uid goes to `reducer/cloudjoin`;
- the `aws_security_group_rule` decode goes to `reducer/schemadecode`;
- readiness phases and keyspaces go to `reducer/gpphase`;
- a symbol the root genuinely owns as logic stays in root, and this package
  does not use it.

Read the declaration before deciding. A body of
`return gpphase.PublishIntentGraphPhase(...)` is a forwarder and costs nothing
to bypass; a real implementation with consumers on both sides needs a
deliberate hoist to a shared leaf, which is how `cloudjoin`, `factdecode`, and
`gpphase` came to exist.

## What must stay conservative

- A rule whose SG anchor or source endpoint is not a materialized node in this
  scope MUST produce no rule node and no edges (graceful degradation,
  counted in the skip tally) -- never fabricate one.
- The reachability edge domain MUST gate on all three canonical-nodes phases
  (rule, endpoint, cloud-resource) before writing an edge. Do not weaken the
  triple gate to a subset.
- The rule node uid folds `sg_uid`, `direction`, `ip_protocol`, `from_port`,
  `to_port`, `source_kind`, and `source_value` (Option D). Do not move port or
  protocol into a relationship property -- that MERGE shape times out on
  NornicDB, which is why this design exists.
- The projected-source ledger record MUST happen before the edge write, and
  the recorded set MUST be the union of the SG->rule source uids (`sg_uid`)
  and the rule->endpoint source uids (`rule_uid`), or a later retract cannot
  anchor on the full prior set and leaks stale edges.
- Every skip reason (`unresolved_anchor`, `unresolved_endpoint`,
  `unknown_source`) is a bounded metric dimension recorded even at zero -- a
  primitive is never dropped silently.

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
  file whose stem equals a sibling package name or starts with `secgroup_` --
  name a compatibility shim for its subject (the family's snake_case name plus
  `_compat.go`), never for the package directory. This trap has bitten this
  epic four times.
- Do not suppress `dirgate` with `//nolint`.
- Do not export a root test helper to use here. Go test files cannot share
  unexported symbols across a package boundary; copy the helper into
  `security_group_test_helpers_test.go` instead, the way the moved tests
  already do.
- Do not confuse the CIDR/prefix-list endpoint materialization (this package's
  `CidrMaterializationDomainDefinition`) with the rule-node or reachability
  domains -- all three are additive and independently gated, but they share
  the `ExtractSecurityGroupReachability` extractor for the rule/anchor
  resolution logic.
