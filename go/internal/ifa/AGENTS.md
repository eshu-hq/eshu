# AGENTS.md - internal/ifa guidance

## Read first

1. `README.md` - package purpose, boundaries, and current P0 limitations.
2. `doc.go` - godoc contract.
3. `odu.go` - Odù contract-layer canonicalization.
4. `go/internal/replay/AGENTS.md` - canonicalization invariants reused here.

## Invariants

- Ifá observes contract seams; it does not import collector or parser internals.
- Canonical comparison must reuse `go/internal/replay.Canonicalize` /
  `CanonicalizeValue`; do not add a second canonicalizer.
- Odù facts are treated as immutable inputs. Clone envelopes before rendering so
  caller-owned payload maps are not shared into comparison work.
- Keep this package deterministic: no wall-clock time, randomness, network, or
  storage side effects inside canonicalization.

## Verification

```bash
cd go && go test ./internal/ifa -count=1
```
