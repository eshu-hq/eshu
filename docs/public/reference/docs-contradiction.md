<!-- docs-catalog
title: Docs Self-Contradiction Gate
description: Defines the advisory gate that flags a docs/public page self-contradicting about the same fact.
type: reference
audience: maintainer, docs author
entrypoint: false
-->

# Docs Self-Contradiction Gate

A docs page self-contradicts when two sections disagree about the same fact —
one section says a capability is implemented, another says the same
capability is not yet implemented. Issue #5340 found two live cases:
`docs/public/languages/php.md` claimed Slim `$app->group()` prefix
concatenation was both implemented (Capability Checklist) and not yet
implemented (Known Limitations), and
`docs/public/reference/collector-reducer-readiness.md` listed two
"Kubernetes live" rows in the same table with contradictory current-state
text. This gate exists to catch that class of drift before it lands, on any
`docs/public/**/*.md` page — not just the docs-catalog page types
`verify-docs-prose-quality.sh` checks.

Run the advisory gate locally:

```bash
bash scripts/verify-docs-contradiction.sh
```

Run the mirror tests:

```bash
bash scripts/test-verify-docs-contradiction.sh
```

Turn the same checker into a blocking gate:

```bash
DOCS_CONTRADICTION_ENFORCE=true bash scripts/verify-docs-contradiction.sh
```

## Checked Pages

Every `docs/public/**/*.md` page is scanned, unconditionally. A reference or
proof page is exactly where a stale capability-status paragraph is most
likely to drift from a table row someone kept current, so this gate does not
filter by docs-catalog `type` the way the prose-quality gate does. A page
carrying a generated or do-not-edit marker is skipped — source truth and
drift gates own that content instead.

## The Two Checks

1. **Modal-polarity with a shared-subject anchor.** A page is flagged only
   when the SAME specific subject — a backticked code span, a bare
   capability-ID string (three-plus hyphen-joined components), or a
   stopword-free three-word n-gram — appears on one line matching a
   positive-polarity phrase (`is/are/now implemented`, `supported`, `is
   available`) and on a DIFFERENT line matching a negative-polarity phrase
   (`not yet implemented`, `not implemented`, `planned`, `unsupported`). Bare
   co-occurrence of a positive and a negative word in the same file is never
   enough — the anchor requirement is the deliberate false-positive guard.
   php.md's Laravel capability row is unflagged for two reasons, either of
   which is sufficient: (a) it pairs "implemented"/"supported" with
   "deferred", and "deferred" is not one of the tracked negative-trigger
   phrases, so no line on that row is ever classified negative; and (b) the
   "implemented" and "deferred" claims sit on the SAME line, which the
   same-line guard (a finding requires the positive and negative matches to
   be on different lines) spares regardless of vocabulary.
2. **Duplicate table-row key.** Within one contiguous markdown table block
   (reset at a blank line or a line that does not start with `|`), the same
   first-column cell value must not repeat. Two rows sharing a label inside
   one table are either a stale row nobody deleted or a copy-paste error.

The full anchor-matching rationale, including the noise-reduction guards
(stripping markdown link URLs, excluding numeric tokens, capping how common
an anchor may be, and excluding a bare single-word backtick status value like
`` `supported` ``), is documented in the header of
`scripts/lib/docs-contradiction-checks.awk`.

## Burn-Down Baseline

`scripts/docs-contradiction-baseline.txt` lists pre-existing findings on pages
this gate does not yet require someone to fix, mirroring
`scripts/docs-refs-baseline.txt`. A finding not in the baseline fails the gate
under enforcement; a baselined finding is silent; a baselined page that gets
fixed simply drops out of a future baseline regeneration. Regenerate with:

```bash
bash scripts/verify-docs-contradiction.sh -update
```

## Blocking Path

The tracked switch for making the gate blocking is
`DOCS_CONTRADICTION_ENFORCE=true`. Once the advisory baseline is clean enough,
flip the CI/local gate command to set that variable and change the gate from
advisory to blocking in `specs/ci-gates.v1.yaml`.

Before flipping, note that the modal-polarity check cannot tell a genuine
self-contradiction from a legitimate base-versus-sub-feature refinement that
shares an anchor: a page that says "`api-v0` reads are supported" in one place
and "`api-v0` streaming is not yet implemented" in another will hard-fail the
blocking gate even though both statements are true. (That `api-v0` example is
itself flagged on this very page and carried in the burn-down baseline —
concrete proof of the false positive.) Flipping therefore requires either
tightening the heuristic (for example, requiring the two polarity phrases to
be adjacent to the shared anchor rather than merely on the same line) or
baselining such statements, and the burn-down baseline must be walked for that
class of finding first.
