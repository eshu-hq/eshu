---
name: eshu-executor-preflight
description: "Use as the last step before an executor hands a branch to a reviewer: shape-checks the PR body, re-measures its claims at HEAD, and clears the registry traps that static gates catch late."
---

# Eshu Executor Preflight

Run this on the final commit, before you hand a branch to a reviewer. It catches
the text and registry defects that otherwise cost one review round each.

It does not replace review, `make pre-push`, or promotion. The coordinator owns
those. You are a leaf agent: do not dispatch other agents.

## Steps

1. **Bind the run.** Commit first. Record `git rev-parse HEAD`. If a PR body or
   draft exists, record its `sha256` too. Every result below belongs to that
   commit. A new commit voids the results; restart at this step.

2. **Check the shape.** If a body or draft exists, run
   `bash .agents/skills/eshu-publish/scripts/check-shape.sh <body-file>`. Fix
   every `FAIL` line. Done when the exit code is 0. If no body exists, record
   this step as `NOT_CHECKED` with that reason.

3. **Re-measure the claims a command can settle.** List each count, SHA, file
   list, and link target in the PR body and in the docs you changed. Run a
   command at this HEAD for each one. Write the rows as
   `claim | command | measured | match`. If a row does not match, fix the text.
   If the text is right, fix the code. Done when every row matches.

   Leave a timing or baseline-bound claim as it is. Do not re-run it at this
   HEAD, because that destroys its provenance. Check that it names its run id
   or baseline SHA, and see
   [timing-proof-rules.md](../../../docs/internal/timing-proof-rules.md).

4. **Clear the registry traps.** Check each row that your diff touches:

   | The diff adds | Check |
   |---|---|
   | a `.go` file, tests included | `bash scripts/verify-license-header.sh`. Fix with `bash scripts/add-license-header.sh` inside the worktree. |
   | a `*_live_test.go` file | A classified row in `specs/live-tests.v1.yaml`, then `bash scripts/verify-live-tests-ledger.sh`. |
   | an `ESHU_*` environment variable | An `Entry` in `go/internal/envregistry/entries.go`. Run `bash scripts/generate-env-registry-doc.sh`, then `scripts/verify-env-registry-doc.sh` and `scripts/verify-docs-cli-env-refs.sh`. |
   | a new test file | Run `rg -n -e TODO -e placeholder <test-files>`. A hit is real when it is a stub type, an empty assertion, or a TODO that skips behavior. SQL placeholders and ordinary prose are not hits. |

   In `specs/ci-gates.v1.yaml`, `env-registry-doc` and `live-backend-tests` (the
   owner of the ledger validator) have no `local.pre_push: floor`. `make
   pre-push` defers them, so this table is their first local run.

5. **Confirm the role gates.** Rerun the registry-selected static gates your role
   names if you committed after their last run. Report each as `PASS`, `FAIL`, or
   `NOT_CHECKED`. Do not run `make pre-push` or `make pre-pr`.

6. **Check test sensitivity.** Skip this step for a test whose red-first run
   already showed the failure. Otherwise mutate the production assertion and
   watch the test fail. Restore the file with `git restore <file>`, and confirm
   `git status --short` is empty before you go on.
   [golang-engineering](../golang-engineering/SKILL.md) holds the rule.

7. **Repeat until clean.** Any fix is a new commit and restarts at step 1. Stop
   when one full pass changes nothing. After six passes, stop and report the
   remaining blockers instead of looping.

## Handoff

Include the head line from
[Messages name their head](../eshu-session-lifecycle/SKILL.md#messages-name-their-head).
Your report must contain:

- `HEAD`, the body `sha256` when a body exists, and the number of passes
- the claim table, or the path to it
- each gate and registry check with `PASS`, `FAIL`, or `NOT_CHECKED`
- the reason for every `NOT_CHECKED`

## Mistakes

| Mistake | What it costs |
|---|---|
| Running the preflight, then committing a "small" fix | Results that belong to a HEAD no reviewer will see |
| Copying a figure from the last run into the body | A stale claim that a reviewer re-measures and blocks on |
| Re-running a baseline-bound timing at this HEAD | A number that no longer matches its named baseline |
| Treating a `make pre-push` exit 0 as proof the ledger and env-registry checks passed | The first run of those checks happens in CI |
| Running `make pre-push` yourself to feel safe | One more floor run per branch: changed-package tests, race tests, and lint |
