# AGENTS.md - collector-sbom-shadow guidance

Read these before editing this package:

1. `README.md`
2. `doc.go`
3. `go/internal/collector/sbom/document/AGENTS.md`
4. `go/internal/collector/extensionhost/README.md`

## Invariants

- Every invocation returns complete with the pinned document's facts.
  Never return unchanged with facts: the host validator rejects it.
- Echo the host claim verbatim. Any normalization breaks host identity
  validation and terminal-fails the claim.
- Never add source observation, network calls, credentials, or fact
  kinds outside the sbom parser output. The live-read half is a later
  leaf with its own review.
- Parser facts stay reported-confidence with a blank verification status.
  Promotion to verified truth belongs to the attachment reducer, never
  to this leaf.
- Stderr names counts and knob names only. Never echo request bytes,
  claim values, or document content into errors.

## Common changes

- Re-pinning the document means replacing both `testdata/` bytes and the
  embedded copy (same file: the embed reads `testdata/`), then updating
  the golden per-kind counts in `main_test.go` in the same commit.
- Emitter behavior changes need a failing test first, then the golden
  suite. Mutate the state or a count to prove sensitivity.
- Keep the host-admissibility test (`TestResultPassesHostValidation`)
  green: it runs the SDK validator the host runs before commit.

## Failure modes

- Undecodable stdin fails closed with no stdout bytes.
- Oversize stdin fails closed at the 4 MiB cap.
- A non-decimal claim fencing token fails closed naming the knob.
