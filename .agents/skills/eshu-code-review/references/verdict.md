# Findings And Verdict

## Finding Schema, Severity, And Disposition

Every finding must include:

- pass: `0`, `1`, `2`, `3`, or `4`;
- class: one hostile-read class or `correctness`, `performance`,
  `concurrency`, `security`, `docs`, `workflow`;
- severity: `P0`, `P1`, `P2`, or `P3`;
- confidence: `high`, `medium`, or `low`;
- disposition: one of the allowed dispositions below;
- file:line or exact evidence location;
- violated Eshu rule, skill, contract, or proof tier;
- concrete fix and verification that would close it.

Severity:

- **P0**: correctness, data loss, security/private-data leak, main break, or
  deadlock. Blocks commit, push, PR, and merge-readiness.
- **P1**: accuracy regression, missing idempotency/retry/ordering, silent
  failure, false-green test, missing runtime telemetry, unmeasured performance
  change on a hot path, or required proof tier not run. Blocks push/PR update
  until fixed and re-reviewed.
- **P2**: edge case, doc drift, genuine missing coverage, minor performance or
  naming issue. Fix inline by default; it blocks only when it contradicts a
  claim the PR makes or is cheap and in the same edit. Otherwise track it in a
  linked issue with the owner's agreement quoted in the PR, name it there with
  its severity-table category, and merge. Count fix-induced findings
  separately. Full bar and the unbounded loop it prevents:
  [merge-bar.md](merge-bar.md).
- **P3**: cosmetic and non-actionable. A typo, a formatting slip, a wording
  preference, a number in a narrative sentence that changes no decision. Fix it
  inline when it is a line, and never open an issue for one — a tracked typo is
  backlog, not progress. P3 never blocks, and a review returning only P3s is a
  clean review.

  A P3 takes disposition `fixed` or `not-a-bug-with-evidence` like any other
  finding. It may NOT take `deferred-to-linked-follow-up`, because that
  disposition means a linked issue exists and P3s do not get issues. A P3 left
  unfixed is recorded in the verdict's P3 list and named in the PR; that list
  is a record, not a disposition, and nothing downstream waits on it.

**P3 is decided by consequence, not by file type.** "It is markdown" is not a
severity. Documentation in this repo is the control plane: `AGENTS.md`, the
skills under `.agents/skills/`, and the hook docs are read and followed by
agents, so text that misdirects one is as expensive as code that misbehaves.
Prose stays at P2 or above whenever it is an instruction an agent follows, a
diagnostic procedure, an evidence table or claim a reviewer relies on, or
anywhere the documentation contradicts the code — that last one is already a
blocking condition and does not become weaker for being written in English.

Worked examples, all real findings on #6220, all documentation, none of them
P3: a diagnostic naming a stamp file the hook no longer writes, so following it
produces the wrong conclusion; an escape hatch documented as "not blocked" when
it blocks; a block message naming three variables when the guard probes five,
so an agent following it stays blocked; and an evidence table understating its
own test count by ten. A file-extension rule would have downgraded all four.

Disposition must be one of: `fixed`, `not-a-bug-with-evidence`,
`deferred-to-linked-follow-up`, or `blocked`. No finding may disappear between
review passes, and none may be re-derived: a finding restated across rounds is
the same finding. Reference the prior round and escalate it rather than
re-litigating it from scratch.

`fixed` is the default disposition. `deferred-to-linked-follow-up` is the
exception and must be justified: a defect found during review is usually still in
scope, especially in the same function, file, or evidence path. Defer only when
the fix needs a design decision the owner must make, would change unrelated
projected truth and needs its own proof, or blocks on credentials/infrastructure —
and confirm with the owner before opening a new issue. Filing an issue per finding
produces backlog sprawl rather than progress (see `eshu-issue-driver` defect handling).

## Hard Blocks

The verdict is `blocked` when any of these are true:

- base/head are not the final rebased diff to be pushed or merged;
- the full-picture gate is incomplete for any touched production surface;
- proof tier is missing, wrong, or not actually run for in-scope behavior;
- an applicable adversarial probe is missing or only checks a helper instead of the production subject;
- any P0 or P1 finding, or any blocking P2 ([merge-bar.md](merge-bar.md)), remains
  unresolved before preflight or push; a deferred P2 must be tracked (linked
  issue, owner agreement quoted in the PR), named with its severity-table
  category, and never silently dropped;
- generated artifacts or cassettes changed without source-of-truth proof;
- root `AGENTS.md` and `CLAUDE.md` drift;
- public text contains private data, credentials, internal identifiers, or AI attribution;
- review comments exist on the latest head and are unresolved;
- CI/check evidence does not match the changed surface.
- an existing PR lacks a current live check-rollup snapshot after its last push or rebase.
