<!-- docs-catalog
title: Documentation style guide
description: Defines how contributors choose, structure, and write public Eshu documentation.
type: project
audience: contributor, maintainer
entrypoint: false
landing: false
-->

# Documentation style guide

Use this guide when you add or revise a page under `docs/public/`. It turns the
architecture decision in
`docs/internal/design/4593-docs-restructure-diataxis.md` into writing rules.
That decision owns the site structure and the basic page templates. This guide
owns voice, page boundaries, diagrams, headings, and code examples.

The [prose-quality reference](reference/prose-quality.md) describes the checks
the repository can automate. Passing that check does not prove that a page has
the right purpose or tells a coherent story. Review those qualities by reading
the page as its intended reader.

The [docs-catalog metadata reference](reference/docs-catalog.md) defines the
hidden metadata block at the top of public pages. Set its `type` to the page
shape you chose, name the real audience, and mark entrypoints only when a
catalog landing page links to them. A page's navigation section does not
override its metadata type.

## Choose the page type first

Choose by the reader's immediate need, not by the code package being described.

| Reader need | Page type | Home in the navigation |
| --- | --- | --- |
| Learn by completing a guided exercise | Tutorial | Tutorials |
| Complete a specific task | How-to | How-to Guides |
| Understand why the system works this way | Concept | Concepts |
| Look up an exact contract or option | Reference | Reference |
| Run or recover Eshu safely | Operate | Operate |
| Explain project policy or participation | Project | Project |
| Record reproducible public evidence | Proof | Reference or Project |

Ask what the reader should be able to do after reading. If the answer contains
both “understand” and “configure,” split the page and link the two parts. Do not
hide a command catalog inside a concept page or interrupt a task with the full
theory behind it.

**Get Started** is a route into the smallest useful result. It may link to a
tutorial or a short setup task, but it is not a metadata type. Classify each
Get Started page by its actual job.

Tutorial, how-to, concept, and reference are the four Diátaxis reading modes.
Eshu also uses `operate`, `project`, and `proof` metadata where operational
safety, project participation, or evidence is the page's main purpose.

## Write in Eshu's voice

- Address the reader as “you.” Use imperative verbs for steps: “Run,” “Open,”
  “Check,” and “Compare.”
- Use active voice and name the actor. Write “The reducer retries the work”
  instead of “The work is retried.”
- Use present tense for behavior that exists now. Use future tense only for a
  consequence of a step. Describe planned behavior as planned and link its
  public issue.
- Lead with the reader's outcome. Explain project history only when it changes
  what the reader should do.
- Prefer concrete nouns and numbers. Replace claims such as “fast” or “easy”
  with measured behavior or remove them.
- Define an Eshu-specific term the first time it appears. A reader should not
  need an issue, design note, or package README to decode the page.
- State limits beside the claim they qualify. Do not leave a caveat in a later
  section where a reader can miss it.

Keep a calm, factual tone. Do not use launch language, jokes that obscure a
step, or claims that the evidence does not support. If a command was not run in
the documented environment, say where its behavior came from.

## Shape each page for its job

### Tutorial

A tutorial teaches through a complete, reproducible exercise. Use this order:

1. State what the reader will build or observe.
2. Give the time, prerequisites, and starting state.
3. Walk through one path in sequence.
4. Show checkpoints after meaningful steps.
5. Explain common failures that can occur on that path.
6. If the exercise changes state, explain how to clean it up.
7. End with a small set of next pages.

Keep choices out of the main path. If readers must compare several production
options, link to a concept or reference page before the exercise starts.

### How-to

A how-to page solves one named task for a reader who already knows the basics.
Start with the outcome and required state. Give the shortest safe sequence,
then show how to verify the result. Put troubleshooting after the working path
and link exact flags or schemas to reference pages.

Do not teach the product model in the middle of the task. A sentence of context
is enough when it prevents a mistake; deeper explanation belongs in Concepts.

### Concept

A concept page explains a durable mental model. Start with the problem the
model solves, then explain the parts, their relationships, and the boundaries
of the model. Use a small example to make the model concrete. Link to tutorials
or how-to pages for action and to Reference for exact contracts.

Do not bury the explanation in flags, environment variables, or exhaustive
field tables. Mention an option only when it changes the mental model.

### Reference

A reference page supports exact lookup. Name its source of truth, version or
scope, and owner when those are not obvious. Organize commands, routes, fields,
defaults, errors, and limits so readers can scan them without following a
story. Short examples may clarify a contract, but they should not become a
tutorial.

