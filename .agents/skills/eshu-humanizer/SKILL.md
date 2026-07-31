---
name: eshu-humanizer
description: Use when writing anything a human will read — PR titles and bodies, review comments and replies, issue comments, commit messages, evidence docs, CHANGELOG entries, and status updates in chat. Apply it as the last pass before publishing, and to any text a subagent drafted. Explains the actual problem in plain language before the fix, states what is proven separately from what is assumed, and cuts filler.
---

# Eshu Humanizer

Everything here is downstream of one rule: **the reader should finish knowing
what was broken, why it was broken, and what you actually proved.** If a
sentence does not move one of those three, cut it.

This is a pass you run over finished text, not a style to improvise. Apply it to
your own writing and to anything a subagent handed you — an executor's PR body
draft usually needs it more than your own chat reply does.

## Lead with the problem, in words a newcomer understands

The first paragraph explains what was wrong, in plain language, before any
mention of the fix. Not the symptom — the actual defect.

A reader who has never opened this file should understand the problem from that
paragraph alone. If understanding it requires knowing what a fencing token or a
cassette is, explain that in a clause, or pick a plainer framing.

```
Bad:   Fixed the module resolution confidence propagation in the drift writer.
Bad:   `outcome` was incorrect for some findings.
Good:  `outcome: "exact"` is a confidence claim. The writer was stamping it on
       findings whose address came from a heuristic fallback, not from a
       resolution that actually succeeded.
```

The second sentence is the one that matters. "Incorrect" tells the reader
nothing; "was stamping a confidence claim on a guess" tells them the shape of
the bug.

## Explain the mechanism, not just the outcome

Say *how* it broke. A reader who knows the mechanism can spot the same bug
elsewhere; a reader who knows only the symptom cannot.

```
Bad:   The fixture wasn't landing correctly.
Good:  Bash consumes the backslashes in a double-quoted `sed` pattern, so `sed`
       received a bare `$SENTINEL$`, where the trailing `$` is an end-of-line
       anchor. It matched nothing and exited 0.
```

The second version is longer and worth it. "Exited 0" is the detail that makes
the failure make sense.

## Separate what you proved from what you assumed

State the limit of your evidence in the same breath as the evidence. This is the
single highest-value habit in this repo, because a green gate is not the same as
a proven change.

```
Bad:   Verified — all gates pass.
Good:  B-7 gate green at 267s. Note that neither the schema-diff gate nor the
       payload-usage manifest can detect a semantic narrowing of a field's value
       domain, so their passing is not what this decision rests on.
```

When something is reachable in theory but not through the current code path, say
so rather than implying you caught a live bug:

```
Good:  One live defect, one latent. The `data.` collision is reachable today;
       the dotted-`for_each` collision is not, because this collector emits
       `[key:<hash>]` rather than the literal quoted form.
```

## Numbers, not adjectives

```
Bad:   The query was slow and is now much faster.
Good:  287ms at 500K rows, 3.1s at 2M, running every 5 minutes. Now 0.33-0.59ms.
```

If you do not have the number, say you do not have it. Do not reach for "should
be faster" or "significantly improved."

## Own errors in one sentence, then continue

State the correction plainly and move on. No apology paragraph, no re-litigating,
no tallying past mistakes.

```
Bad:   I sincerely apologize — I should have caught this earlier, and I want to
       acknowledge that this is the second time I've made a similar mistake...
Good:  My `-run` filter had a casing bug, so it matched zero tests and I reported
       green on nothing. Re-ran unfiltered.
```

Do not hide the error either. "Tests pass" when the filter matched nothing is a
false statement, and correcting it is not optional.

## Never bury the blocker

If something needs a decision, it goes near the top with exactly what you need
and what each answer causes. Do not let it surface in the last line of a status
update.

```
Good:  This will not merge until you decide one thing: bump the payload schema
       major, or treat the change as a correction. I recommend a correction —
       reasoning below. Everything else is green.
```

## Cut the filler

Delete on sight:

- "I'll now proceed to…", "Let me go ahead and…" — just do it
- "Great question!", "You're absolutely right!" — start with the answer
- Restating the request back to the user
- "It should be noted that", "It is worth mentioning that"
- "Successfully" on anything that would have errored if it failed
- Marketing adjectives: robust, comprehensive, seamless, powerful, leverage
- Em-dash pileups and three-clause sentences where one clause works

## Structure by content type

**Tables** carry evidence — runs, counts, before/after, pass/fail. One fact per
cell.

**Prose** carries reasoning. An argument compressed into a table cell stops being
an argument. If a row needs a "because," it belongs in a paragraph.

**Bullets** carry parallel items. If your bullets each run three sentences,
write paragraphs instead.

## PR bodies

Order that works, adapted as needed:

1. **The problem** — plain language, mechanism included, no fix mentioned yet.
2. **The fix** — what changed and why that shape, including options rejected and
   why. A reviewer who disagrees with the approach should find their objection
   already addressed.
3. **Evidence** — a table. Commands, numbers, gate results. Include the limits.
4. **Anything a reviewer would otherwise have to ask** — a coupling they will
   trip over, a deliberate gap, a decision that needs an owner.

Title states the change, not the ticket: `stop stamping drift findings exact
when the address came from a heuristic` beats `fix #5572`.

If review changed the shape of the work, rewrite the title and body to match the
final diff. A body describing the first attempt is worse than no body — it sends
reviewers hunting for code that is not there.

## Review replies

Say what changed, where, and what proves it. If the reviewer was right about
something you had argued against, say so plainly.

If a finding was correct when written but is now moot for a reason outside the
diff, explain the reason rather than just closing it:

```
Good:  Resolved by the sibling PRs merging, not by an edit here. Both #5593 and
       #5623 have since landed, so those rows now live in main. The finding was
       correct when you raised it.
```

Never resolve a thread with "fixed" alone.

## Issue comments on close

The comment is the durable record. Someone will read it in a year with no
context. Include what was wrong, what shipped, and — if the issue's own proposal
was incomplete — what it missed and why.

## What this is not

Not dumbing down. Precision goes up, not down: `mergeStateStatus=BLOCKED means
13 checks are still pending` is both plainer *and* more precise than "CI isn't
done."

Not padding. Plain language is usually shorter, because filler is the first
thing to go.

Not informality for its own sake. No forced jokes, no exclamation marks, no
emoji unless the user uses them first.

## Final pass

Before publishing, check:

- [ ] First paragraph explains the actual problem, not the fix.
- [ ] Mechanism stated, not just symptom.
- [ ] Every claim's evidence named, and its limits stated.
- [ ] Numbers where numbers exist.
- [ ] Anything needing a decision is near the top.
- [ ] No filler openers, no marketing adjectives.
- [ ] Tables hold facts; reasoning is in prose.
- [ ] A reader outside this lane could follow it.
- [ ] No AI attribution anywhere (repo rule, and it never adds information).
