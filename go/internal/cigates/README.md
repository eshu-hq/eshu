# cigates

`cigates` is the typed core of the CI gate registry ([#4213](https://github.com/eshu-hq/eshu/issues/4213), drift [#4220](https://github.com/eshu-hq/eshu/issues/4220)). It provides the loader, selector, validator, drift checker, and glob matcher that back the `cmd/ci-gates` CLI and the `scripts/dev/select-gates.sh` / `scripts/dev/run-selected-gates.sh` wrappers.

It answers one question: **given a set of changed paths and a tier ceiling, which credential-free CI verifiers should run locally — and which are registered but CI-only or out of scope?**

## Files

| File | Purpose |
| --- | --- |
| `registry.go` | Types (`Registry`, `Gate`, `Tier`, `Category`, `Requirement`, `Local`, `CI`) and `Load` |
| `select.go` | `(*Registry).Select` — pure path-trigger matcher |
| `validate.go` | `(*Registry).Validate` — script (command + test_command) + workflow existence checks |
| `drift.go` | `DriftCheck` — `.pre-commit-config.yaml` / `.github/workflows` lockstep ([#4220](https://github.com/eshu-hq/eshu/issues/4220)), plus `ci.job` check-name resolution ([#5010](https://github.com/eshu-hq/eshu/issues/5010)) |
| `scriptworkflow.go` | `checkVerifyScriptWorkflowMatch`, called from `DriftCheck` — a gate whose `verify-*.sh` is executed by exactly one workflow must declare that workflow ([#5748](https://github.com/eshu-hq/eshu/issues/5748)) |
| `pathfilter.go` | `checkPathFilterCoverage`, called from `DriftCheck` — registry trigger vs. CI `dorny/paths-filter` glob cross-check ([#5855](https://github.com/eshu-hq/eshu/issues/5855)), resolving a gate's filter key through `append_gate` or through a job's `if:` on a paths-filter output ([#5546](https://github.com/eshu-hq/eshu/issues/5546)) |
| `glob.go` | `MatchGlob` — doublestar matcher, no external deps |

## Registry format

The registry lives at `specs/ci-gates.v1.yaml`. Each gate entry has a stable kebab-case id, a tier, a set of path-glob triggers, an optional local command, and a CI workflow reference. Gates whose `local` field is absent are CI-only and always require a non-empty `ci_only_reason`. Gates with a local command but no CI workflow are local-only and must carry a non-empty `local_only_reason` when used as replay proof gates.

## Selector semantics

`Select(changed, tier)` returns one `Selection` per gate in registry order. A gate is selected when:

1. Its tier is ≤ the requested tier ceiling.
2. At least one of its triggers matches at least one changed path.
3. Its `local` field is non-nil (CI-only gates are reported but never selected).

`ci-heavy` and `manual` tiers are never selected locally, regardless of the requested ceiling.

## Glob semantics

`MatchGlob` supports:
- `**` — matches zero or more path segments.
- `*` — matches any characters within a single segment (no `/` crossing).
- All other characters are literal.

Patterns with a leading `/` or trailing `/` never match.

## Drift semantics ([#4220](https://github.com/eshu-hq/eshu/issues/4220))

`DriftCheck(repoRoot, reg)` keeps the pre-commit-hook and workflow surfaces in lockstep with the registry. (Reconciling `make pre-pr`'s step set is [#4214](https://github.com/eshu-hq/eshu/issues/4214), which makes `pre-pr.sh` registry-driven.) It fails when:

1. a `local` pre-commit hook id is neither a gate's `hook_id` nor a declared `hygiene_hooks` entry;
2. a gate's `hook_id` is missing from `.pre-commit-config.yaml`, or its hook stage is inconsistent with the gate tier (pre-commit gate ⇒ stage `pre-commit`/default; pre-push gate ⇒ stage `pre-push`);
3. a `.github/workflows/*.yml` file is referenced by neither a gate `ci.workflow` nor `non_gate_workflows`, appears in both, or is a stale `non_gate_workflows` entry;
4. a gate's `ci.job` does not name a real check in its `ci.workflow` — a job `name:`, a job key, or an `append_gate` display, not the workflow title ([#5010](https://github.com/eshu-hq/eshu/issues/5010));
5. a gate's literal (non-glob) trigger is not matched by its CI workflow's `dorny/paths-filter` block, for a `ci.job` that resolves to a filter key either via `append_gate` in a matrix-dispatch workflow such as `static-contract-gates.yml` ([#5855](https://github.com/eshu-hq/eshu/issues/5855)) or via a job whose `if:` is gated on a paths-filter output, the shape `test.yml` / `security-scan.yml` / `mcp-schema-drift.yml` use ([#5546](https://github.com/eshu-hq/eshu/issues/5546));
6. a gate whose `scripts/verify-*.sh` is executed by exactly one workflow declares a different `ci.workflow` ([#5748](https://github.com/eshu-hq/eshu/issues/5748)). Only executable `run:` blocks count as executing the script — a `dorny/paths-filter` entry watches a path, it does not invoke it.

Checks 4, 5 and 6 skip rather than false-error when they cannot resolve an unambiguous mapping — an unparseable workflow, a `ci.job` with no matching append_gate call, (check 5 only) a glob-form trigger, or (check 6 only) a verify script that no workflow runs (CI legitimately uses a different entrypoint than the local gate) or that several run (no single owner). It is exposed via `ci-gates validate --drift` and needs no network, Docker, or credentials.

## Tests

```bash
cd go && go test ./internal/cigates/ -count=1
```
