# 6778 — filename destutter: correlation/rules, cmd/reducer, ask/facet

Issue: #6778. Branch `refactor/6778-filename-destutter`, base `origin/main`
a49323c7b57e9f5083d569525a582bd8c8e4b9d3.

Twenty-one files whose stem repeated their own leaf directory were renamed, dropping
the repeated word. This note exists because `go/cmd/reducer/executor_adapters.go` is a
hot path under `scripts/verify-performance-evidence.sh`, so the gate requires tracked
evidence in the repo rather than only in the PR body.

## What changed

- `go/internal/correlation/rules/`: the `_rules` suffix dropped from fourteen
  rule-pack files and their two test files.
- `go/cmd/reducer/`: the `reducer_` prefix dropped from four files, one production
  (`executor_adapters.go`) and three test.
- `go/internal/ask/facet/`: `facet_match.go` -> `match.go`.
- Reference updates only: `specs/ci-gates.v1.yaml`,
  `.github/workflows/ifa-determinism-gate.yml`,
  `scripts/lib/ifa_live_gate_selector_cases.sh`,
  `scripts/moved-file-refs-allowlist.txt`, four scoped `AGENTS.md` files, and two Go
  doc comments.

## Why no measurement is reported

No-Regression Evidence: none of the twenty-one renames changed a byte of any file.
`git diff --find-renames --numstat a49323c7b...HEAD` reports `0 0` for all twenty-one,
and `git diff --find-renames --name-status` classifies all twenty-one `R100`. The
remaining ten modified files add and delete a handful of lines, none of them an
executable Go statement: they are YAML trigger rows, shell case strings, Markdown
pointers, an allowlist, and two comments.

There is therefore no before/after measurement to report, and inventing one would be
dishonest. The claim this note makes is narrower and checkable: the compiled behaviour
of `go/cmd/reducer` is unchanged because its source bytes are unchanged. The proof is:

- `go build`, `go vet` and `go test -count=1` over
  `./internal/correlation/rules/... ./cmd/reducer/... ./internal/ask/facet/...` all
  exit 0.
- `go test -list '.*'` over those same three trees yields 186 test names on the base
  commit and the same 186 on this head. The sorted sets diff empty. A control that
  drops one name from the after-list makes the same comparison report a difference,
  so the empty diff is a real pass and not a broken command. The raw unsorted lists
  differ only in emission order, because `go test -list` walks files in filename order
  and the renames reorder them.

No Cypher, graph write, queue, lease, batching, reducer projection or materialization
path is touched. No backend, corpus, profile, topology or storage state is involved,
so there is no baseline manifest to compare against and no queue or row count to
report.

## Observability

No-Observability-Change: no metric, span, log key, status field or pprof surface is
added, removed or renamed. `go/cmd/reducer/executor_adapters.go` keeps its contents
byte for byte, so every instrument it registers keeps its name and labels. Operators
see no difference; nothing in a dashboard, alert or runbook needs updating.

## Gate evidence

Two blocking gates were the real risk here, and both were proven with a mutation
control rather than assumed:

`ci-gates` registry — `specs/ci-gates.v1.yaml` named the pre-rename path as a trigger
for `ifa-determinism` and `ifa-fault-injection`. Reverting only those two rows in a
throwaway worktree makes `scripts/verify-ci-gates-registry.sh` exit 1 with
"trigger ... does not exist — a stale trigger silently stops selecting this gate
instead of failing loud"; it exits 0 on this head.

Workflow path filter — `.github/workflows/ifa-determinism-gate.yml` holds the single
`on.paths` list that starts both of those gates, and the registry validator cannot see
workflow files. Reverting only that one line makes
`scripts/test-verify-ifa-determinism.sh` exit 1 with "workflow does not retrigger the
live matrices for IFA proof input: go/cmd/reducer/executor_adapters.go"; it exits 0 on
this head. Both controls were run before the rebase onto a49323c7b, on the identical
one-line reverts; the rebase changed no byte of either file. That mirror needs bash 4 or newer — under macOS bash 3.2 it dies with
`sql_relationships: unbound variable`, which reads like a gate failure but is an
environment mismatch.

Also green on this head: `verify-filename-stutter.sh` in `--range` and merge-base
modes, and over all 132 tracked files in the three directories;
`verify-moved-file-refs.sh`; `verify-docs-refs.sh`; `verify-package-docs.sh`;
`verify-markdown-line-cap.sh`; and `git diff --check`.

The `verify-filename-stutter.sh` pass is not vacuous: the same gate exits 1 on the two
pre-rename names and on a planted `go/internal/correlation/rules/planted_rules.go`.
