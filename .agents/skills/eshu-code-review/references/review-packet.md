# Review Packet

## Review Packet And Read-Only Contract

Before asking a separate reviewer or running self-review, build a bounded review
packet. Do not ask a reviewer to infer scope from chat history, branch names, or
the author's summary.

Use this packet shape:

```text
Review target:
- repo/worktree:
- branch:
- base SHA:
- head SHA:
- PR:
- no PR exists yet: yes|no
- review phase: preliminary|final
- preliminary review head and P0/P1/P2-blocking/P2-deferred/P3 counts:
- pre-pr command and result:
- post-preflight head and clean-status result:

Intent:
- issue/PR requirement:
- acceptance criteria:
- out of scope:

Diff:
- commands to inspect: git diff --stat <base>..<head>; git diff <base>..<head>
- files changed:
- generated artifacts changed:

Eshu surfaces:
- packages/services:
- API/MCP/CLI contracts:
- graph/reducer/query/cassette/golden surfaces:
- workflow/docs/agent-rule surfaces:
- system impact map:
- production subject and invariants:

Proof:
- selected proof tier:
- commands actually run:
- commands not run and why:
- performance or observability evidence:
- adversarial probes run:

GitHub truth:
- review-thread API snapshot or no-PR disposition:
- check-rollup snapshot or no-PR disposition:
- mergeability/base-drift snapshot:
```

Reviewer mode is read-only until the verdict is written. The reviewer may run
read-only commands such as `git diff`, `git show`, `rg`, `gh pr view`, `gh pr
checks`, GraphQL/API review-thread queries, and CI log fetches. The reviewer
must not edit files, stage, commit, rebase, push, resolve threads, rerun
generators, or mutate PR state while forming the verdict. Fixes happen after the
verdict, then the review repeats on the new base/head.

If delegating to a separate reviewer, include the review packet verbatim plus
this instruction: "Evaluate the diff against its requirements and evidence. Return findings first, with pass, class,
severity, confidence, disposition, file:line, violated Eshu rule or proof tier,
and concrete verification. Do not approve from intent, summary, or partial
evidence."
