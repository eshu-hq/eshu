# Reducer directory gate reconciliation (#9164)

At `7d0cced37317c5d96a037fd7f30474dd05dc44d7`, the dirgate pre-push
check fails on `go/cmd/reducer` even with no file change in that directory.
The frozen baseline records 49 non-test Go files. Four reachable `+1` trailers
cover growth through 53 files; the base tree has 54. The gate reports one
unwaived file already on the base, so every later pull request inherits RED.

Commit `8545023c9fd6` (PR #9159) added `content_policy_gauge.go` without a
reachable `cmd/reducer` growth trailer. That file owns the reducer binary's
raw-rows gauge snapshot source, observable gauge registration, and observer
construction. `main_helpers.go` calls its registration function during
reducer startup. This wiring belongs with the other binary-local gauge
adapters in `cmd/reducer`; moving it to a shared package would move binary
composition across the existing package boundary.

The documented one-commit repair carries exactly `cmd/reducer +1` against
issue #9164. It changes neither Go source nor the 40-file cap or write-once
baselines. The trailer covers the existing unwaived unit; it grants no
allowance for later directory growth.

## Reproduction and acceptance

From the repair worktree, run:

```bash
ESHU_DIRGATE_BASE=7d0cced37317c5d96a037fd7f30474dd05dc44d7 \
  bash scripts/dev/precommit-go.sh dirgate-all
```

On the base, this exits 1 and reports `54 -> 54`, `needs +1`, and one RED
directory. On the repair commit, it must exit 0 with no RED directory.
