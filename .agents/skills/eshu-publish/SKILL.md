---
name: eshu-publish
description: Use when writing or editing an Eshu PR, issue, review reply, issue close, decision record, owner report, or other prose a person will read. Gives short ASD-STE100-style wording and a fixed skimmable shape. Uses Mermaid and tables where they help, and collapses evidence below the answer.
---

# Eshu publish

A reader must get the point in the first 10 seconds and find the proof in the
next minute. The owner reads many of these at once, so the top of the text is
the product. Pick the artifact, then read its reference.

| Artifact | Reference |
|---|---|
| Issue (new, or a rewrite) | [issue](references/issue.md) |
| PR title and body | [pull request](references/pull-request.md) |
| Review reply, issue close, status comment, resume note | [replies](references/replies.md) |
| Design or decision record | [decision record](references/decision-record.md) |
| Report back to the owner | [owner report](references/owner-report.md) |

**Other prose.** For `doc.go`, `README.md`, `AGENTS.md`, and other package docs,
use the Wording rules only. The Shape section below does not apply to them.
Keep their own structure. `<details>` and a bold lead do not belong in a godoc
comment.

## Wording (STE-flavored)

The full rule set and its sources are in [wording](references/wording.md). The
shared sentence rules are in `docs/internal/writing-for-agents.md`. These rules
are specific to published text:

- Keep every hedge. Write "may have failed" as "may have failed". Label a
  theory as a theory. Never add a fact the source did not state.
- Keep logs, identifiers, test names, commands, and reviewer text verbatim.
- Use one word for one thing across the whole text.
- Say "STE-style". Never claim ASD-STE100 compliance: the approved-word list is
  not public domain and this skill does not check it.

## Shape

This section covers issues, PR bodies, replies, decision records, and owner
reports.

- **Lead.** The first sentence says what is broken or what changed, and why the
  reader cares. Bold it. It must make sense with no other context.
- **Decisions and blockers** come directly after the lead. Settle a decision
  with evidence or the arbiter first. Raise it to the owner only for an act that
  only the owner can authorize.
- **Headings.** Use `Problem`, `Expected`, `Acceptance criteria` for issues and
  `Problem`, `What changed`, `Proof` for PRs. These match the best existing
  issues and PRs.
- **Paragraphs.** One topic each. Keep one under about 600 characters. Split a
  longer one or turn it into a list.
- **Lists and tables.** Use a list for 3 or more steps, or for 3 or more conditions. Use a table
  to compare before and after, or claim and evidence.
- **Pictures.** Use a Mermaid diagram for a flow, a dependency, or a before
  and after that has 3 or more nodes. Label each node with the real
  identifier. Use a file tree or a `diff` block for a layout change. The
  `show-me` skill, when your harness has it, helps choose the view.
- **Collapse the data.** Put a log, command output, verbatim reviewer or
  arbiter text, or a file list inside `<details>` when it runs past 10 lines. The summary line says what is
  inside. Keep the claim that the data supports outside the fold.
- **Confidence.** Mark each claim as `Proven`, `Inferred`, or `NOT_CHECKED`, and
  give the command or source for `Proven`. End with a `NOT_CHECKED` line.
- **Scale to the change.** Delete a section that has nothing to say.
- **No decoration.** Skip bold for emphasis, emoji, alerts, forced triples, and
  praise words. Do not explain at a child's level.

## Rules that do not change

`AGENTS.md` owns these rules. This list repeats them because published text
breaks them first.

- A PR body starts with `Fixes #N.` or `Refs #N.`. Use a closing keyword only
  for an issue that must close on merge. `scripts/dev/pre-enqueue-check.sh`
  fails a body that has no `#N` reference.
- No AI attribution in any title, body, comment, or commit.
- The PR title and body must describe the final diff. Rewrite them when review
  changes the work.
- A byte changed in the title or body after you capture the
  `ci-gates review-attest` claims file voids the receipt. If you must edit,
  repeat the affected proof and the full review, then capture a new receipt.
- Keep issue headings of the `competitive-audit` form exactly as the form
  defines them. `audit-preflight` parses them. Keep the fields of the
  `wrong-answer` form, including the share-safe attestation.
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
