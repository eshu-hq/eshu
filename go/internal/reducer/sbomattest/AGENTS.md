# AGENTS.md — internal/reducer/sbomattest

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract`,
`reducer/factdecode`, `reducer/factload`, `reducer/payloadcore` and
`internal/telemetry`. It must **never** import the parent `internal/reducer`
package, directly or transitively.

If you find yourself needing a symbol that the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a slice diff, a payload accessor, a nil-guard) goes to
  `reducer/payloadcore`, with a one-line forwarder left in root so existing root
  callers compile unchanged;
- vocabulary (a fact-kind name, an enum, an outcome value) goes to
  `reducer/contract`, with a root alias;
- a symbol the root genuinely owns as logic stays in root, and this package does
  not use it.

Most apparent blockers here are the first kind wearing a domain filename. Read
the declaration before deciding: a body of `return payloadcore.DerefString(v)`
is a forwarder and costs nothing to bypass, while a real implementation needs a
deliberate hoist.

## Adding a fact kind

`SBOMAttestationAttachmentFactKind` is declared in `reducer/contract` and only
aliased here. That is not incidental. The reducer root's `supply_chain_impact`
family joins against the same fact kind, so the name has consumers on both sides
of the boundary and neither may import the other.

If you add another fact kind with consumers in root, put it in
`reducer/contract` the same way and alias it in both places. Do not declare it
here and export it upward.

The fact-kind string is a durable wire value written into stored facts. Changing
it orphans every fact already persisted under the old name, and a type-identity
test cannot catch that. If you change one, pin the literal in a test.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`, `README.md`
  and `AGENTS.md`. Deleting one fails the build.
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
  named for its subject — `sbom_attestation_attachment_compat.go`, not
  `sbomattest_compat.go`.
- Do not suppress `dirgate` with `//nolint`.
- Do not treat `SBOMAttachmentStatus` as a boolean. Collapsing "unverified" into
  either neighbour loses the distinction between a document that does not apply
  and one that applies but could not be verified.
