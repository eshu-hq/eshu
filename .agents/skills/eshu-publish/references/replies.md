# Replies, closes, and status comments

Write these in a plain human voice. Use "I" for what you did. Relax the length
caps of the wording rules, but keep one idea in each sentence.

## Review reply

Say what changed, where, and what proves it. Start with the answer.

- Fixed: `Fixed in <sha>: <the change>. <The test or command that proves it>.`
- The reviewer was right and you argued otherwise: say so in one sentence.
- Not changed: give the reason and the evidence. Do not say "by design" alone.
- Moot for a reason outside your diff: name the reason.

  ```text
  This resolved itself when #5593 and #5623 merged. Those rows live in main
  now, not in this PR. Your finding was right when you filed it.
  ```

Never close a thread with only "fixed". Fix the code first, then reply.

A reply is usually 40 to 120 words. The median inline review comment in the
last 30 PRs was about 70 words. Keep to that range.

## A review comment with many findings

When a review has more than 3 findings, do not write one block.

1. Lead: the verdict and the count. `Not ready: 1 P1, 2 P2.`
2. A list. One finding per item: severity, the claim, the file, the fix.
3. Put verbatim evidence and tool output in `<details>`.

## Issue close

The reader has no context and may read this in a year. Write three things:

1. What was broken.
2. What shipped, with the PR link.
3. What the issue's own plan got wrong or missed, if anything.

If part of the work remains, comment on the issue with what remains.

## Status comment

Put the state first: `Done`, `Working`, `Blocked`, or `Needs a decision`. Then
the reason in one sentence and the next step.

Use `Needs a decision` only after the arbiter route cannot settle the question,
or for an act that only the owner can authorize. Say so plainly and give your
recommendation. Otherwise report `Done` or `Working` and record the arbiter
verdict.

## Resume note or handoff

Write for a reader who has none of your context. Give four things, in this
order:

1. The goal, in one sentence.
2. The state: what you finished, with the commit or PR, and what remains.
3. The next action, as one imperative sentence.
4. What you did not check, as a `NOT_CHECKED` list.

Use the Wording rules. Do not add a bold lead, a diagram, or a `<details>` block
unless the note runs past 10 lines.
