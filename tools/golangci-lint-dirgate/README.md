# golangci-lint-dirgate

A [Go plugin](https://golangci-lint.run/docs/plugins/go-plugins) for
[golangci-lint v2.12.2](https://github.com/golangci/golangci-lint) that
enforces Eshu's per-directory sprawl limits (issue #6054, epic #6053):

1. **Size cap** -- a package directory may hold at most **40 non-test
   `.go` files**.
2. **Naming rule** -- a file whose name is a sibling subdirectory's
   package name, or that name plus an underscore prefix (`bar.go` or
   `bar_baz.go` next to a `bar/` subpackage), belongs inside that
   subpackage instead.

This restructure epic exists because several package directories grew far
past a reviewable size before any gate stopped it (`internal/query` alone
has 879 non-test files). This plugin, and its bash mirror in
`scripts/dev/precommit-go.sh` (`dirgate` / `dirgate-all`), exist so that
growth stops immediately while the epic's move-issues (#6056-#6062) shrink
the pre-existing offenders over time.

It is built and wired the same way as `../golangci-lint-filelength` (its
README documents the "why a Go plugin" and version-pinning rationale in
more depth; this file covers what is specific to dirgate).

## What it checks

For every package directory:

| Condition | Result |
| --- | --- |
| Directory is `vendor/`, `testdata/`, `generated/`, or hidden (or nested under one) | not evaluated |
| Non-test `.go` file count > 40, directory not grandfathered | **cap violation** |
| Non-test `.go` file count > 40, directory grandfathered and still within its pinned envelope | passes (see Grandfather ledger) |
| A qualifying file's name matches a sibling subpackage's name (exactly, or with an `_` boundary) | **naming violation** for that file |

`_test.go` files are excluded from both the count and the naming check --
a package's test suite legitimately outgrowing its production surface is
not sprawl, and a misnamed test file is expected to move alongside its
production counterpart when the family moves.

## Grandfather ledger

Two SEPARATE ledgers pin the two checks independently -- a directory's cap
state and a file's naming exemption never gate each other:

- `scripts/lib/dirgate-grandfather.tsv` pins the **cap** check: one row per
  directory that was already over the cap when this gate landed, with the
  exact qualifying-file count and a sha256 digest of the sorted file list
  at that time.
- `scripts/lib/dirgate-naming-exempt.tsv` pins the **naming** check: one
  row per individual file (`dir<TAB>file`) that already violated the
  naming rule when this gate landed. It is a separate file, not a column
  on the cap ledger, so that appending a new exemption is a new,
  reviewable row rather than an edit buried inside a long comma-separated
  cell, and so two PRs fixing different files in the same directory don't
  conflict on one shared line.

`grandfather.go` in this directory is **generated** by joining both TSVs
(`scripts/generate-dirgate-grandfather-go.sh`) -- edit the relevant TSV,
then re-run the generator; never hand-edit `grandfather.go`. A
naming-exempt row's directory must already have a cap-ledger row; the
generator fails before writing any output otherwise.

A grandfathered directory's CAP rule, applied by `evaluateCapViolation` in
`grandfather_eval.go`, is a **monotonic ratchet**: a directory passes only
while its live state exactly matches the pin.

- Holding exactly at the pinned count requires the digest to still match
  -- this catches a same-count *swap* (one file removed, a different one
  added) that pure counting would miss.
- Exceeding the pinned count fails outright, regardless of digest:
  **adding one file to a grandfathered directory un-grandfathers its cap
  check.** The naming check is NOT re-applied by this -- it never was
  suppressed by the cap in the first place; see below.
- **Shrinking below the pinned count also fails**, and requires re-pinning
  the row -- it is not a free pass. An earlier version of this check
  treated any live count below the pin as an unconditional, digest-free
  pass, which let a directory pinned at, say, 100 shrink to 50 and then
  silently regrow to 99 without ever failing, because the live count
  never crossed back over `entry.FileCount`. That defeated the ratchet
  the ledger exists to enforce. Now a shrink must be re-pinned in the
  same change that caused it -- run `scripts/dev/precommit-go.sh
  dirgate-digest <dir>`, update the row in `dirgate-grandfather.tsv` to
  the new count and digest, and regenerate `grandfather.go` -- so any
  later regrowth is always measured against the best (lowest) state the
  directory ever reached, not its original landing snapshot. This is
  deliberate ratchet pressure: an otherwise-unrelated PR that deletes a
  `.go` file from a grandfathered directory must re-pin that row in the
  same PR. (A directory whose live count drops to or below the 40-file
  cap entirely is unaffected by this rule -- it is no longer a cap
  offender at all, and its row should be deleted; see below.)

The NAMING rule, applied by `namingExemptSet` in `grandfather_eval.go`, is
independent of the cap check and of the directory's aggregate file count
entirely: a file is exempt if and only if its exact basename has a row in
`dirgate-naming-exempt.tsv` for that directory. A brand-new naming
violation is reported the moment it appears, even in a directory that
sits well under its cap-ledger pin; a pinned exemption stays suppressed
even while the directory grows past its cap. (An earlier version of this
gate gated the naming check on the directory's aggregate count instead --
that suppressed brand-new violations for as long as a directory stayed at
or below its pinned cap, which got WORSE as the epic's move-issues
(#6056-#6062) shrank those directories. The per-file design above is the
fix.)

Both ledgers only shrink:

- Remove a cap-ledger row once its directory's real, unpinned file count
  is at or below 40 (`scripts/dev/precommit-go.sh dirgate-digest <dir>`
  prints the current count and digest to confirm; `scripts/verify-dirgate.sh
  --all` also nudges a row that no longer needs it with a non-fatal NOTE).
  If the directory shrank but is still over 40 files, its row must instead
  be **bumped down** to the new count and digest (a hard failure, not a
  nudge -- see the ratchet rule above), not removed.
- Remove a naming-exempt row once its file has actually moved into the
  sibling subpackage (or been renamed/removed so it no longer collides).
  Unlike the cap ledger's soft NOTE, a naming-exempt row whose file no
  longer exists, or no longer collides with any sibling subpackage, makes
  `scripts/verify-dirgate.sh --all` **fail outright** -- the PR that fixes
  the violation must delete the row in the same change. `scripts/verify-dirgate.sh
  --digest <dir>` also prints that directory's current naming violations
  (if any) so a new row, when genuinely needed, can be authored honestly
  instead of guessed.

## Escape hatch

```go
package foo //nolint:dirgate // 92 files; tracked for the #6058 split
```

The marker must be on the file's `package` declaration line, immediately
followed by a second `//` comment with a non-empty justification. Unlike
`//nolint:filelength` (enforced by convention only -- see
`../golangci-lint-filelength/README.md`), a **bare** `//nolint:dirgate`
with no justification is not accepted: `nolintJustification` in
`nolint.go` parses this explicitly rather than relying on
golangci-lint's own nolint processor, which can only see marker
presence, not justification content. `scripts/lib/dirgate-core.sh` enforces
the identical rule for every local and CI path that does not go through
`golangci-lint run` (see `specs/ci-gates.v1.yaml`'s `go-dir-gate` entry).

For a cap violation, the marker goes on the directory's **representative
file**: `doc.go` if present, otherwise the alphabetically-first
qualifying file (`representativeFile` in `dirgate.go` -- the same file
the diagnostic itself is reported against). For a naming violation, the
marker goes on the specific offending file; it suppresses only that
file, not the rest of the directory.

## Build and test

```bash
make build   # produces dirgate.so
make test    # runs the unit tests
make clean   # removes the .so
```

## How CI uses it

`go/.golangci.yml` lists the plugin under `linters.settings.custom`,
mirroring the filelength entry:

```yaml
linters:
  settings:
    custom:
      dirgate:
        type: goplugin
        path: ../tools/golangci-lint-dirgate/dirgate.so
        description: "Eshu directory-size and naming gate (#6054)"
        original-url: github.com/eshu-hq/eshu/tools/golangci-lint-dirgate
```

`.github/workflows/test.yml` builds this plugin (alongside the
filelength plugin) before invoking `golangci-lint run ./...`.

Locally, `scripts/dev/precommit-go.sh lint` / `lint-all` strip this
custom plugin from the config the same way they strip filelength (see
that script's `stripped_config`, for the same cross-machine toolchain
reason); local enforcement instead runs through the `dirgate` /
`dirgate-all` bash subcommands, which mirror `evaluateDirectory`'s rules
directly rather than depending on a matched Go toolchain.

## The cap escape hatch does not apply to grandfathered directories

`//nolint:dirgate // <reason>` on a directory's representative file suppresses
the **cap for that whole directory**, not for one file. On a directory with a
row in `scripts/lib/dirgate-grandfather.tsv` that would be a hole rather than an
exemption: one marker on `internal/query/doc.go` un-gates 854 files, silently
and for good.

So both implementations refuse it there. `grandfather_eval.go` reports the cap
finding regardless of any marker when the directory has a ledger row, and
`scripts/lib/dirgate-core.sh` mirrors that, with a message naming the real exit.
Tests pin the behaviour on both sides
(`TestEvaluateDirectoryGrandfatheredCapNolintIsRefused`, and
`test_grandfathered_cap_nolint_is_refused` in `scripts/test-verify-dirgate.sh`).

A grandfathered directory over its pin has two exits:

- **Split it into a subpackage.** The right answer, and the one the epic is
  built around. Note it does not compile yet for `query`, `reducer`, `projector`
  or `mcp` — the acyclic-boundary prerequisite in
  `docs/internal/design/package-restructure.md` has to land first.
- **Bump its row, in a reviewed commit.** Legitimate only to absorb growth that
  reached main before the gate could enforce there. Never to absorb a file the
  same change is adding. The rules are written at the top of the ledger.

The naming rule has no such carve-out: it is pinned per file in
`dirgate-naming-exempt.tsv`, and a `//nolint:dirgate` on a specific file
suppresses only that file, which is a proportionate exemption rather than a
directory-wide one.
