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

A reply is usually 40 to 120 words. The 58 inline review comments in the
last 30 PRs had a median of 70 words. Keep to that range.

## A long review comment

When a review has many findings, do not write one block.

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

Put the state first: `Done`, `Blocked`, or `Needs a decision`. Then the reason
in one sentence and the next step. If you wait on the owner, say so plainly and
give your recommendation.
