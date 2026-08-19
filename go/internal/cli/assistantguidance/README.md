# Assistant Guidance

## Purpose

`assistantguidance` owns the logic behind `eshu assistant install`,
`eshu assistant status`, and `eshu assistant uninstall`. Those commands keep a
short block of Eshu guidance inside the project instruction files that AI
coding assistants already read, so an assistant working in the repo knows to
reach for Eshu's bounded MCP/API tools before falling back to full-repo grep,
and knows to honor Eshu's truth labels.

The guidance sits inside a marker-delimited managed block. That is the whole
design constraint: these are files the operator owns and edits, so install,
reinstall, and uninstall have to touch the managed region and nothing else.

## Ownership boundary

This package owns the guidance *logic*: which platforms exist, what the
guidance body says, how the managed block is found, replaced, and removed, how
results are formatted, and the `--verify` ritual check.

It does not own process wiring. Reading cobra flags, resolving `--path` against
the process working directory, resolving the output stream, and mapping errors
to exit codes all stay in `go/cmd/eshu/assistant.go`, the cobra `RunE`
wrapper, because `go/cmd/eshu` is `package main` and nothing can import it. The
wrapper resolves process state and passes it in as plain values -- an absolute
root, a `[]Platform`, and an `io.Writer`; this package returns data and errors
and prints only through the writer it was given.

`assistant hook` is a different command family and a different package
(`internal/cli/hookpreflight`). It attaches its subcommand to the same
`assistantCmd` in the wrapper; nothing is shared here.

## The managed block

```text
<!-- BEGIN ESHU GUIDANCE -->
...body...
<!-- END ESHU GUIDANCE -->
```

The marker pair is the block's only identity, which has three consequences
worth knowing before changing anything:

- An **exact** pair anywhere in the file is the managed region -- including a
  pair quoted inside a fenced code block. A file that documents the markers
  verbatim will have that documented pair rewritten.
- A **near miss** is not a block. Different spacing, different case, or extra
  words inside the comment leave the text alone, and a fresh block is appended
  instead.
- A begin marker with **no following end marker** reports "no block", so a
  truncated file gains a fresh block rather than being spliced at a guessed
  boundary.

`Upsert` replaces only the bytes from the begin marker through the end marker;
everything before and after is preserved exactly. Appending a new block
separates it from prior content with exactly one blank line. `Remove` strips
the pair and collapses the blank lines that bracketed it, leaving the text
above and below byte-identical. Markers and their separating newlines are
always LF -- a CRLF file keeps its carriage returns in the surrounding content,
which is preservation, not CRLF support.

## The file-write surface

Every write goes through the `FileSystem` seam, under the absolute root the
caller supplied, at the platform-relative paths in `SupportedPlatforms`:
`CLAUDE.md`, `AGENTS.md`, and `.cursor/rules/eshu.mdc`.

| Operation | Call | When |
| --- | --- | --- |
| `Install` | `MkdirAll(dir, 0o755)` | Only when the file content will change. Covers `.cursor/rules/`. |
| `Install` | `WriteFile(path, 0o644)` | Only when the rendered content differs from what is on disk. An unchanged reinstall performs no write. |
| `Uninstall` | `WriteFile(path, 0o644)` | A block was removed and other content remains. |
| `Uninstall` | `Remove(path)` | Stripping the block left nothing but whitespace, so the file held the Eshu block and nothing else. |
| `Status` | none | Status never writes. |

Two properties operators depend on:

- **A file with operator content is never deleted.** Deletion is gated on the
  post-removal content being whitespace-only.
- **The write replaces the file in place.** It is not atomic, takes no backup,
  and `0o644` applies only at creation -- `os.WriteFile` leaves an existing
  file's mode alone.

## What reaches the written block

Nothing operator-supplied does. `GuidanceBody` is assembled from package
constants and depends only on the `Platform` value, so the bytes between the
markers cannot carry the `--path` root, the `--platform` filter, or anything
from the surrounding file. The operator's own content is preserved verbatim
*outside* the block, and the renderers print paths relative to the project root
rather than the absolute root. Returned errors are the one place an absolute
path appears, which is what an operator needs in order to fix a failed write.
`redaction_test.go` holds that line with a sentinel planted inside an ordinary
value in the seeded file, across nine different preceding characters.

## Exported surface

Block manipulation (`block.go`):

- `BeginMarker`, `EndMarker` -- the marker pair
- `BlockStatus` with `BlockAbsent` / `BlockCurrent` / `BlockStale`
- `RenderBlock`, `FindBlock`, `ExtractBody`, `Classify`, `Upsert`, `Remove`,
  `BlockSummary`

Platforms and content (`content.go`):

- `Platform`, `SupportedPlatforms`, `LookupPlatform`, `GuidanceBody`

File operations (`engine.go`):

- `FileSystem`, `OSFileSystem`
- `Engine` with `NewEngine`, `NewEngineWithFS`, `Root`, `Install`, `Status`,
  `Uninstall`
- `Result`, `SelectPlatforms`

Rendering and verification (`render.go`):

- `RenderInstall`, `RenderStatus`, `RenderUninstall`, `RelOrPath`
- `RitualVerification`, `RenderVerifyReport`

## Verification

```bash
cd go && go test ./internal/cli/assistantguidance ./cmd/eshu -count=1
```

`TestPackageStaysProcessNeutral` parses every file in this directory and fails
on a cobra import or a process-bound selector, which is the standing guard
behind the ownership boundary above.
