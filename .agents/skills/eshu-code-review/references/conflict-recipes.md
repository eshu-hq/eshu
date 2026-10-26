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

The root `/CHANGELOG.md` merges by union (`.gitattributes`, #9096), so a rebase
or merge normally applies with no conflict and keeps both sides. A conflict
marker in it means union was not in effect: a checkout without the attribute,
or a server-side merge (whether GitHub honors the attribute is unverified).
Then keep both entries under `## Unreleased`, the PR's block below main's newest
block. The file is outside the Markdown cap, which walks `go/` and `docs/` only.

The two SDK changelogs, `sdk/go/collector/CHANGELOG.md` and
`sdk/go/factschema/CHANGELOG.md`, are not union-merged and still conflict. They
are versioned release documents that repeat headings per version, and the
release skill reads them. Resolve a conflict there by hand: keep both entries
and check each against its version section.

After any merge of the root file, run:

```bash
bash scripts/verify-changelog-unreleased.sh
git diff --check
```

The script fails a second `## Unreleased` heading, a conflict marker, a
duplicate `### ` heading, and a duplicate entry. The duplicate key is the
entry's bold title joined across its wrapped lines. The first line alone is
wrong: on `main`, two distinct entries (#7547 and #7129) share a wrapped first
line, so `rg '^- \*\*' | sort | uniq -d` reports a false duplicate. A duplicate
entry is a correctness finding: fix it.

A clean union merge is not proof the result is right. Two branches that edit
the same existing entry merge without a conflict, keep both versions, and can
still pass the script, for example when one branch changes the title and the
other the body. Read that hunk. How the merged entries read is prose form, which
is P3 ([merge-bar](merge-bar.md#prose-form)).

## Collector content pin

`go/internal/collector/repo/git/epoch/ledger.tsv` is generated and never
hand-merged (#9097). Two PRs that both move collector output conflict on its
`epoch` line; two that touch the same fixture file conflict on that bucket
line. The shortcut is `bash scripts/dev/regen-conflicts.sh` (#9150), which
does the take-main's-copy plus re-pin below and stages the result. By hand,
take main's copy, then re-pin:

1. `git checkout origin/main -- go/internal/collector/repo/git/epoch/ledger.tsv`
2. From `go/`, run
   `go test ./internal/collector/repo/git -run TestGenerationContentVersionPinsCollectorOutput -update-content-version-pin`.
   It advances the epoch past main's when the PR's output moved on unchanged
   input, and keeps main's epoch for a fixture-only change. Add
   `-advance-content-epoch` only when the PR's own description says it edits a
   fixture file and that file's output together. This applies after the
   helper re-pins too: a combined change re-pinned without the flag ships a
   stale epoch with every gate green, so confirm the decision even when the
   helper announces the re-pin as routine.
3. Run the same test again without the flag. It must pass.

Do not keep both sides' bucket lines under one side's `epoch` line: the re-pin
would then see no moved output, keep the epoch, and ship new facts under an old
hint. The `epoch` line carries a seal over the bucket lines, so the gate test
fails on such a ledger and the re-pin refuses it. No Go source is edited, so
the recipe is 4a with the gate test as the proof.

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
`.github/workflows/live-postgres-readiness.yml`. Never hand-resolve them. The
shortcut is `bash scripts/dev/regen-conflicts.sh` (#9150), which regenerates
and stages them. By hand, take either side, then run:

```bash
python3 scripts/generate-live-postgres-readiness.py
bash scripts/verify-live-postgres-readiness-drift.sh
```

The drift script must exit 0.

## Generated docs (`ci-gates.md`, env registry)

`docs/public/reference/ci-gates.md` and the env registry pages
(`docs/public/reference/env-registry.md` plus `env-registry/*.md`) are
generated and never hand-merged: resolve a conflict there with
`bash scripts/dev/regen-conflicts.sh` (#9150), which reruns the owning
generator and stages the result, then run the artifact's own drift gate
(`bash scripts/verify-ci-gates-registry.sh --drift` for the former,
`bash scripts/verify-env-registry-doc.sh` for the latter).

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
