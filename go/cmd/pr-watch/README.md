# pr-watch

One committed watcher for the pull requests a session owns (#9095, part of
#9092). It replaces the hand-rolled poll loops that missed review threads,
trusted a single zero-pending read, ran under the wrong account, or exited
silently.

```bash
cd go && go run ./cmd/pr-watch -dir .. 9101 9102   # foreground
```

Run the built binary in the background under the harness Monitor so each
output line wakes the agent:

```bash
cd go && go build -o /tmp/pr-watch ./cmd/pr-watch
/tmp/pr-watch -dir /path/to/worktree 9101 9102
```

Events, flags, and account rules are in `doc.go`. In short:

| Event | Meaning | What to do |
|---|---|---|
| `CI-FAIL` | A check ended red; the line carries the job and log tail | Reproduce the failing gate, fix, push |
| `CONFLICT` | `CONFLICTING`, `BEHIND`, or persistent `UNKNOWN`, with a test merge | `recipe=4a`, `count-choice-lines`, `rebase`, or `unknown`; see below |
| `NEW-THREAD` / `NEW-REVIEW` / `NEW-COMMENT` | Feedback from any author | Fix first, then reply and resolve |
| `HEAD-MOVED` | The head SHA changed | `source=foreign` means someone else pushed: fetch before you push |
| `MERGED` / `CLOSED` | The PR ended | Stop pushing to the branch |
| `GREEN` | Complete and unchanged on two reads of the full check set | Judge threads and merge bar |

## Conflict recipes

The labels follow `.agents/skills/eshu-code-review/references/conflict-recipes.md`
and "Row 4a" in `eshu-code-review/SKILL.md`. The watcher only names files; it
never counts choice lines.

- **`4a`.** Every conflicted file is a recipe file: `CHANGELOG.md`,
  `go/internal/collector/repo/git/testdata/content_version_pin.json` (and
  `snapshot_freshness.go`, whose constant the same recipe re-pins),
  `specs/live-postgres-readiness-weights.v1.json`, or a path in the `GENERATED`
  list of `scripts/verify-live-postgres-readiness-drift.sh` (read from the
  checkout). Follow the recipe and run its named check; row 4a holds at any size.
- **`count-choice-lines`.** At least one conflicted file is not a recipe file.
  Row 4a may still apply at 10 or fewer choice lines in 2 or fewer files; more
  than that is row 4b. `surface-inventory.generated.json` is not a recipe file.
  If `agent_rule_files=N` is present, run `bash scripts/verify-docs-contradiction.sh`.
- **`rebase`.** The test merge was clean, usually `BEHIND`. Rebase onto the base
  and follow `rebase-verify.md`.
- **`unknown`.** The test merge could not run (see `reason=`); read the
  conflict yourself.

## Design notes

- State is read on every poll; the watcher keeps only markers of what it
  already reported. Nothing about a PR is cached at launch.
- The account is a path rule, not "the active gh user": the shared keyring
  holds more than one login, and a token for the wrong one reads as a 404.
- Logs come from `gh api --allow-escape-sequences repos/O/R/actions/jobs/ID/logs`
  because `gh run view --log-failed` refuses while a run is in progress.
- The tests run the real `run` and real `os/exec` path against a fake `gh`
  (the test binary re-executed) and a real git repository with a real conflict.
