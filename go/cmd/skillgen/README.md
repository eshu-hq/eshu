# cmd/skillgen

`skillgen` is the Eshu skillgen CLI. It reads the `skill-fragments/`
source of truth at the repo root, renders one skill file per host
(Claude Code, Cursor, Codex), and either writes the result to
`expected/` (`gen`) or byte-compares the result against the committed
`expected/` baseline (`check`).

## Usage

Run from the `go` module directory:

```bash
# Render the baseline and write to expected/.
go run ./cmd/skillgen gen

# Verify the baseline is in lockstep with the fragments.
go run ./cmd/skillgen check
```

The `check` subcommand is the build-time gate. It exits non-zero when
any host file in `expected/` does not match a fresh render. The S3
CI gate (out of scope for S2) wires this into CI.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-fragments` | `../skill-fragments` | path to the skill-fragments/ directory |
| `-expected` | `../expected` | path to the expected/ baseline directory |
| `-caps` | `<fragments>/capabilities.local.yaml` | path to a capabilities.local.yaml override file |

A missing `-caps` file is the default (all collectors enabled). A
present file with a non-empty `collectors` map disables a subset;
collectors not in the file are enabled by default.

## Invariants

- **Thin driver.** `main.go` only calls `run`. `run` collects
  fragments, loads capabilities, calls `skillgen.RenderAll`, and
  dispatches on the subcommand. All render and drift logic lives in
  `internal/extensions/skillgen`.
- **`gen` is deterministic.** The same fragments plus the same
  capabilities always produce the same bytes. A regenerated baseline
  only changes when a fragment or capability changed.
- **`check` is byte-equal.** The drift check is a byte-for-byte
  comparison against `expected/<host>/<output_path>`. It does not
  normalize line endings or strip whitespace; the byte contract is
  the contract.

## Related

- `docs/internal/skill-fragments-design.md` — the S1 design contract.
- `go/internal/extensions/skillgen/` — the package the command drives.
- `skill-fragments/` — the source-of-truth fragments.
- `expected/` — the roundtrip baseline.
