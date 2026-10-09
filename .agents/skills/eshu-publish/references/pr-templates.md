# Pull request templates

Pick the template for your kind of PR. Copy it. Replace every `REPLACE:` line.
Delete a section that has nothing to say. Then run
`scripts/check-shape.sh --pr <file>` on the text.

Rules for all four:

- The first line is `Refs #N.` or `Fixes #N.`.
- The lead is bold and has 45 words or fewer.
- The glance table has the header `| At a glance | |` and 4 rows. Each row is one
  sentence. Put counts by package, file lists, and hashes in `<details>`, not in
  the table.
- Keep every prose paragraph under 600 characters.

## Fix

Use it when the PR fixes wrong behavior.

````markdown
Refs #NNNN.

**REPLACE: what was broken, what this PR does, and why it matters.**

| At a glance | |
|---|---|
| Broken | REPLACE: the failing behavior, in one sentence. |
| Fix | REPLACE: the change, in one sentence. |
| Proof | REPLACE: the test that failed first and passes now. |
| Risk | REPLACE: what else this touches, or "none" and the reason. |

## Problem

REPLACE: how it broke. Quote the one failing line. Collapse the rest.

## What changed

- REPLACE: the verb, the object, and the why.

## Proof

| Check | Before | After |
|---|---|---|
| REPLACE: the command | REPLACE: the RED result | REPLACE: the GREEN result |

<details>
<summary>REPLACE: what is inside, such as gate output</summary>

REPLACE

</details>

**NOT_CHECKED:** REPLACE: what you did not run, and why.
````

## Refactor or mechanical move

Use it when the PR changes shape and not behavior, such as a rename, a package
move, or a batch of caller updates.

````markdown
Refs #NNNN.

**REPLACE: what moved or went away, in one sentence, and "No logic changes".**

| At a glance | |
|---|---|
| Behavior change | REPLACE: "None", and why the old and new names give identical values. |
| Size | REPLACE: the number of files edited and deleted. |
| Proof | REPLACE: the test result before and after, and whether the sets match. |
| Review | REPLACE: the verdict and the findings fixed. |

## What changed

- REPLACE: the verb, the object, and the why.

## Proof

| Check | Before | After |
|---|---|---|
| REPLACE: the test command | REPLACE: result | REPLACE: result |

<details>
<summary>REPLACE: callers by package, and the gates that ran</summary>

REPLACE

</details>

**NOT_CHECKED:** REPLACE: what you did not run, and why.
````

## Performance

Use it when the PR claims a latency, throughput, or resource change.

````markdown
Refs #NNNN.

**REPLACE: the path that got faster or cheaper, by how much, and on what data.**

| At a glance | |
|---|---|
| Path | REPLACE: the query, handler, or job that changed. |
| Before and after | REPLACE: the exact numbers with units, on the same corpus and topology. |
| Proof | REPLACE: the measurement method and where the raw numbers are. |
| Cost | REPLACE: the risk, the extra storage, or "none" and the reason. |

## Problem

REPLACE: the measured bottleneck, with its source.

## What changed

- REPLACE: the verb, the object, and the why.

## Proof

| Measure | Before | After |
|---|---|---|
| REPLACE: the metric | REPLACE: value and unit | REPLACE: value and unit |

<details>
<summary>REPLACE: raw output, plans, and the run environment</summary>

REPLACE

</details>

**NOT_CHECKED:** REPLACE: what you did not measure, and why.
````

## Docs

Use it when the PR changes only documentation or skill text.

````markdown
Refs #NNNN.

**REPLACE: what a reader can now do or understand that they could not before.**

| At a glance | |
|---|---|
| Readers get | REPLACE: the new or corrected content, in one sentence. |
| Pages | REPLACE: the pages changed, or the count. |
| Proof | REPLACE: the docs build result and the claims you checked. |
| Review | REPLACE: the verdict and the findings fixed. |

## What changed

- REPLACE: the verb, the object, and the why.

**NOT_CHECKED:** REPLACE: what you did not run, and why.
````
