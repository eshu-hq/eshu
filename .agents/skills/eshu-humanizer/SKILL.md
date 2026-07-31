---
name: eshu-humanizer
description: Use when writing anything a person will read — PR titles and bodies, review comments and replies, issue comments, commit messages, evidence docs, CHANGELOG entries, and chat updates. Run it as the last pass before publishing, including over text a subagent wrote. Strips the words and sentence shapes that make writing sound machine-generated, and rewrites jargon so a working developer understands it without a glossary.
---

# Eshu Humanizer

Two jobs. Stop sounding like a machine, and stop writing only for people who
already know this codebase.

Write for a competent developer who has never opened this repo. Not a beginner —
they know Go, SQL, and Docker. They do not know what a cassette is, what B-7
means, or why a fencing token exists. If your sentence needs that knowledge and
does not supply it, rewrite the sentence.

Run this as a pass over finished text. It applies just as much to a PR body an
executor drafted as to your own chat reply — usually more.

## Part 1: Say it plainly

### Explain the shorthand the first time you use it

Every project word gets a short gloss on first use, then you can use it freely.

```
Bad:   The B-7 gate is green and the cassette replays clean.
Good:  The golden-corpus gate (B-7) is green — that's the run that indexes 20
       real repos and checks the answers against a saved snapshot.
```

Repo terms that always need a gloss: cassette, Odù, B-7, B-12, fencing token,
reducer intent, scope generation, projection, admission, lease, drift outcome.
Assume none of them mean anything to the reader.

### Pick the shorter word

| Instead of | Write |
| --- | --- |
| utilize, leverage | use |
| facilitate, enable | let, help |
| prior to | before |
| in order to | to |
| is able to | can |
| a number of | some, several, or the actual number |
| at this point in time | now |
| in the event that | if |
| terminate | stop, end |
| initiate | start |
| exhibits, demonstrates | shows |
| attempt | try |

### Cut the words that add nothing

Delete on sight: `It's worth noting that`, `It should be mentioned that`,
`Importantly`, `Notably`, `Essentially`, `Fundamentally`, `In essence`,
`At its core`, `Simply put`, `Needless to say`, `As we can see`,
`It is important to understand that`.

Also delete `successfully` from anything that would have errored if it failed.
"Successfully committed" is just "committed."

### Say who did what

Passive voice hides the actor and it is the fastest way to sound like a report
nobody wrote.

```
Bad:   It was determined that the query was not using the index.
Good:  EXPLAIN showed the query skipping the index.

Bad:   The conflict markers were committed.
Good:  I committed the conflict markers — `git add -A` staged them while I was
       only looking at one file.
```

Objects do not act on their own. "The migration decided," "the gate believes,"
"the query wants" — none of those things want anything. Name the real actor or
rewrite.

## Part 2: Stop sounding like a machine

