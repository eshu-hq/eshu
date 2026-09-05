# Core Review Passes

## Pass 0: Scope, Ownership, And Diff Integrity

Before reviewing behavior, prove the review is pointed at the right work:

- base/head SHAs match the rebased final diff that will be pushed or merged;
- branch target is not `main` or `master`;
- touched surfaces map to their owning service or package boundary;
- scoped `AGENTS.md` rules and required skills have been loaded;
- changed files are limited to the intended issue/PR scope;
- no sibling PR rollback, unrelated deletion, generated-output churn, or
  accidental main-checkout mutation slipped in;
- root `AGENTS.md` and `CLAUDE.md` remain in lockstep when either changes;
- `.codex/skills` and `.claude/skills` discovery links exist for project
  skills that must be visible to both harnesses.

## Pass 1: Correctness And Truth

Check the changed contract against its actual consumers and requirements. For
agent instructions, trace what an agent would do, including authority and proof
boundaries. For runtime or capability changes, read the relevant sections in
[runtime-surfaces.md](runtime-surfaces.md).

## Pass 2: Performance And Storage/Query Shape

Identify whether runtime cost or backend behavior changes. When it does, apply
[runtime-surfaces.md](runtime-surfaces.md) and cite the required measurements.
For a change without runtime impact, explain that boundary briefly.

## Pass 3: Reliability, Concurrency, Security, Workflow Hygiene

Review for production operation and delivery safety:

- retries, leases, lock order/duration, transaction scope, idempotency,
  duplicate delivery, partial failure, rollback, recovery, and dead letters;
- startup/restart lock waits, schema/bootstrap behavior, stale generated
  artifacts, and rerun/idempotency of generators;
- private data, secrets, hostnames, IPs, credentials, internal URLs, employer
  identifiers, and AI attribution;
- docs, package docs, root `AGENTS.md`/`CLAUDE.md` lockstep, `.codex/skills`
  and `.claude/skills` discovery, hooks, pre-commit, pre-push, and GHA parity;
- follow-on validation needs when the PR cannot honestly prove a separate runtime,
  backend-version, cassette, full-corpus, or performance condition.

For CI or workflow changes, review the parity contract:

- every workflow-only behavior change has a local static mirror or test script;
- every prior GHA failure is either reproduced locally or documented as
  Actions-only with the nearest possible local guard;
- workflow tokens and permissions match the command path that uses them;
- path filters include the workflow, scripts, source, manifest-declared proof
  artifacts, fixtures, specs, and docs whose drift would make the workflow
  false-green;
- `gh pr checks --json` is captured after push before any readiness claim.

## Pass 4: Hostile Read And Abuse Cases

Read the diff as a future rushed agent, tired merger, or bot reviewer trying to
satisfy the letter while violating Eshu's intent. This pass is mandatory even
for docs-only and skill-only changes.

Ask and answer:

- What claim could this PR make too early?
- What proof could be deferred even though it is in scope?
- What wording allows a silent fallback, broad skip, or "follow-up" escape?
- What test could pass while the production subject is still broken?
- What generated artifact, cassette, snapshot, or registry could drift without
  this review catching it?
- What rebase, force-push, or stale-review sequence could make the reviewed diff
  differ from the pushed/merged diff?
- What would an operator be unable to diagnose at 3 AM from telemetry alone?
- What would NornicDB do if one label, index, constraint, or query shape differs
  from the happy path?
- Which changed input is not covered by a local or CI trigger, and what false
  green would that produce?
- Which advertised command, flag, report field, or artifact has not been
  executed exactly as users or CI will execute it?

Classify every hostile-read finding with one class:

| Class | Meaning |
| --- | --- |
| `wording-loophole` | Text permits behavior the author says they did not intend. |
| `scope-smuggling` | In-scope work is being treated as a follow-up or unrelated risk. |
| `evidence-overclaim` | The PR claims proof that the attached evidence does not provide. |
| `false-green-proof` | A test/gate can pass without exercising the production failure mode. |
| `stale-diff-risk` | Rebase, force-push, generated output, or unresolved review state can invalidate the review. |
| `runtime-proof-gap` | Required backend, scaled, full-corpus, or operator proof is missing. |
| `generated-drift-risk` | Generated artifacts, registries, cassettes, snapshots, or docs can drift from source truth. |
