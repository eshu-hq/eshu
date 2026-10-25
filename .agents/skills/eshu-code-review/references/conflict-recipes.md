# Conflict Recipes

Resolutions for the files that conflict most often, so a rebase on one of them
is [row 4a](../SKILL.md#row-4a-mechanical-conflict) at any size. A recipe and
its named check are what make the resolution mechanical; skip the check and it
is not a recipe case.

The list comes from a census on 2026-10-25: 9 of 12 open PRs conflicted with
`main`, 7 on `CHANGELOG.md`, 5 on the collector content pin, 2 on the readiness
weights, and 2 on the generated readiness inventory (#9092). A file not listed
here follows row 4a's class and choice-line rules.

## `CHANGELOG.md`

Keep both entries under `## Unreleased`: the PR's block below main's newest
block. No gate, workflow, or release step reads the root file (the release
skill names only the two SDK changelogs), and it is outside the Markdown cap,
which walks `go/` and `docs/` only.

Check that these print nothing:

```bash
rg -n '^(<<<<<<<|=======|>>>>>>>)' CHANGELOG.md
awk '/^- \*\*/{t=$0; while (t !~ /\*\*.*\*\*/ && (getline l) > 0) t=t " " l; sub(/^- \*\*/,"",t); sub(/\*\*.*$/,"",t); gsub(/  +/," ",t); print t}' CHANGELOG.md | sort | uniq -d
rg '^### ' CHANGELOG.md | sort | uniq -d
```

Also check that `rg -c '^## Unreleased' CHANGELOG.md` prints 1, and run
`git diff --check`.

The duplicate key is the entry's bold title joined across its wrapped lines.
The first line alone is wrong: on `main`, two distinct entries (#7547 and
#7129) share a wrapped first line, so `rg '^- \*\*' | sort | uniq -d` reports
a false duplicate. A duplicate entry is a correctness finding: fix it. How the
merged entries read is prose form, which is P3
([merge-bar](merge-bar.md#prose-form)).

## Collector content pin

`go/internal/collector/repo/git/testdata/content_version_pin.json` is never
hand-merged. Take main's side, then re-pin:

1. Set `gitGenerationContentVersion` in
   `go/internal/collector/repo/git/snapshot_freshness.go` to one above main's
   value. Every open PR that changes collector output bumps the same constant,
   so each later one bumps again after the earlier one merges.
2. From `go/`, run
   `go test ./internal/collector/repo/git -run TestGenerationContentVersionPinsCollectorOutput -update-content-version-pin`.
3. Run the same test again without the flag. It must pass.

The constant lives in non-test Go, but the recipe still counts as 4a: the gate
test is the proof. The global version is a known serialization point (#9097).

## Live PostgreSQL readiness weights

For `specs/live-postgres-readiness-weights.v1.json`, take main's file whole,
then add back only the PR's new `package|Test` keys with the PR's values. A PR
usually adds one or two keys; a refit on `main` rewrites the whole file, so the
conflict block looks far larger than the PR's change. The refresher needs CI
shard logs, so this file cannot be regenerated locally. A missing key falls back
to a default weight. Check: `python3 scripts/test-live-postgres-readiness-shards.py`.

## Generated readiness files

Every path in the `GENERATED` list of
`scripts/verify-live-postgres-readiness-drift.sh` is generated, including
`scripts/lib/live_postgres_readiness_inventory_gen_*.py` and
`.github/workflows/live-postgres-readiness.yml`. Never hand-resolve them. Take
either side, then run:

```bash
python3 scripts/generate-live-postgres-readiness.py
bash scripts/verify-live-postgres-readiness-drift.sh
```

The drift script must exit 0.

## Agent-rule files

`AGENTS.md`, `.agents/skills/**`, `.claude/rules/**`, `.claude/settings.json`,
`.muse/hooks.json`, `.codex/**`, `docs/internal/agent-*.md`, and
`docs/internal/writing-for-agents.md` are the rules this review regime runs on.
A conflict there is row 4a only as a keep-both union of distinct bullets, rows,
or sections. Run `bash scripts/verify-docs-contradiction.sh`, post its output,
and treat any contradiction it reports as row 4b (a scoped re-review). A
resolution that rewrites or drops a rule is 4b regardless.

## Other append-only files

For registry rows, ledgers, and other append-only lists, keep both entries,
then run the file's own validator: `bash scripts/verify-ci-gates-registry.sh
--drift` for the gate registry (a keep-both union of trigger entries is a recipe
case here, with the drift gate as the proof), or the drift or verify script named in the
file's header. When a file has no validator, its entry lines must deduplicate to
nothing (`sort | uniq -d`).