These are the patterns readers clock as AI writing. The list comes from
[Wikipedia's Signs of AI writing](https://en.wikipedia.org/wiki/Wikipedia:Signs_of_AI_writing),
which is the most complete catalogue anyone maintains.

### Banned words

Never ship these: `delve`, `tapestry`, `landscape` (as metaphor), `realm`,
`robust`, `seamless`, `crucial`, `pivotal`, `vital`, `intricate`, `meticulous`,
`comprehensive`, `holistic`, `nuanced`, `underscore`, `showcase`, `highlight`
(as verb), `foster`, `garner`, `boast`, `testament`, `vibrant`, `rich`,
`profound`, `groundbreaking`, `transformative`, `game-changer`, `paradigm`,
`unprecedented`, `elevate`, `empower`, `unlock`, `unleash`, `harness`,
`cutting-edge`, `state-of-the-art`, `best-in-class`, `synergy`, `streamline`.

Also drop the connective tics: `Moreover`, `Furthermore`, `Additionally`,
`Consequently`, `Thus`. Start the sentence, or use `and`, `so`, `but`.

### Don't dodge "is"

Machines avoid plain copulas. Say what a thing is.

```
Bad:   The pairing function serves as a mechanism for correlating the two halves.
Good:  The pairing function matches the two halves.

Bad:   Migration 089 represents a shift toward durable ordering.
Good:  Migration 089 replaces the wall-clock token with a Postgres sequence.
```

Watch for: `serves as`, `stands as`, `functions as`, `operates as`,
`represents`, `constitutes`, `acts as`, `plays a role in`.

### Kill the rule of three

Three parallel items in a row is the single loudest tell. It shows up because
three sounds thorough, not because there were three things.

```
Bad:   The fix is safe, correct, and performant.
Good:  The fix adds no new queries. It runs one extra directory walk per row,
       bounded at depth 10.
```

If there really are three, keep them. If you padded to three, cut to two.

### Kill "not just X, but Y"

```
Bad:   This is not just a test failure, but a signal about our proof strategy.
Good:  The test failure shows our proof strategy has a hole.

Bad:   The gate doesn't merely check syntax — it validates semantics.
Good:  The gate validates semantics too.
```

Same for `X rather than Y` used for drama, and `It's not about A, it's about B`.

### Stop inflating significance

Machines end paragraphs by explaining why the thing mattered. Do not.

```
Bad:   ...which underscores the importance of rigorous verification in
       maintaining system integrity.
Good:  (delete the sentence)
```

Cut any clause starting `highlighting`, `underscoring`, `emphasizing`,
`reflecting`, `symbolizing`, `demonstrating the importance of`,
`contributing to`, `setting the stage for`, `marking a shift`.

### Vary the rhythm

Machine prose runs every sentence to about the same length. Real writing does
not. Put a four-word sentence next to a thirty-word one. Read it aloud — if it
has a metronome beat, break it.

### Formatting tells

- **Em dashes.** One per paragraph, maximum. They pile up fast.
- **Bold.** For the two or three things that genuinely matter, not every term.
- **Title Case Headings.** Use sentence case.
- **Bullet lists where every bullet is `**Term:** description`.** Write
  paragraphs, or make the bullets actual short items.
- **Lists of exactly three.** See above.
- **Emoji.** Only if the reader used them first.

## Part 3: Be honest about evidence

This part is specific to this repo, because the failure mode here is claiming
proof you do not have.

### Say what you proved and what you assumed, together

```
Bad:   Verified — all gates pass.
Good:  All gates pass. But neither the schema-diff gate nor the payload-usage
       manifest can detect a value changing meaning, so their passing is not
       what this rests on.
```

### Say when a bug is theoretical

```
Good:  One of these is live and one is not. A data-source address collides with
       a managed resource today. The dotted-key collision needs an address shape
       this collector never emits.
```

### Give the number

```
Bad:   The query was slow, now it's much faster.
Good:  287ms at 500K rows, 3.1s at 2M. Now 0.33-0.59ms.
```

No number? Say so. Never reach for "should be faster."

### Own a mistake in one sentence

```
Bad:   I sincerely apologize for this oversight and want to acknowledge...
Good:  My test filter had a typo, matched zero tests, and I called it green.
       Re-ran it properly.
```

Then keep going. No apology paragraph, no listing past mistakes.

### Put the blocker first

If someone has to decide something, it goes near the top with what each answer
causes. Not in the last line.

## Part 4: Shapes

### PR body

1. **What was broken** — plain words, no fix yet. Include *how* it broke, not
   just that it did. "`sed` exited 0 having replaced nothing" beats "the fixture
   didn't land."
2. **What changed** — and why this shape. Name the approaches you rejected, so a
   reviewer who disagrees finds their objection already answered.
3. **Evidence** — a table. Commands, numbers, results, and the limits.
4. **What a reviewer would otherwise have to ask** — a coupling they will hit, a
   gap you left on purpose, a decision that needs an owner.

Title says what changed, not the ticket number:
`stop stamping drift findings exact when the address came from a guess` beats
`fix #5572`.

If review changed the approach, rewrite the title and body. A body describing
your first attempt sends reviewers looking for code that is not there.

### Review reply

What changed, where, and what proves it. If the reviewer was right and you had
argued otherwise, say so.

If their finding is now moot for a reason outside your diff, explain the reason:

```
Good:  This resolved itself when #5593 and #5623 merged — those rows live in
       main now, not in this PR. Your finding was right when you filed it.
```

Never close a thread with just "fixed."

### Issue close

Someone reads this in a year with no context. What was broken, what shipped,
and — if the issue's own plan was wrong — what it missed.

## What this is not

Not dumbing down. Plain writing is more precise: "13 checks still running" beats
"CI isn't done."

Not padding. Cutting filler usually makes it shorter.

Not forced casualness. No jokes you had to reach for, no exclamation marks.

## Last pass

- [ ] A developer outside this repo could follow it.
- [ ] Every project term glossed on first use.
- [ ] No banned words.
- [ ] No rule of three, no "not just X but Y", no `serves as`.
- [ ] No sentence explaining why the thing mattered.
- [ ] Sentence lengths vary. Read it aloud.
- [ ] One em dash per paragraph at most.
- [ ] Numbers where numbers exist; limits stated next to claims.
- [ ] Anything needing a decision is near the top.
- [ ] No AI attribution (repo rule, and it tells the reader nothing).
