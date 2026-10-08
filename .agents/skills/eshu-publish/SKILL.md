---
name: eshu-publish
description: Use when writing or editing an Eshu PR title or body, issue, review reply, issue close, or owner report. Gives short ASD-STE100-style wording, a fixed skimmable shape, Mermaid and tables where they help, and evidence collapsed below the answer.
---

# Eshu publish

A reader must get the point in the first 10 seconds and find the proof in the
next minute. The owner reads many of these at once, so the top of the text is
the product. Pick the artifact, then read its reference.

| Artifact | Reference |
|---|---|
| Issue (new, or a rewrite) | [issue](references/issue.md) |
| PR title and body | [pull request](references/pull-request.md) |
| Review reply, issue close, status comment | [replies](references/replies.md) |
| Report back to the owner | [owner report](references/owner-report.md) |

## Wording (STE-flavored)

Details and sources: [wording](references/wording.md).

- One idea in each sentence. Keep it to 20 words or fewer for a step, 25 for a
  description. Use the active voice and name the actor.
- Use one word for one thing. Do not rotate `lease`, `claim`, and `hold`.
- Put the condition first: "If the test fails, fix the cause."
- Write the verb, not the noun made from it: "check", not "perform a check of".
- Use no semicolons and no phrasal verbs ("set up", "take off").
- Keep every hedge. Write "may have failed" as "may have failed". Label a
  theory as a theory. Never add a fact the source did not state.
- Keep logs, identifiers, test names, commands, and reviewer text verbatim.
- Say "STE-style". Never claim ASD-STE100 compliance: the approved-word list is
  not public domain and this skill does not check it.

## Shape

- **Lead.** The first sentence says what is broken or what changed, and why the
  reader cares. Bold it. It must make sense with no other context.
- **Decisions and blockers** come directly after the lead.
- **Headings.** Use `Problem`, `Expected`, `Acceptance criteria` for issues and
  `Problem`, `What changed`, `Proof` for PRs. These match the best existing
  issues and PRs.
- **Paragraphs.** One topic each. Keep one under about 600 characters. Split a
  longer one or turn it into a list.
- **Lists and tables.** Use a list for 3 or more steps or conditions. Use a table
  to compare before and after, or claim and evidence.
- **Pictures.** Use a Mermaid diagram when the text describes a flow, a
  dependency, or a before and after with 3 or more parts. Label each node with
  the real identifier. Use a file tree or a `diff` block for a layout change.
  See the `show-me` skill for the choice of view.
- **Collapse the data.** Put logs, long command output, verbatim reviewer or
  arbiter text, and file lists inside `<details>`. The summary line says what is
  inside. Keep the claim that the data supports outside the fold.
- **Confidence.** Mark each claim as `Proven`, `Inferred`, or `Not checked`, and
  give the command or source for `Proven`. End with a `Not checked` line.
- **No decoration.** Skip bold for emphasis, emoji, alerts, forced triples, and
  praise words. Do not explain at a child's level.

## Rules that do not change

- A PR body starts with `Fixes #N.` or `Refs #N.`. Use a closing keyword only
  for an issue that must close on merge. `scripts/dev/pre-enqueue-check.sh`
  fails a body that has no `#N` reference.
- No AI attribution in any title, body, comment, or commit.
- The PR title and body must describe the final diff. Rewrite them when review
  changes the work.
- Do not edit a title or body after you capture the `ci-gates review-attest`
  claims file. Any changed byte voids the receipt.
- Keep issue headings of the `competitive-audit` form exactly as the form
  defines them. `audit-preflight` parses them.
- Never close a review thread with only "fixed". Name the change and the proof.
- Keep pod IDs, image digests, credentials, and employer-private names out of
  public text.

## Check the draft

1. Read the lead alone. Does it stand without the rest?
2. Run the linter when it is available. It checks semicolons, long sentences,
   phrasal verbs, passive voice, and synonym rotation. It is a heuristic.
   Fix hard findings. Judge advisory findings.

```bash
python3 -I ~/os-repos/asd-ste100-skill/scripts/ste-lint.py draft.md
```

3. Compare every number and claim in the draft with the diff and the proof.
