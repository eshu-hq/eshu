# dirgate agent guidance

- The cap is derived from git. Do not add a committed digest ledger, allowlist
  of over-cap directories, or generated mirror. That design conflicted on every
  sibling PR (a digest is a pure function of the tree). A waiver lives in a
  commit-message trailer, never in a file. The only committed data are the two
  write-once baselines, `baseline-cap.tsv` and `baseline-naming.tsv`: never edit,
  shrink, or remove a row once they are on main, and regenerate them only
  with `dirgate baseline` at landing or after a rebase.
- The absolute term (`abs_needed`) is what stops two concurrent PRs from
  overshooting the cap unwaived. Do not drop it, and do not make push, schedule or
  workflow_dispatch report-only again.
- Keep exactly one implementation of the rule. `scripts/verify-dirgate.sh` and
  `scripts/dev/precommit-go.sh` only dispatch here.
- Never use `git merge-base` for the CI base. The CI base is `HEAD^1`: a PR that
  branched before main moved files out of a directory would otherwise read as a
  shrink and hide its own growth.
- An unresolvable base, an unreadable history, or a failed unshallow fetch exits
  2. Never turn it into a pass or a warning, and do not add a commit-count cliff.
- The `//nolint:dirgate` marker is the naming escape only. Do not let it waive
  the cap again.
- Pre-commit cannot see the pending commit message, so cap and trailer findings
  are NOTE there. Do not make them RED: that would make every first growth
  commit un-committable without `--no-verify`.
- A new gate behaviour needs a seeded RED case and a GREEN case in
  `scenario_test.go`, and a deliberate mutation of the rule that makes one of
  them fail. Tests build scratch git repositories under `t.TempDir()`; `TestMain`
  unsets the `GIT_*` environment so a hook cannot aim them at the real repo.
- A commit message in this repository must not contain a line that starts with
  the trailer key unless it is a real waiver; the gate reads every message in
  the range.
- The CI `dirgate` job must stay dedicated and depend on the trusted
  `pr-plan`. Keep the go-dir-gate `"**"` trigger so the gate is
  required for every pull request, including docs-only changes. The job runs
  when the trusted plan selects it; only validated reuse may omit it. This gate
  reads commit messages, so a code-path guard could let a stray trailer inflate
  `W` unchecked. `workflow_test.go` pins the job and planner wiring.
- Flags go before the mode. Only `pre-commit` takes arguments after it.
- The invariant is not airtight, and the slack is permanent: `W` sums every
  reachable commit and nothing subtracts from it, so a stray trailer on main counts
  for good. Do not claim a revert or shrink PR repairs `W`, or that the push run
  goes RED for an unused trailer once it is merged. The README and the docs page
  list the cases.
- go-dir-gate keeps the `"**"` trigger so the `dirgate` job is a required check on
  every change. `go/internal/cigates/dirgate_required_test.go` pins it.
