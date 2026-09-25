# naming-glue-gate

## Purpose

Flags a newly introduced Go package directory whose name glues two or more
full words together where [naming.md](../../../docs/internal/naming.md) rule
3 requires nesting instead (`workloadinstance` should be
`workload/instance`). This is the mechanical file/directory-repeat gates'
blind spot: `scripts/verify-filename-stutter.sh` and
`tools/golangci-lint-dirgate`'s naming check both compare a name against its
*parent*; neither catches a directory whose own name is a glued compound
unrelated to its parent.

## Ownership boundary

This tool owns exactly one judgment call: is a newly added directory basename
a glued compound under rule 3. It does not check identifier stutter (rule 4;
see `revive`'s `exported` rule), file-vs-directory stutter (rule 2; see
`scripts/verify-filename-stutter.sh`), or file placement
(`tools/golangci-lint-dirgate`).

## Exported surface

- `run(args, stdout, stderr, classifier, runner, getenv) int` — the testable
  CLI implementation; `main` wires it to the real process.
- `NewDirectories(runner GitRunner, baseRef, headRef string, dirs []string) ([]Candidate, error)` —
  the newly introduced directory basenames between two git refs.
- `Classifier` / `DeepSeekClient` — the model call; `Classifier` is the
  interface `run` depends on so tests inject a fake.
- `Report`, `Finding`, `Verdict`, `Disposition` — the finding shape, matching
  AGENTS.md's severity/confidence/disposition/evidence vocabulary so this
  gate's JSON output composes with the rest of the review pipeline.

See `doc.go` for the full contract, including the blocking-mode design.

## Dependencies

- `git` (shelled out to via `os/exec`) to read each ref's directory tree.
- DeepSeek's chat-completions API (`deepseek-flash`) over `net/http`, no SDK.
- `docs/internal/naming.md` and `docs/internal/design/naming-remediation.md`
  as prompt grounding (quoted/summarized in `prompt.go`'s `systemPromptText`,
  not read live) — no separate word list is maintained for this gate.

## Telemetry

None. This is a one-shot CLI gate, not a long-running process; its output is
its own report (JSON on stdout, human summary on stderr).

## Gotchas / invariants

- This is the first non-deterministic gate in the repository: a DeepSeek
  model update can change a borderline verdict without any code change here.
  Keep it advisory (`-blocking=false`) in CI until its false-positive rate is
  measured against real PRs; pre-commit runs it with `-blocking=true` because
  a wrong call there costs one re-run, not a blocked merge.
- Missing `DEEPSEEK_API_KEY` or any classify error fails OPEN (exit 0): an
  infrastructure problem must never block a commit for an unrelated reason.
  Only `git` producing an unresolvable ref set fails closed (exit 2).
- Candidates are pre-filtered to lowercase, unseparated, >=8-character
  basenames before any network call. This is a shape filter, not a word
  list — it costs nothing to maintain and needs no updates as the repo grows.

## Related docs

- `docs/internal/naming.md`
- `docs/internal/design/naming-remediation.md`
