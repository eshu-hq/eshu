# Naming

Names are a correctness concern: an unreadable name hides the wrong
abstraction, and a duplicated name hides the real one. This refactor work
exists to replace unreadable names with ones a new reader can sound out —
and to keep every part cleanly separable so it can one day move into its own
repo. These rules apply to EVERYTHING — packages, directories, files, types,
functions, branches, scripts, and docs — not just the package being
refactored today.

1. **Plain English, human-readable.** A new reader must be able to sound out
   what a thing is without decoding abbreviations or glued compounds. Prefer
   `billing/correlation` over `billingcorrelation`; prefer `writer.go` over
   `pkg_corr_wr.go`. Abbreviate only when the short form is standard and
   obvious in the repo.
2. **Never repeat the directory name in the file name.** The path already says
   where the file lives, so `reducer/correlation/writer.go` is right
   and `reducer/correlation/correlation_writer.go` is wrong.
   The stutter adds noise to every listing, import, and citation with zero
   information.
3. **Split compound names into nested directories, do not glue full names
   together.** Combining two full names into one (`billingcorrelation`,
   `workloadmaterialization`) is ugly and unreadable. Nest instead:
   `billing/correlation/provenance/edges.go` reads as a sentence; the glued
   form does not. Each level clarifies responsibility. Repository and runtime
   boundaries come from the
   [ecosystem repository plan](design/eshu-ecosystem-repository-migration.md),
   not path depth.
4. **Follow the language's own conventions.** Go names follow Effective Go
   (see `golang-engineering`): no package-name stutter in exported
   identifiers (`correlation.Writer`, not
   `billingcorrelation.BillingCorrelationWriter`), short consistent receivers,
   names that read well at the call site. The same no-stutter principle
   applies to file names per rule 2.
   Rules 2 and 3 are enforced for new paths by
   `scripts/verify-filename-stutter.sh` (pre-commit `filename-stutter` hook
   and the Agent hygiene gate in CI). It checks only Added and Renamed paths,
   so legacy names never block unrelated work.

   - **Files, any type.** A file fails when its stem repeats its directory's
     words as a contiguous run, treating `-` and `_` alike and ignoring case
     and extension case: `run-locally/run-locally-compose.md` and
     `evidence/6821-evidence.md` fail. Tool-conventional names that repeat
     the directory are flagged too, so use the plain name: new Compose files
     belong at `compose/compose.yaml`, not `compose/docker-compose.yml`.
   - **Directories.** A newly introduced directory fails when its name
     repeats its parent's, compared case-insensitively and tiered by the
     parent's length. A parent of one or two characters must appear as a
     whole word (`db/db-auth` fails; `db/nornicdb` and `parser/c/cpp` pass).
     A three-character parent matches as a whole word or as a prefix, never
     as a suffix (`api/apiauth`, `mcp/mcpserver` and `git/github` fail;
     `sql/postgresql` and `net/dotnet` pass). A parent of four or more
     characters matches as a prefix or suffix (`query/queryauth` fails,
     `query/auth` passes). Prefix glue is the common shape and the busiest
     short domain dirs (`mcp`, `api`, `cli`, `aws`) are where it happens,
     while short-parent suffixes are usually unrelated real words.
   - **Exempt.** `README.md`, `AGENTS.md`, `CLAUDE.md`, `doc.go`, a file
     named for its own directory, dot-directories such as `.codex`, and the
     structural parents `go`, `internal`, `cmd`, `docs`, `scripts`, `specs`,
     `testdata`, and `tests`. Every directory at or below a `testdata` or
     `fixtures` root is exempt, and so is every non-Go file there, because
     fixture corpora mirror third-party conventions (`*_test.rb`,
     `test_*.py`). Any component named `fixtures` counts, including
     first-party e2e helper directories. Directories above such a root are
     still checked, and Go files under it keep the file rule.
5. **Refactors must leave names better than they found them.** When moving
   code, apply all four rules to every touched path — do not carry a glued or
   stuttering name into its new home. A move that preserves unreadable names
   is not done. If the right name is unclear, ask the owner; never guess and
   never assume the old name was right.
