# Naming

Names are a correctness concern: an unreadable name hides the wrong
abstraction, and a duplicated name hides the real one. This refactor work
exists to remove AI slop and restore human readability — and to keep every
part cleanly separable so it can one day move into its own repo. These rules
apply to EVERYTHING — packages, directories, files, types, functions,
branches, scripts, and docs — not just the package being refactored today.

1. **Plain English, human-readable.** A new reader must be able to sound out
   what a thing is without decoding abbreviations or glued compounds. Prefer
   `package/correlation` over `packagecorrelation`; prefer `writer.go` over
   `pkg_corr_wr.go`. Abbreviate only when the short form is standard and
   obvious in the repo.
2. **Never repeat the directory name in the file name.** The path already says
   where the file lives, so `reducer/correlation/writer.go` is right
   and `reducer/correlation/correlation_writer.go` is wrong.
   The stutter adds noise to every listing, import, and citation with zero
   information.
3. **Split compound names into nested directories, do not glue full names
   together.** Combining two full names into one (`packagecorrelation`,
   `workloadmaterialization`) is AI slop: ugly, unreadable, and fused in a
   way that can never split into its own repo later. Nest instead:
   `package/correlation/provenance/edges.go` reads as a sentence; the glued
   form does not. Each level holds what fits and each level is a future
   repo boundary; split again before a directory grows past what one screen
   of `ls` can show.
4. **Follow the language's own conventions.** Go names follow Effective Go
   (see `golang-engineering`): no package-name stutter in exported
   identifiers (`correlation.Writer`, not
   `packagecorrelation.PackageCorrelationWriter`), short consistent receivers,
   names that read well at the call site. The same no-stutter principle
   applies to file names per rule 2.
5. **Refactors must leave names better than they found them.** When moving
   code, apply all four rules to every touched path — do not carry a glued or
   stuttering name into its new home. A move that preserves AI slop is not
   done. If the right name is unclear, ask the owner; never guess and never
   assume the old name was right.
