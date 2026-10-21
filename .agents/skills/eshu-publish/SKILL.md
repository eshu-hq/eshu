---
name: eshu-publish
description: Write or rewrite Eshu issues, PRs, commits, review comments, decision records, reports, and documentation for capable colleagues. Preserve technical accuracy, evidence, and required artifact formats; apply the repository communication standard silently.
---

# Eshu publish

Start with what matters, then give the context and evidence a teammate needs.
Use the repository [communication standard](../../../docs/internal/writing-for-agents.md)
for original writing and rewrites. It applies by default, including progress
updates, without a personal humanizer or global skill. Pick the artifact, then
read its reference. Keep editing passes internal unless the user requests them.

| Artifact | Reference |
|---|---|
| Issue (new, or a rewrite) | [issue](references/issue.md) |
| PR title and body | [pull request](references/pull-request.md), then the skeleton for your kind of PR in [templates](references/pr-templates.md) |
| Review reply, issue close, status comment, resume note | [replies](references/replies.md) |
| Commit message that grows a Go directory past 40 non-test files | [`Dirgate-Growth` trailer](references/commit-trailers.md) |
| Design or decision record | [decision record](references/decision-record.md) |
| Report back to the owner | [owner report](references/owner-report.md) |

**Other prose.** For `doc.go`, `README.md`, `AGENTS.md`, and other package docs,
use the Wording rules only. The Shape section below does not apply to them.
Keep their own structure. `<details>` and a bold lead do not belong in a godoc
comment.

## Wording

The shared voice and instruction rules live in
[Writing For Agents](../../../docs/internal/writing-for-agents.md). The
[wording reference](references/wording.md) gives examples for conversations,
commits, PRs, and review comments. Preserve technical terms, explain unfamiliar
ones in context, and retain reasoning, tradeoffs, and useful uncertainty.

- Keep the strength of a claim. "May have failed" stays uncertain; do not turn
  a theory into a fact or invent successful validation.
- Keep logs, identifiers, test names, commands, and quoted reviewer text verbatim.
- Use consistent names for the same technical concept. Explain shorthand such as
  "initial measurements" instead of leaving a reader to decode "T0 census".
- Preserve required formats and an explicit request to keep an existing shape.
  Style advice does not change engineering, evidence, privacy, or approval rules.

## Shape

This section covers issues, PR bodies, decision records, and owner reports.
Release and operational checklists keep their existing prerequisite and monitoring
sections; use `--issue-checklist`. Bug and feature issues use the issue template.
Replies, closes, and status comments follow [replies](references/replies.md).
They answer first and need no bold lead, glance table, or `<details>` block.

- **Lead (standard issues, PRs, and reports).** The first sentence says what is
  broken or what changed, and why the reader cares. Bold it. It must make sense
  with no other context.
- **At a glance (PRs).** Directly after the lead, a table with the header
  `| At a glance | |` and 4 rows of one sentence each. The rows depend on the
  kind of PR. See the templates. Put counts by package and lists of files in
  `<details>`, not in this table.
- **Decisions and blockers** come after the lead. In a PR, they come after the
  At a glance table. Settle a decision with evidence or the arbiter first. Raise
  it to the owner only for an act that only the owner can authorize.
- **Headings.** Use `Problem`, `Expected`, `Acceptance criteria` for bug or
  feature issues; use `Problem`, `What changed`, `Proof` for PRs.
- **Paragraphs.** One topic each. Keep one under about 600 characters. The
  shape check warns above 600 bytes and fails above 800. Split a
  longer one or turn it into a list.
- **Lists and tables.** Use lists for steps or conditions that a reader must
  check separately. Use a table when it helps compare before and after, or claim
  and evidence.
- **Pictures.** Use a Mermaid diagram when it clarifies a flow, dependency, or
  before and after. Label each node with the real identifier.
  Use a file tree or a `diff` block for a layout change.
- **Collapse the data.** Always put gate output, quoted reviewer or arbiter
  text, and the evidence section inside `<details>`. Put any other log,
  command output, or file list inside `<details>` when it has more than 10
  lines. The summary line says what is inside. Keep the claim that the data
  supports outside the fold.
- **Confidence.** In an owner report, tag each claim `Proven`, `Inferred`, or
  `NOT_CHECKED`, and give the command or source for `Proven`. In a PR or an
  issue, label a theory as a theory and put the evidence in the Proof or Problem
  section. End a PR with a `NOT_CHECKED` line.
- **Scale to the change.** Delete a section that has nothing to say.
- **No decoration.** Skip bold for emphasis, emoji, alerts, forced triples, and
  forced praise. Explain unfamiliar topics to a capable colleague.

## Rules that do not change

`AGENTS.md` owns these rules. This list repeats them because published text
breaks them first.

- A PR body starts with `Fixes #N.`, `Refs #N.`, or `Refs owner/repo#N.`.
  Use a closing keyword only for an issue that must close on merge. `scripts/dev/pre-enqueue-check.sh`
  fails a body that has no `#N` reference.
- No AI attribution in any title, body, comment, or commit.
- The PR title and body must describe the final diff. Rewrite them when review
  changes the work. A wrong derivable figure that the verdict's P3 list records
  is the exception (see
  [Derivable figures](../eshu-code-review/references/merge-bar.md#derivable-figures)).
- A byte changed in the title or body after you capture the
  `ci-gates review-attest` claims file voids the receipt. If you must edit,
  repeat the affected proof and the full review, then capture a new receipt.
- Before you capture the receipt, fix a wrong derivable figure. After capture,
  do not edit the body only to fix one. Record the correct figure in the
  verdict's P3 list.
- Keep issue headings of the `competitive-audit` form exactly as the form
  defines them. `audit-preflight` parses them. Keep the fields of the
  `wrong-answer` form, including the share-safe attestation.
- Never close a review thread with only "fixed". Name the change and the proof.
- Keep pod IDs, image digests, credentials, and employer-private names out of
  public text.

## Check the draft

1. Read the lead alone. Does it stand without the rest?
2. Run the shape check on the draft before you capture the review receipt.
   For release and operational checklists, select `--issue-checklist` to preserve
   their sections. The check still enforces privacy and paragraph limits. It
   fails a PR body without a local or qualified `Refs` first line, a bold lead,
   or the glance table, and any prose paragraph over 800 bytes. Fix every `FAIL` line. Skip
   the check for an issue made from an issue form, for a revert, and for an edit
   that changes no text. `AGENTS.md` owns this rule. A body under 600 characters
   with no heading may omit the glance table.

```bash
bash .agents/skills/eshu-publish/scripts/check-shape.sh --pr body.md
bash .agents/skills/eshu-publish/scripts/check-shape.sh --issue issue.md
bash .agents/skills/eshu-publish/scripts/check-shape.sh --issue-checklist checklist.md
```

3. Review wording with the repository communication standard. Keep technical
   meaning, evidence, and useful detail while removing padding and unexplained
   shorthand. No external linter or global skill is required.

4. Compare every claim that bears on a decision with the diff and the proof.
   Write a derivable figure as a link or a command, not as a hand-typed count
   (see [Derivable figures](../eshu-code-review/references/merge-bar.md#derivable-figures)).
