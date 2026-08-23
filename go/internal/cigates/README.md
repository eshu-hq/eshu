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
| `requiredworkflow_concurrency.go` | serialized per-head publisher concurrency contract |
| `requiredworkflow_triggers.go` | trusted workflow trigger and source-workflow validation |
| `scriptworkflow.go` | `checkVerifyScriptWorkflowMatch`, called from `DriftCheck` — a gate whose `verify-*.sh` is executed by exactly one workflow must declare that workflow ([#5748](https://github.com/eshu-hq/eshu/issues/5748)) |
| `pathfilter.go` | `checkPathFilterCoverage`, called from `DriftCheck` — registry trigger vs. CI `dorny/paths-filter` glob cross-check ([#5855](https://github.com/eshu-hq/eshu/issues/5855)), resolving a gate's filter key through `append_gate` or through a job's `if:` on a paths-filter output ([#5546](https://github.com/eshu-hq/eshu/issues/5546)); also exports `DornyFilters` so a gate can assert its own workflow filter covers what its verdict depends on (`internal/evidencecontinuity`'s trigger self-check, [#6131](https://github.com/eshu-hq/eshu/issues/6131)) |
| `trivyskipdirs.go` | `checkTrivySkipDirsParity`, called from `DriftCheck` — `scripts/lib/trivy-skip-dirs.sh` must be provably wired to read `specs/trivy-skip-dirs.txt`, the single authoritative skip-dirs list, and `scripts/dev/trivy-fs-local.sh` must be provably wired to `source` that shared helper, call the function it defines, and `set` pipefail rather than reading the specs file or re-deriving the list itself (rationale in `AGENTS.md`) |
| `trivyskipdirs_specentries.go` | `trivySkipDirsSpecEntries` — parses and validates `specs/trivy-skip-dirs.txt`'s entries (split out of `trivyskipdirs.go` to stay under the 500-line-per-file limit) |
| `trivyskipdirs_ci.go` | `checkCIWorkflowSkipDirsFromHelper`, called from `checkTrivySkipDirsParity` — `security-scan.yml`'s trivy-fs job's producer step must be provably wired the same way: `source` the shared helper and call the function it defines, not merely mention its path ([#5927](https://github.com/eshu-hq/eshu/issues/5927)); does NOT verify the step writes the exact `$GITHUB_OUTPUT` key the trivy-action step references (a line-regex assertion of that, `trivyOutputAssignmentPattern`, was attempted and retired after five review rounds — see `AGENTS.md`) |
| `scripttrigger.go` | `checkScriptTriggerCoverage`, called from `DriftCheck` — a gate's own `local.command` / `local.test_command` script, and every `scripts/` file those source, must be matched by one of that gate's triggers ([#5762](https://github.com/eshu-hq/eshu/issues/5762)) |
| `gopkgtrigger.go` | `checkGoPackageTriggerCoverage`, called from `DriftCheck` — the Go-language half of the same rule: a gate's own `local.command` / `local.test_command` Go packages must be matched by one of that gate's triggers ([#5873](https://github.com/eshu-hq/eshu/issues/5873)) |
| `glob.go` | `MatchGlob` — doublestar matcher, no external deps |

## Registry format

The registry lives at `specs/ci-gates.v1.yaml`. Each gate entry has a stable kebab-case id, a tier, a set of path-glob triggers, an optional local command, an optional local self-test command, and a CI workflow reference. The CLI runs a selected gate's command followed by its distinct, non-empty self-test command; byte-identical pairs run once. Gates whose `local` field is absent are CI-only and always require a non-empty `ci_only_reason`. Gates with a local command but no CI workflow are local-only and must carry a non-empty `local_only_reason` when used as replay proof gates.

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
source workflow. Per-head concurrency keeps one aggregate running and retains
only the latest pending run without cancelling the active status writer. The
publisher posts pending on the exact pull-request head as its first step,
before checkout or setup. The running aggregate therefore reaches a real
success or failure, and the retained run invalidates that result before it
recomputes.

The terminal publisher does not start when cancellation is already observed,
so cancellation alone is not converted into failure. A manual cancellation
after the first step leaves pending, which fails closed. GitHub cannot run the
first step after a cancellation that happens before runner allocation, and the
commit-status API has no compare-and-swap or generation token; the workflow
does not claim atomic fencing for that operator/API boundary. Success is
published only after every selected check reports `pass`. Failed, neutral,
missing, and timed-out checks fail closed, and so does a check GitHub skipped
for its own reasons. A check that never produced a verdict is the carve-out
([#6189](https://github.com/eshu-hq/eshu/issues/6189)) -- a cancelled check, a
stale one, one skipped because the run that owned the job was cancelled, or one
missing outright because that run was cancelled before the job was created.
That is infrastructure state, not a gate result, so the aggregate publishes
`error` naming the re-run instead of claiming a gate failed, and
`validateCancelledArm` rejects a publisher whose cancelled-gate arm is missing,
maps that outcome to `state=failure`, or publishes anything other than
`state=error`. It locates the arm structurally -- a line inside the
`AGGREGATE_CODE` case block that begins with `13)`, comment lines skipped --
and reads the arm's **effective** assignment, the last `state=` that survives,
because bash runs an arm top to bottom. A substring search finds the marker in
ordinary prose such as "the cancelled-gate outcome (13)", and a substring
assertion accepts `state=error; state=success`; each of those left the arm
deletable or invertible with the validator still green.

The arm is read in two layers, both deliberately **narrow** rather than
general. `requiredworkflow_shell.go` is the word-level reader: it accepts a
documented grammar -- statements separated by `;`, `&&` and `||`, words built
from unquoted runs and `'`/`"` segments, `#` starting a comment at a word
boundary, `;;` ending the arm -- and returns an error for anything else,
including backslash escapes, command substitution, subshells, and `$'…'`
ANSI-C quoting, whose value is not the text between its quotes.
`requiredworkflow_arms.go` is the statement-level reader on top of it: an arm
may hold a run of assignments (`description=x state=error`, which is how bash
makes an assignment persist) or a call to `echo` or `exit`, and nothing else.
Both matter for the same reason: a `state=` token inside a quoted description
is text and must not be read as the arm's assignment, and an arm built from
shell the reader does not model has to fail the gate loudly instead of being
guessed at. [#6194](https://github.com/eshu-hq/eshu/issues/6194) is why -- nine
review rounds went into growing a textual model of bash one character class at
a time without ever closing it. The statement layer is a whitelist for that
same reason: `export state=failure`, `if true; then state=failure; fi` and
`state=$'failure'` all set `state` in bash, and the earlier rule -- read the
leading assignment words, stop at the first word that is not one -- read all
three as assigning nothing at all, which is a still-running arm publishing
`failure` with the gate green.

Two more checks cover the consuming end, because pinning the argument alone did
not. The `gh api` call must post `-f state="${state}"` (and, when it sends one,
`-f description="${description}"`) rather than a literal: a hard-coded
`-f state=success` publishes the same status for every outcome, a genuinely
failed gate included. And nothing between the case block's `esac` and that call
may assign `state` or `description`: a bare `state=success` there satisfies
every arm assertion AND the literal check above, overwrites the arm's verdict
on the way to the publish, and passes the step's own closing
`[[ "${state}" == "success" ]]` so the job goes green too. The load-bearing
guard for that second one is not static at all -- `publishedRequiredStatus` in
`cmd/ci-gates` runs the publisher from its `PENDING_OUTCOME` guard through the
line before `gh api` under real bash, so the per-code tests observe whatever
value actually reaches the publish. The static check here is its mirror, so the
two have to be defeated together.

`error` still blocks the merge, so the carve-out changes what
the status says, not whether it holds the PR. The classification itself lives
in `cmd/ci-gates`; this package only holds the publisher to the arm it implies.
`DriftCheck` rejects an
aggregator that runs directly on pull-request code, lacks serialized per-head
concurrency and first-step invalidation, lacks the declared source, uses
repository secrets, checks out a non-default ref, or lacks its minimal
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
8. any `scripts/` token in a gate's own `local.command` / `local.test_command` — every one in a chained command, not only the first — or a `scripts/` file one of them sources, at any depth, is matched by none of that gate's triggers ([#5762](https://github.com/eshu-hq/eshu/issues/5762)). Without it, a PR touching only `scripts/verify-openapi.sh` got `SKIPPED openapi-surface — no trigger matched changed paths` from `make pre-pr` and first failed in CI. Skipped, not reported: a command with no `scripts/` token once tokenized (nothing to check); a command starting with `cd ` unconditionally, before tokenizing runs at all — today no such command in the registry carries a `scripts/` token either, but that is a coincidence the check does not enforce; and sourcing that only a variable or computed path names (`. "${script_dir}/lib/foo.sh"`), which the sourcing scan cannot resolve at any depth — see `checkScriptTriggerCoverage`'s doc comment in `scripttrigger.go` for the full list, the measured extent of that last gap, and why no gate is false-green from it today;
9. the required-status manifest or its trusted aggregator workflow violates the source, trigger, permission, checkout, secret, or publisher-command contract;
10. a Go package a gate's own `local.command` / `local.test_command` builds, runs, or tests is matched by none of that gate's triggers ([#5873](https://github.com/eshu-hq/eshu/issues/5873)). This is check 8's rule for the local gates implemented as a Go package (`go run ./cmd/x`, `go test ./internal/y`) instead of a `scripts/` file — check 8 cannot see any of them, because it only derives `scripts/`-prefixed tokens. Two gates were in that state when the check landed: `product-claim-ledger` ran `go run ./cmd/capability-inventory` while triggering only on `specs/*.yaml`, and `ifa-materialized-edge-coverage` ran package-level `go test` on `./internal/ifa` and `./internal/reducer` while triggering on individual files inside them. A recursive spec (`./...`) demands a trigger that reaches nested files too, since that is the file set the command compiles. Coverage must be directory-wide: a `go/internal/x/*.go` trigger does NOT satisfy a package-level command, because a package's compiled output depends on more than its Go sources (`go/internal/capabilitycatalog` embeds `data/catalog.generated.json`). A subcommand naming no package, or a bare `.`, resolves to the working directory rather than being skipped. Skipped, not reported: `go list` packages (listing a package neither builds nor runs it, so a change inside cannot alter the verdict); a package argument with no preceding `go build`/`generate`/`run`/`test`/`vet`; one resolving outside the repository; and the repository root itself, where the property is vacuous. Both `cd` and `go -C <dir>` are honoured wherever they appear, and subshell scope is modelled so a `cd` inside `( )` does not outlive it — see `checkGoPackageTriggerCoverage`'s doc comment in `gopkgtrigger.go` for why leaking it was a false green rather than the conservative direction it looked like, and for the silent narrowings that probing the parser found after it was written.

Checks 4, 5 and 6 skip rather than false-error when they cannot resolve an unambiguous mapping — an unparseable workflow, a `ci.job` with no matching append_gate call, (check 5 only) a glob-form trigger, or (check 6 only) a verify script that no workflow runs (CI legitimately uses a different entrypoint than the local gate) or that several run (no single owner). Check 7 does not share that escape hatch: its only skip condition is none of the FOUR trivy artifacts (specs file, helper, local script, CI workflow) being present, which is the shape of this package's synthetic drift fixtures rather than a real tree. Any subset present, an unparseable `security-scan.yml`, a missing or ambiguous `trivy-fs` job or trivy-action step, a hard-coded or appended skip-dirs value, or a referenced step that does not both source and call the helper all fail loudly instead. It is exposed via `ci-gates validate --drift` and needs no network, Docker, or credentials.

## Tests

```bash
cd go && go test ./internal/cigates/ -count=1
```
