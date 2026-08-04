# cigates

`cigates` is the typed core of the CI gate registry ([#4213](https://github.com/eshu-hq/eshu/issues/4213), drift [#4220](https://github.com/eshu-hq/eshu/issues/4220)). It provides the loader, selector, validator, drift checker, and glob matcher that back the `cmd/ci-gates` CLI and the `scripts/dev/select-gates.sh` / `scripts/dev/run-selected-gates.sh` wrappers.

It answers two related questions:

1. Given changed paths and a tier ceiling, which credential-free verifiers run locally?
2. Given a pull request's changed paths, which blocking CI checks must pass before merge?

## Files

| File | Purpose |
| --- | --- |
| `registry.go` | Types (`Registry`, `Gate`, `Tier`, `Category`, `Requirement`, `Local`, `CI`) and `Load` |
| `select.go` | `(*Registry).Select` — pure path-trigger matcher |
| `required.go` | `(*Registry).RequiredGates` — every path-selected blocking CI job, including CI-only and heavy tiers |
| `validate.go` | `(*Registry).Validate` — script (command + test_command) + workflow existence checks |
| `drift.go` | `DriftCheck` — `.pre-commit-config.yaml` / `.github/workflows` lockstep ([#4220](https://github.com/eshu-hq/eshu/issues/4220)), plus `ci.job` check-name resolution ([#5010](https://github.com/eshu-hq/eshu/issues/5010)) |
| `requiredworkflow.go` | trusted required-status publisher validation: trigger, source workflow, permissions, checkout, and status command |
| `scriptworkflow.go` | `checkVerifyScriptWorkflowMatch`, called from `DriftCheck` — a gate whose `verify-*.sh` is executed by exactly one workflow must declare that workflow ([#5748](https://github.com/eshu-hq/eshu/issues/5748)) |
| `pathfilter.go` | `checkPathFilterCoverage`, called from `DriftCheck` — registry trigger vs. CI `dorny/paths-filter` glob cross-check ([#5855](https://github.com/eshu-hq/eshu/issues/5855)), resolving a gate's filter key through `append_gate` or through a job's `if:` on a paths-filter output ([#5546](https://github.com/eshu-hq/eshu/issues/5546)) |
| `trivyskipdirs.go` | `checkTrivySkipDirsParity`, called from `DriftCheck` — `scripts/dev/trivy-fs-local.sh`'s `skip_dirs` must equal `security-scan.yml`'s trivy-fs `skip-dirs`, compared as a set (rationale in `AGENTS.md`) |
| `glob.go` | `MatchGlob` — doublestar matcher, no external deps |

## Registry format

The registry lives at `specs/ci-gates.v1.yaml`. Each gate entry has a stable kebab-case id, a tier, a set of path-glob triggers, an optional local command, and a CI workflow reference. Gates whose `local` field is absent are CI-only and always require a non-empty `ci_only_reason`. Gates with a local command but no CI workflow are local-only and must carry a non-empty `local_only_reason` when used as replay proof gates.

The top-level `required_status_checks` manifest mirrors the contexts expected in
the effective `main` ruleset. Exactly one entry sets
`aggregates_blocking_gates: true` and names its trusted `source_workflow`.
Matrix jobs declare their concrete API-visible check names in `ci.check_names`;
the aggregate never relies on a prefix match.

## Selector semantics

`Select(changed, tier)` returns one `Selection` per gate in registry order. A gate is selected when:

1. Its tier is ≤ the requested tier ceiling.
2. At least one of its triggers matches at least one changed path.
3. Its `local` field is non-nil (CI-only gates are reported but never selected).

`ci-heavy` and `manual` tiers are never selected locally, regardless of the requested ceiling.

## Required-gate semantics

`RequiredGates(changed)` is separate from local `Select`. It includes every
matching `blocking: true` row regardless of tier or local availability,
deduplicates rows that share one workflow/job, and fails if a blocker has no CI
workflow/job mapping or shared rows disagree on concrete check names.

The required-status workflow runs from default-branch code after its declared
source workflow. It publishes a pending status on the exact pull-request head
before resolving the PR, and an always-run terminal failure prevents setup or
identity errors from leaving an old success in place. Success is published only
after every selected check reports `pass`. Failed, skipped, neutral, missing,
and timed-out checks fail closed. `DriftCheck` rejects an
aggregator that runs directly on pull-request code, lacks the declared source,
uses repository secrets, checks out a non-default ref, or lacks its minimal
read/status-write permissions.

The trusted boundary covers the aggregate policy, selector, and publisher. The
leaf workflows and the test code they execute remain ordinary reviewable pull
request changes. This prevents accidental policy drift and self-modification of
the aggregate, but it is not an immutable-CI boundary against a collaborator
who can both weaken a leaf check and approve or merge the same change.

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
6. a gate whose `scripts/verify-*.sh` is executed by exactly one workflow declares a different `ci.workflow` ([#5748](https://github.com/eshu-hq/eshu/issues/5748)). Only executable `run:` blocks count as executing the script — a `dorny/paths-filter` entry watches a path, it does not invoke it. Skipped, not reported, when no workflow runs the script (CI legitimately uses a different entrypoint) or when several do (no single owner);
7. `scripts/dev/trivy-fs-local.sh`'s `skip_dirs` disagrees, as a set, with `security-scan.yml`'s trivy-fs `skip-dirs` input. Whitespace is compared literally — trivy comma-splits `--skip-dirs` without trimming, so a padded entry is a different directory pattern to trivy and must not compare equal here;
8. the required-status manifest or its trusted aggregator workflow violates the source, trigger, permission, checkout, secret, or publisher-command contract.

Checks 4, 5, 6 and 7 skip rather than false-error when they cannot resolve an unambiguous mapping — an unparseable workflow, a `ci.job` with no matching append_gate call, (check 5 only) a glob-form trigger, (check 6 only) a verify script that no workflow runs (CI legitimately uses a different entrypoint than the local gate) or that several run (no single owner), or (check 7 only) neither trivy artifact being present, which is the shape of this package's synthetic drift fixtures rather than a real tree. It is exposed via `ci-gates validate --drift` and needs no network, Docker, or credentials.

## Tests

```bash
cd go && go test ./internal/cigates/ -count=1
```
