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
| `trivyskipdirs.go` | `checkTrivySkipDirsParity`, called from `DriftCheck` — `scripts/lib/trivy-skip-dirs.sh` must be provably wired to read `specs/trivy-skip-dirs.txt`, the single authoritative skip-dirs list, and `scripts/dev/trivy-fs-local.sh` must be provably wired to `source` that shared helper, call the function it defines, and `set` pipefail rather than reading the specs file or re-deriving the list itself (rationale in `AGENTS.md`) |
| `trivyskipdirs_specentries.go` | `trivySkipDirsSpecEntries` — parses and validates `specs/trivy-skip-dirs.txt`'s entries (split out of `trivyskipdirs.go` to stay under the 500-line-per-file limit) |
| `trivyskipdirs_ci.go` | `checkCIWorkflowSkipDirsFromHelper`, called from `checkTrivySkipDirsParity` — `security-scan.yml`'s trivy-fs job's producer step must be provably wired the same way: `source` the shared helper and call the function it defines, not merely mention its path ([#5927](https://github.com/eshu-hq/eshu/issues/5927)); does NOT verify the step writes the exact `$GITHUB_OUTPUT` key the trivy-action step references (a line-regex assertion of that, `trivyOutputAssignmentPattern`, was attempted and retired after five review rounds — see `AGENTS.md`) |
| `scripttrigger.go` | `checkScriptTriggerCoverage`, called from `DriftCheck` — a gate's own `local.command` / `local.test_command` script, and every `scripts/` file those source, must be matched by one of that gate's triggers ([#5762](https://github.com/eshu-hq/eshu/issues/5762)) |
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
7. the specs file, the shared derivation helper `scripts/lib/trivy-skip-dirs.sh`, the local script `scripts/dev/trivy-fs-local.sh`, and `security-scan.yml`'s `trivy-fs` job are not all provably wired together: the specs file must exist, be non-empty, have no duplicate entries, no entry with leading/trailing whitespace, no entry containing a comma (the delimiter the shared derivation's `paste -sd, -` joins entries with, so an embedded comma would smuggle extra entries past every other per-entry check — single-source review F1), no entry containing a `#` (only a WHOLE-LINE comment is supported, never a trailing one — round-2 review P2-2), no entry containing a glob metacharacter (`*`, `?`, `[`, `]`, `{`, `}`), no entry that normalizes (`filepath.Clean`, the way trivy's own `CleanSkipPaths` does) to the catch-all `.` that would disable the scan's coverage entirely (round-2 review P1-1, closing the class after `**`, `**/*`, `./`, `.//`, `./.`, and `?` all defeated the original entry-specific literal check against real trivy 0.72.0), no entry that normalizes to `""` — a DIFFERENT, narrower reason than the catch-all check states: proven against real trivy 0.72.0 and against trivy's own source, an empty-normalizing entry (e.g. `/`) is not dropped by `CleanSkipPaths`, but `SkipPath`'s `doublestar.Match` against an empty pattern never matches a real repo-relative path, so it disables nothing and is dead weight, not a catch-all (#5927 round-7 review F1), and no entry that normalizes to `..` or a path escaping the repository root via a leading `../` — a DIFFERENT, narrower reason than the catch-all check: proven against real trivy 0.72.0, `..` alone does not disable coverage, it is meaningless because it escapes the root the list is defined relative to (round-3 review P2-1 closed the class of `../`-prefixed escapes, not only the bare `..` literal); and no entry that is itself an absolute path (a leading `/`), checked on the raw entry rather than the normalized value, since `filepath.Clean` plus the trimmed leading `/` the catch-all and escape-root checks above use would otherwise strip it before either of those questions is asked; the helper must reference the specs file's path exactly once (over whole-line-comment-stripped content, so a comment mentioning the path elsewhere does not miscount); the local script must both `source` the HELPER exactly once, call the function it defines, `trivy_skip_dirs_csv`, exactly once (same comment-stripping rule, not merely mention the helper's path once), and `set` pipefail (round-2 review P2-3, the local-script half of the failure-mode-parity requirement `shell: bash` enforces CI-side); and the `trivy-fs` job's `aquasecurity/trivy-action` step's `skip-dirs` input must be EXACTLY a `${{ steps.<id>.outputs.<name> }}` expression (read job-scoped via YAML, never regexed file-wide) whose named step is held to that same source-and-call proof, not a bare mention, in its `run:` block, and declares `shell: bash` ([#5925](https://github.com/eshu-hq/eshu/issues/5925) redesign, after three review rounds found the string-literal-comparison predecessor design unsound, then a follow-up (F2) replaced two independently-maintained derivation pipelines on the local and CI sides with the one shared helper both must now invoke; [#5927](https://github.com/eshu-hq/eshu/issues/5927) then closed the false green where merely mentioning the helper's path — in a string literal or an echo — read as "invokes the helper" while a hard-coded value governed the emitted skip-dirs, by requiring proof of both a `source` line and a `trivy_skip_dirs_csv` call). A further #5927 attempt to also assert the step's `run:` block WRITES the exact referenced output name into `$GITHUB_OUTPUT`, via an `echo`/`printf` assignment line (`trivyOutputAssignmentPattern`), was retired after five review rounds each closed one instance of the same class without closing it: round 5 proved the pattern's positive-alphabet argument only holds in unquoted shell text, while every value it was asked to match lives inside a quoted argument, so deciding which shell word an `=` belongs to needs the same mid-line bash parsing this redesign exists to avoid (full rationale in `AGENTS.md`). This check therefore proves the producer step is wired to the helper and that the trivy-action step references that step's output — NOT that the producer writes that exact output key; a key mismatch yields an empty `skip-dirs` value, so trivy scans everything and the job fails loudly on the intentionally-vulnerable fixtures, costing diagnosis time rather than scan coverage;
8. any `scripts/` token in a gate's own `local.command` / `local.test_command` — every one in a chained command, not only the first — or a `scripts/` file one of them sources, at any depth, is matched by none of that gate's triggers ([#5762](https://github.com/eshu-hq/eshu/issues/5762)). Without it, a PR touching only `scripts/verify-openapi.sh` got `SKIPPED openapi-surface — no trigger matched changed paths` from `make pre-pr` and first failed in CI. An inline toolchain command with no `scripts/` token (`cd go && go test …`) is skipped, since it names no file whose edit could go unselected;
9. the required-status manifest or its trusted aggregator workflow violates the source, trigger, permission, checkout, secret, or publisher-command contract.

Checks 4, 5 and 6 skip rather than false-error when they cannot resolve an unambiguous mapping — an unparseable workflow, a `ci.job` with no matching append_gate call, (check 5 only) a glob-form trigger, or (check 6 only) a verify script that no workflow runs (CI legitimately uses a different entrypoint than the local gate) or that several run (no single owner). Check 7 does not share that escape hatch: its only skip condition is none of the FOUR trivy artifacts (specs file, helper, local script, CI workflow) being present, which is the shape of this package's synthetic drift fixtures rather than a real tree. Any subset present, an unparseable `security-scan.yml`, a missing or ambiguous `trivy-fs` job or trivy-action step, a hard-coded or appended skip-dirs value, or a referenced step that does not both source and call the helper all fail loudly instead. It is exposed via `ci-gates validate --drift` and needs no network, Docker, or credentials.

## Tests

```bash
cd go && go test ./internal/cigates/ -count=1
```