Generated reference must carry a generated marker, name its generator, and
have a drift check. Edit the source or generator instead of the generated page.

### Operate

An operate page begins with the operational objective and preconditions. It
names the signals to inspect, distinguishes normal from degraded state, and
gives recovery or escalation steps. Link the relevant logs, metrics, traces,
and status surfaces so an operator can verify each decision. Use a how-to page
instead when the task does not need an operational state model.

### Project

A project page explains how people participate in Eshu or how the project makes
public decisions. State who the page is for, what is current, and where durable
status lives. Keep temporary delivery notes in issues instead of turning the
public guide into a work log.

### Proof

A proof page records evidence that supports a public claim. Name the claim,
scope, environment, commands, results, and limits. Keep observations separate
from conclusions and link the product or reference page that consumes the
evidence. Maintainer-only run logs belong under `docs/internal/`, not in the
public navigation.

Reference and proof pages are exempt from the automated human-prose checks
because exact tables and evidence can be dense. Generated pages are exempt too.
The exemption is from that checker, not from clear writing or source-of-truth
rules. Apply this guide to their handwritten source and surrounding prose.

### Get Started

A Get Started page should make the next action obvious and reach one useful
result quickly. Remove branches that can wait until after that result. Link to
Concepts and Reference instead of front-loading them, and record the page's
actual type in its docs-catalog metadata.

## Use diagrams when relationships are the hard part

A page earns a diagram when prose or a small table cannot explain a sequence,
state transition, hierarchy, ownership boundary, or flow without making the
reader hold several relationships in memory. Use Mermaid for diagrams that can
be maintained as text.

Do not add a diagram to decorate a page or repeat a linear list. Keep labels
short, mark important boundaries, and explain the takeaway in nearby prose so
the page still works for someone using a screen reader or a text-only view.
Prefer stable system roles over screenshots of a UI that changes often.

## Keep headings useful in isolation

- Use one H1 and sentence case.
- Give how-to and tutorial headings an action or outcome.
- Name the model or lookup subject in concept and reference headings.
- Keep the hierarchy shallow. Split the page if headings need more than three
  levels to stay understandable.
- Do not put issue numbers, implementation phases, or internal team names in a
  public heading.
- Avoid headings such as “Overview,” “Details,” or “Miscellaneous” when a more
  specific label tells the reader what follows.

## Make code examples safe to copy

- Tag every fence with its language, such as `bash`, `json`, `yaml`, or `text`.
- Omit shell prompts. Put commands and their output in separate fences.
- State the working directory and prerequisites before they matter.
- Use placeholders that explain their shape, such as `<repository-url>`, and
  use the same placeholder throughout the page.
- Show the expected result when it proves that a step worked. Keep output to
  the lines the reader needs.
- Test commands against the current CLI. Check flags and environment variables
  against code-owned sources instead of copying them from another page.
- Never use real credentials, account identifiers, private hostnames, or
  machine-specific paths. Generic paths such as `/tmp/eshu-example.json` and
  clearly named placeholders are safe when the task needs a filesystem path.

Prefer one command per fence when the reader may need to diagnose a failure.
If commands must run together, explain whether later commands depend on earlier
ones succeeding.

## Link instead of duplicating

Keep one source for each exact contract. A task page may summarize why a value
matters, then link to the reference page that owns its spelling and default. A
concept page may link to a how-to without repeating its steps.

Use descriptive link text. “Configuration reference” helps a reader decide;
“click here” does not. Use relative Markdown links between public pages so the
strict docs build can catch broken paths.

## Review and verification

Before opening a documentation pull request:

1. Confirm the page type and intended reader.
2. Check commands, flags, environment variables, response shapes, and links
   against their current sources.
3. Read the page once for the reader's path, without consulting the code.
4. Run the focused docs checks:

```bash
bash scripts/verify-docs-catalog.sh
bash scripts/verify-docs-prose-quality.sh
bash scripts/verify-docs-contradiction.sh
bash scripts/verify-docs-refs.sh
bash scripts/verify-docs-cli-env-refs.sh
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

These commands are the focused docs checks, not the complete gate selection for
every change. Follow the repository's required review sequence and run
`make pre-pr` at the late promotion point before a push; it selects the
authoritative preflight for the final changed paths.

The prose and contradiction checks are advisory, but review every finding. A
justified exception should name the reason in the pull request instead of
teaching the checker to ignore a wider class of pages.
