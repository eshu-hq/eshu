# AGENTS.md — internal/reducer/secretsiam

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`,
`reducer/factdecode`, `reducer/factload`, `reducer/factwrite`,
`reducer/gpphase`, `reducer/payloadcore`, `reducer/schemadecode`,
`internal/facts`, `internal/graph/edgetype`, `internal/telemetry`,
`internal/truth` and the factschema SDK. It must **never** import the parent
`internal/reducer` package, directly or transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper or an identity derivation (a payload accessor, a slice
  dedupe, a node uid) goes to `reducer/payloadcore`, with a one-line forwarder
  left in root so existing root callers compile unchanged;
- vocabulary (a domain name, a result status, an ownership shape) goes to
  `reducer/contract`, with a root alias;
- a graph-readiness shape a family must read goes to `reducer/gpphase`, with a
  root alias — that is how `EndpointPresenceLookup` got here in #6061, while
  its write half stayed at the root;
- a symbol the root genuinely owns as logic stays in root, and this package does
  not use it.

Most apparent blockers here are the first kind wearing a domain filename. Read
the declaration before deciding: a body of `return payloadcore.PayloadString(...)`
is a forwarder and costs nothing to bypass, while a real implementation needs a
deliberate hoist.

## Strings here are wire and storage contracts

Three kinds of literal in this package are read by something outside Go, and a
type-identity test cannot catch a change to any of them.

- **`SecretsIAMEndpointNotReadyFailureClass`.** `internal/storage/postgres`'
  reducer-queue readiness gate matches `secrets_iam_endpoint_not_ready` to
  decide re-enqueue versus dead-letter. Change the constant and that query
  stops recognizing a deferral.
- **The four derived fact kinds** the trust-chain writer publishes. They are
  durable wire values in stored facts, and `internal/query`'s posture stores and
  the Postgres evidence loader select on them. Changing one orphans every fact
  already written under the old name.
- **The node labels and `SECRETS_IAM_*` edge types** the projection writes.
  They are the graph contract the query surface and the Ifá edge-coverage specs
  join on.

If you change any of them, pin the literal in a test and update the consumer in
the same change.

## Do not collapse the state enum

`SecretsIAMTrustChainState` has six values on purpose. `partial`, `stale`,
`permission_hidden` and `unsupported` each say something different about *why*
a chain is not exact, and an operator acts differently on each. Folding any of
them into `unresolved` — or treating "not exact" as a boolean — throws away the
distinction between "there is no such path" and "we could not see far enough to
tell." Only `exact` rows may be projected into the graph.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`, `README.md`
  and `AGENTS.md`. Deleting one fails the build. The gate checks only that they
  exist; keeping their contents true is on you.
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
- **`verify-dirgate.sh`** — this directory counts against the per-directory cap,
  and the `internal/reducer` row in `scripts/lib/dirgate-grandfather.tsv` is a
  monotonic ratchet. If you move files, re-derive the row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the mirror with
  `generate-dirgate-grandfather-go.sh`. Never hand-edit either, and never
  grandfather a count upward.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose name matches a sibling package, so a compatibility shim must be
  named for its subject — `secrets_iam_compat.go`, not `secretsiam_compat.go`.
- Do not suppress `dirgate` with `//nolint`.
- Do not move the registration wiring test in here. It exercises the root's
  `appendAdditiveDomainDefinitions` and `NewDefaultRegistry`, so it lives at the
  root as `secrets_iam_graph_projection_wiring_test.go`.
- Do not treat a nil `PresenceLookup` as "endpoints are ready". It means the
  cross-scope gate is off, which is an opt-out a deployment makes deliberately.
- Do not admit a blank or whitespace-only join key. A chain keyed on the empty
  string joins everything to everything, so the `add*ByKey` helpers trim the key
  and skip the fact. Do not quarantine it: a present-but-whitespace value is a
  valid decode, not `input_invalid`, and `secrets_iam_blank_key_test.go` asserts
  nothing is quarantined for it.
