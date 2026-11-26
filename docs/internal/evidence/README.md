# Internal Evidence Records

This directory holds a small set of durable, live-environment validation records
(for example, live-cluster measurements backing a capability claim or a major
storage decision). It is **not** a per-PR drop box.

## Where proof belongs instead

- **Routine PR verification and "nothing regressed" checks:** put them in the PR
  description or point at a GitHub Actions workflow artifact.
- **Durable code, query, or concurrency invariants:** document them next to the
  code in the owning package doc (for example, `go/internal/<pkg>/README.md` or
  `gotchas-and-invariants.md`) or in an ADR under `docs/public/adrs/`.
- **Raw `EXPLAIN` plans, timing logs, and scope dumps:** keep them out of git.
  During local review, pass them through an untracked scratch directory; in CI,
  upload them as a workflow artifact.

## Rules for files in this directory

`scripts/verify-evidence-hygiene.sh` enforces these four rules on commit and in CI:

1. **Markdown only.** Never commit `.txt`, `.sql`, `.tsv`, `.log`, `.json`,
   `.sh`, `.py`, or `*-raw/` dump directories here.
2. **150-line cap on new notes.** A teammate or future agent should be able to
   read the record in under a minute.
3. **Required `## Summary` section.** Open with plain English stating what was
   tested, what happened, and what constraint now holds.
4. **No links to untracked local state.** Never link to `.claude/`, `.codex/`,
   `/tmp/`, or `/Users/` paths.

**Writing standard (enforced in review):** avoid internal session or workflow
shorthand ("Lane-1b", "F2 branch 1", "arbiter delta", "RED-2"); explain domain
terms in plain words following `docs/internal/writing-for-agents.md`.

## Template

```markdown
# <Short, plain-English title>

Refs #<issue>. Date: YYYY-MM-DD.

## Summary

One short paragraph answering three questions: what question we tested, what
the measurement showed, and what decision or constraint now holds.

## Environment

- Target: <backend, version, dataset size>
- Build: <commit SHA or image tag>
- Method: <one-sentence description of how the test ran>

## Results

| Scenario | Before | After | Outcome |
| --- | --- | --- | --- |
| <case> | <baseline with units> | <new value with units> | <pass/fail> |

## Constraints and open risks

- <Invariant future changes must preserve>
- <Known limitation or follow-up issue, if any>
```

## Pruning policy

When a parent epic closes, promote any surviving architectural or query
invariant into the owning package doc or ADR and delete superseded issue-level
evidence files from this directory.
