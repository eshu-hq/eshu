# AGENTS.md - internal/coordinator/egress guidance

## Read first

1. `go/internal/coordinator/egress/README.md` for this package's ownership
   boundary and invariants.
2. `go/internal/coordinator/egress/collector_policy.go` for collector egress
   parsing and `Decide`.
3. `go/internal/coordinator/egress/extension_policy.go` for extension egress
   parsing and `Decide`.
4. `go/internal/coordinator/collector_egress_filter.go` and
   `go/internal/coordinator/component_extension_service.go` for the root
   methods that apply these decisions.
5. `go/internal/coordinator/governance_audit.go` for how a deny decision
   becomes a governance audit event.

## Invariants

- Do not import `internal/coordinator`; the parent imports this package to
  hold `CollectorEgressPolicy` and `ExtensionEgressPolicy` on `Config`.
- Keep filtering methods (`collector_egress_filter.go`,
  `component_extension_service.go`) in the parent package; they are methods
  on `coordinator.Service` and do not belong here.
- Keep `broad` mode rejecting any collector- or extension-specific rules at
  parse time rather than ignoring them.
- Keep the collector and extension default-deny/default-allow behaviors as
  they are; do not unify them without updating both call sites and their
  tests.

## Common changes

- A new rule field (for example a new match dimension on `ExtensionRule`)
  needs a failing parse or `Decide` test first, then the JSON config struct,
  the parse validation, and the `matches` check together.
- A new reason constant must be wired through both the parser and `Decide` so
  every code path that can produce a decision names an existing reason.

## Failure modes

- An unsupported `mode` value fails parsing before any rule is read.
- A `broad` mode with rule entries present fails parsing rather than
  discarding the rules.
- An unconfigured `ExtensionPolicy` denies unconditionally; an unconfigured
  `CollectorPolicy` allows unconditionally. Confirm which policy you are
  testing before asserting a default.

## Verification

Run this package's focused tests, then the recursive coordinator tree
(`internal/coordinator/...`) since the parent's filter and extension-service
tests exercise these decisions end to end.
