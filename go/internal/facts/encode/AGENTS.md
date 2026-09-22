# AGENTS.md — internal/facts/encode

Scoped instructions for this package. Read `README.md` and `doc.go` first;
the root `AGENTS.md` and `CLAUDE.md` still apply.

## The one rule that matters

This package MUST NOT import `go/internal/facts` or any nested fact family
(`cloud`, `code`, `docs`, `supply/chain`, ...), directly or
transitively. It exists so those packages can share `StableID` and the
payload helpers without a cycle:

    facts root  -->  nested families  -->  encode

Importing anything above `encode` in that chain re-creates the cycle
#6776 introduced this package to break.

## Invariants

- **`StableID` output is a durable identifier.** Changing its
  normalization (time formatting, map/slice traversal, hash algorithm)
  changes which facts collide as "the same fact" across every ingestion
  run that has ever used it, through `facts.StableID`'s forwarder. Treat
  any change here the same as the facts root `AGENTS.md`'s "What NOT to
  change without an ADR" rule for `StableID` normalization — it is the
  same rule, just relocated.
- **`facts.StableID` must keep forwarding here, not reimplement.** If you
  ever touch `go/internal/facts/stableid.go`, keep it a one-line call into
  `encode.StableID`; a second implementation would let the two drift and
  silently change stable-key computation depending on which call path a
  caller uses.
- **Pointer helpers encode "unset," not "false/zero/empty."** Do not add a
  helper that returns a pointer to an explicit zero/false/empty value
  unless the caller genuinely needs a three-state (unset/false/true)
  field — that is a different contract from `BoolPtr`'s and needs its own
  name.

## Common changes

- **Add a new payload helper** — add it to `payload.go` (or a new file,
  not named with a form of `encode` — see naming rule 2, no
  directory-name stutter) with a doc comment stating exactly what absence
  means for that type, mirroring `IntPtr`/`StringPtr`/`BoolPtr`'s
  zero-is-absent contract or explicitly saying why a new helper differs.
- **Wire a new nested family's import of this package** — `docs` and the
  facts root's own `semantic_encode.go` already do this (see `README.md`'s
  "Depended on by"): add the
  `github.com/eshu-hq/eshu/go/internal/facts/encode` import and qualify
  each call site (`encode.StringPtr`, not an unexported local `stringPtr`).
  Follow the same pattern for a family that has not wired this package yet.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md`, and `AGENTS.md` present.
- **`verify-dirgate.sh`** — this directory counts against the repo's
  per-directory file cap; check before adding files.
