<!-- docs-catalog
title: Docs Prose Quality
description: Defines the advisory prose-quality gate for human-facing Eshu documentation.
type: reference
audience: maintainer, docs author
entrypoint: false
-->

# Docs Prose Quality

The prose-quality gate keeps human-first docs concise, task-focused, and easy to
scan after the documentation restructure. It is advisory first: it reports
findings but exits successfully unless enforcement is explicitly enabled.

Run the advisory gate locally:

```bash
bash scripts/verify-docs-prose-quality.sh
```

Run the mirror tests:

```bash
bash scripts/test-verify-docs-prose-quality.sh
```

Turn the same checker into a blocking gate:

```bash
DOCS_PROSE_ENFORCE=true bash scripts/verify-docs-prose-quality.sh
```

## Checked Pages

The gate reads `docs-catalog` metadata and checks human-facing page types:

- `project`
- `tutorial`
- `how-to`
- `concept`
- `operate`

It intentionally skips `reference` and `proof` pages. Those pages optimize for
exact lookup, tables, generated content, or evidence records, so beginner prose
rules would create noisy false positives. Any page with a generated or
do-not-edit marker is also skipped because source truth and drift gates own that
content.

## Style Contract

Human-facing docs should follow these rules:

- Keep one clear purpose per page. The advisory checker expects exactly one H1.
- Keep pages short enough for their job: concepts should stay tighter than
  tutorials, and task pages should split when they become long runbooks.
- Replace filler such as "seamless", "robust", "powerful", "leverage",
  "world-class", and similar launch prose with concrete task language.
- Keep tutorial and how-to prose readable. Long dense lines should be split into
  shorter steps or paragraphs.
- Put commands in language-tagged fences such as `bash`; do not include shell
  prompt prefixes like `$`.
- Put command output in a separate `text`, `json`, `yaml`, or similar fence when
  the result matters.

## Blocking Path

The tracked switch for making the gate blocking is
`DOCS_PROSE_ENFORCE=true`. Once the advisory baseline is clean enough, flip the
CI/local gate command to set that variable and change the gate from advisory to
blocking in `specs/ci-gates.v1.yaml`.
