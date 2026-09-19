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
   so legacy names never block unrelated work: a file of any type whose name
   repeats its directory fails, and so does a newly introduced directory
   whose name starts or ends with its parent's (`query/queryauth` fails,
   `query/auth` passes). Both compare case-insensitively; `README.md`,
   `AGENTS.md`, `CLAUDE.md`, `doc.go`, a file named for its own directory,
   and `testdata` fixture trees are exempt, as are the structural parents
   `go`, `internal`, `cmd`, `docs`, `scripts`, `specs`, `testdata`, and
   `tests`.
5. **Refactors must leave names better than they found them.** When moving
   code, apply all four rules to every touched path — do not carry a glued or
   stuttering name into its new home. A move that preserves unreadable names
   is not done. If the right name is unclear, ask the owner; never guess and
   never assume the old name was right.
