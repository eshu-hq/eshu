# Measurement Ledger

`docs/internal/measurements.jsonl` is the single source of truth for numeric
claims: benchmark trial counts, deadlock rates, wall-time before/afters, and
row counts. Prose in evidence docs, PR bodies, and design docs MUST cite a
ledger row id rather than restate the numbers, because restated numbers
drift. On one branch (#5837) six of eleven review findings were exactly
that: a deadlock figure stated in four places with totals that did not sum, a
"five callers" claim that was actually four, a domain count stale in one of
six homes, and `file.go:123` citations off by four lines. Every one was a real
defect and none was catchable by any prior gate, because `mkdocs --strict`
does not validate `docs/internal/` (`docs_dir: public`) and no gate reads
prose numbers at all.

Read this when adding a measurement to an evidence doc, PR body, or design
doc, when `scripts/verify-measurement-citations.sh` fails, or when deciding
whether an existing `docs/internal/evidence/*.md` or `go/**/evidence-*.md`
file may be edited.

## Schema

One JSON object per line, append-only, stable key order:

```json
{"id": "5837-deadlock-plain-total", "date": "2026-07-29", "issue": 5837, "metric": "aws_drift_retire_deadlock_events", "variant": "plain_delete total across all four keep_set/plan_forcing cells", "value": 0, "unit": "deadlocks", "trials": 210, "host": "local-dev", "backend": "postgresql", "backend_version": "16.14", "commit": "ab9dc1bc705b", "command": "sum of ...", "note": "Cite this row for '0/210' rather than restating the figure in prose."}
```

- `id`: stable, globally unique, `<issue>-<slug>`. Never reused or renumbered.
- `date`: the date the measurement actually ran, not the date the row was added.
- `issue`: the GitHub issue the measurement supports.
- `metric`: a short machine-stable metric name, not a prose description.
- `variant`: free text identifying the experiment cell — statement, shape,
  keep-set size, plan-forcing knob, or whatever else distinguishes this row
  from a sibling row for the same metric. This is what lets a multi-cell
  experiment (plain DELETE vs. a stamped CTE, across several keep-set and
  plan-forcing shapes) live as one row per cell instead of forcing a lossy
  average or a single number.
- `value`: the number this row reports, always interpreted through `unit`.
- `unit`: `seconds`, `milliseconds`, `rows`, `deadlocks`, or another explicit
  unit — never bare.
- `trials`: the denominator when `value` is an event count out of N trials
  (`value: 0, trials: 30` reads as `0/30`); `null` when the row is not a rate.
- `host`, `backend`, `backend_version`: where, and against what backend and
  version, the measurement ran.
- `commit`: the git SHA the measurement was taken against.
- `command`: the exact command run, or an honest note that the harness was ad
  hoc and never checked in.
- `note`: free text — what the row means, how it relates to sibling rows, or
  why a figure changed from an earlier draft.

JSONL over CSV is deliberate: cells vary in shape (a deadlock trial carries
`trials`; a wall-time measurement does not), and CSV's fixed columns and
quoting rules fight that. Every row still uses the same key order and stays
on one line, so the file remains greppable (`rg '"issue":5837'`
`docs/internal/measurements.jsonl`) and diff-friendly — one line changes per
edit, never a reformatted table.

## Citation gate

`scripts/verify-measurement-citations.sh` (test mirror:
`scripts/test-verify-measurement-citations.sh`) diffs the base commit against
HEAD — `HEAD~1` locally, `origin/$GITHUB_BASE_REF` in CI, matching
`verify-performance-evidence.sh`'s convention — and requires every ADDED line
matching one of two narrow patterns to carry a `ledger:<id>` token that
resolves to a real row in the ledger:

- `<N>/<M> trials` or `<N>/<M> runs`, cited on the same line — for example `0/210 trials (ledger:5837-deadlock-plain-total)`
- a line starting (after optional indentation) with the bare word
  `Measurement`, immediately followed by a colon, then the figure and its
  citation — `Measurement: 0/210 trials (ledger:5837-deadlock-plain-total)`

The `Measurement:` trigger is prose-only: a Markdown heading
(`# Measurement:`, `## Measurement:`, ...) or a source-code comment marker
(`# Measurement:`, `// Measurement:`, `-- Measurement:`, ...) is never
recognized, at any heading level or comment style, regardless of how many `#`
characters or what comment prefix precede it. This is a deliberate exclusion,
not an oversight: every `Measurement:` marker that exists in this repo today
is mid-sentence prose, not a heading or a comment, and a comment/heading form
would need its own citation and value-verification rules this gate does not
implement.

Citing a real row is not enough on its own: the cited row's own `value` (and
`trials`, for the ratio shape) must agree with the number in the claim. The
right id with the wrong number is rejected exactly like citing an unknown id
— a citation to the wrong figure is worse than no citation, because it
carries the ledger's authority without the ledger's accuracy. This value
check only ever applies to the `<N>/<M> trials`/`<N>/<M> runs` shape, which is
fully structured; a `Measurement:` line whose figure is NOT in that shape (a
duration, a percentage, a row count) is a figure this gate cannot verify
against the row's structured fields, so it requires the prose to drop the
figure and cite only, rather than restate a number the gate never checks.
Restate a duration as a ledger row's `value`/`unit` and use the ratio shape
instead, or cite with no figure at all and let the ledger row carry the
number.

