# Decision record

Eshu keeps decisions in `docs/internal/design/<issue>-<slug>.md`. Do not start an
`adr/` folder. A decision record has two readers: the owner, who must approve
it, and an agent, who must build from it. The owner needs the decision in the
first 15 lines. The agent needs measurable limits and a checklist.

## Order

1. **Title.** A verb phrase that states the decision, then the issue number:
   `# Use Neo4j as the supported graph backend (#7331)`.
2. **Status line.** `Status:` and one of `Proposed`, `Accepted (who, date)`,
   `Rejected`, or `Superseded by <link>`. Say what the document does not approve,
   such as a schema change or a deployment.
3. **Decision.** One or two bold sentences, before any context. If the owner must
   decide, add your recommendation and the cost of each option.
4. `## Context`. Why now, what limits the choice, and the measured facts with
   their source. Label a theory as a theory.
5. `## Options`. A table: option, what it costs, why chosen or why not. Add a
   Mermaid diagram when the options differ in structure.
6. `## Decision`. The detail: scope, non-goals, and limits as numbers. Write
   "p95 under 200 ms on the named corpus", not "fast".
7. `## Consequences`. Two lists, `Good` and `Bad`. Number each follow-up issue.
8. `## Implementation plan`. Affected paths, patterns to follow, patterns to
   avoid, and the telemetry an operator needs at 3 AM.
9. `## Acceptance criteria`. A task list. Each item is a check that a test,
   command, or query can run.
10. `<details>` named `Evidence and measurements`. Tables, logs, and quoted
    rulings go here.

## Rules

- **Supersede, do not rewrite.** When a decision changes, write a new record and
  set the old `Status:` to `Superseded by <link>`. Edit an accepted record only
  for its status, links, and typos.
- **Agent test.** Before you publish, ask: can an executor implement this
  without asking a question? If not, add the missing limit, path, or check.
- **Size.** Aim for 250 lines or fewer. The Markdown cap is 500 lines. Move long
  evidence to the `<details>` block or to a linked file.
- **Open questions.** Number them under `## Decisions for the arbiter`. Name the
  options. Do not hide a question inside a paragraph.
- Do not record a routine choice inside an established pattern, a bug fix, or a
  style preference that a linter already enforces.

## Source

The agent-test and checklist ideas come from the `adr-skill` in
[vercel/ai](https://github.com/vercel/ai/tree/main/skills/adr-skill)
(Apache-2.0). This file copies none of its text.