The ledger itself is checked for append-only-ness independent of any added
prose line: a commit that edits or deletes a row already present at the diff
base is rejected even if it adds no new measurement-shaped claim, because a
mutated or removed row silently changes what every existing citation to it
means.

This is deliberately narrow. It does NOT catch:

- a bare duration or count restated without "trials"/"runs" (`5.927s`, `5400 rows`)
- a percentage restating a ratio (`61/210` written as `29%`)
- a single-run claim with no denominator (`ran clean`, `no deadlocks observed`)
- a citation that sits elsewhere in the same paragraph but not the same line
- a number inside a table cell whose line doesn't say "trials" or "runs"
- a `Measurement:` marker inside a Markdown heading or a source-code comment
  (`# Measurement:`, `## Measurement:`, `// Measurement:`, ...) — excluded by
  design, see above
- a `Measurement:` figure outside the `<N>/<M> trials`/`<N>/<M> runs` shape
  that happens to numerically agree with the cited row anyway — the gate
  requires such lines to drop the figure rather than attempting to parse and
  verify an arbitrary duration/percentage/count shape
- the ratio-shape value check compares digits as text, not numbers: a row
  whose `value` or `trials` is written with a decimal (`0.0`) will not match
  a claim that (correctly, for a whole trial count) writes the same number
  without one (`0`). Trial-shape rows should use plain integers for `value`
  and `trials` for this reason
- anything under `testdata/` or in the gate's own two files (their regex
  source and test fixtures necessarily contain the trigger patterns without
  being claims about Eshu's behavior)

A gate this narrow is a starting point, not a complete solution. It exists so
that a nonzero true-positive rate stays trustworthy, rather than firing on
everything and getting disabled — a noisy gate nobody trusts is worse than no
gate. Widen the patterns only after the current ones prove out in practice.

## Frozen historical evidence

Existing `docs/internal/evidence/*.md` and `go/**/evidence-*.md` documents are
frozen AS A RECORD OF THE NUMBERS THEY STATE: do not edit one to correct or
re-derive a figure it already contains. Appending NEW evidence stays allowed —
the Performance Evidence Gate expects `Performance Evidence:` lines to keep
landing in existing `go/**/*.md` files, which is what #5869's scoping is for.

A change that would have edited one of those documents' numbers instead:

1. Adds the real figures to `docs/internal/measurements.jsonl` as one row per
   experiment cell.
2. Drops the prose that would have restated those figures — in a new doc, a
   PR body, or a design doc — and cites the ledger row (`ledger:<id>`)
   instead.
3. Leaves the old frozen document untouched.

Narrative reasoning — why a measurement was taken, what it disproves, what a
reviewer objected to — belongs in the PR body, not a new doc. The ledger holds
numbers; PRs hold arguments.
