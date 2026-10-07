# git

## Purpose

`go/internal/collector/repo/git` is the git repository collector. It answers
"which repositories should we look at, what is in them right now, and what facts
does that produce" — the `sync -> discover -> parse -> emit facts` span of the
pipeline for git sources.

The root `collector` package keeps only the seam every collector kind shares:
`Service`, `Source`, `Committer`, `CollectedGeneration`, and the
`claimed_service*` machinery that roughly fifteen other collector kinds use.
Everything git-specific lives here.

## Layout

```
git/                     selection + snapshot + source (the tangled core)
  model/                 types and the fact-stream writer everything below shares
  docs/                  documentation extraction (markdown, OOXML, diagrams, ...)
  observability/         observability route and metric facts
  codeowners/            CODEOWNERS ownership facts (git-collector hook)
  submodule/             .gitmodules facts and pinned-SHA resolution (hook)
  service/catalog/       service catalog manifest facts (hook)
  tfstate/               Terraform backend-expression warnings
  workflow/image/        CI workflow container image evidence
```

Imports run one way: `git -> leaf -> model`. No leaf imports `git`.

## Why the core is one package

`snapshot_*`, `selection_*` and `source*` reference each other in
production code in all three directions. Splitting them into separate packages
does not compile without first inverting those dependencies, which is a
behaviour-changing refactor rather than a file move. They stay together until
that refactor is done and measured.

Three leaf names intentionally repeat a sibling parser package —
`codeowners`, `submodule`, and the `catalog` under `service/` — because each
is the git-collector hook over that parser: the parser understands the file
format, the hook decides when to run it and how the facts reach the stream.
The hook lives under `git/` and the parser stays outside it
(`collector/repo/codeowners`, `collector/repo/submodule`,
`collector/servicecatalog`), so the path still reads as a sentence. Files
that need both import the hook by its full path.

## Follow-up entity keys

The `shared_followup` facts that enqueue reducer domains carry an `entity_key`
of `<prefix>:<repository ID>`, built by one helper (`followupEntityKey` in
`followup_facts.go`) from `repo.ID`, the same canonical id the repository fact
publishes (`repository:r_<8-hex>`). It is never derived from the checkout path
or the display name: names are not unique across a run and can end in a colon
the alias normalizer cannot match, so name-keyed keys never select. The
reducer's candidate filter and retract matcher compare ids only (#7384); a
legacy name-keyed work item matches nothing and heals on the next full
generation, which re-emits id keys.

## Directory size

This directory is over the 40-file cap and carries a row in
`scripts/lib/dirgate-grandfather.tsv`. That row is a monotonic ratchet: the gate
fails if the count grows OR shrinks without the row being re-pinned in the same
change, so every future extraction has to lower it. Recompute with:

```bash
bash scripts/dev/precommit-go.sh dirgate-digest internal/collector/repo/git
bash scripts/generate-dirgate-grandfather-go.sh
```

## Reindex watermark

`NativeRepositorySelector` and `WebhookTriggerRepositorySelector` read the fleet
reindex watermark once per git cycle through `ReindexWatermarkReader` (#7620).
`decideForScope` checks a scope the sweep leaves `fresh` against it and forces
reason `reindex_requested` when the newest activated full predates it. Forcing
shares the sweep's per-cycle budget and throttle. A watermark later than the
cycle's `observedAt` is deferred, because generations are ingested at
`observedAt`. The reader is read-only: the request is never claimed. See
`docs/public/reference/reconciliation-sweep.md#reindex-requests`.

Both selectors also read the per-repository watermarks newer than the fleet
watermark through `RepositoryReindexWatermarkReader`, once per cycle
(`resolveRepositoryReindexWatermarks`), and defer each row on its own. A
scope's effective watermark is the later of the two
(`gitDeltaBaseline.reindexWatermarkFor`); the reason is
`repository_reindex_requested` only when the scope's own row is later.
`prioritizeRepositoryReindex` moves requested repositories to the front of
the sync order so the shared per-cycle budget cannot starve them. With the
sweep off and no fleet watermark, `reconcileDue` reads state only for scopes
with a row. This directory is pinned at its file count by the dirgate ledger,
so the code lives in `selection_reconcile_config.go` and
`selection_baseline.go`, not a new file.

Performance Evidence (#7620, per-repository): the read is one sequential scan
per cycle per shard, about 1 ms per 10,000 rows on PostgreSQL 18. Ordering
derives a scope ID per repository only when at least one row is active, using
`gitScopeIDForManagedRepo`, which does path and string work with no disk or
network I/O. `TestRepositoryReindexTargetedScopeIsNotStarved` fails without
the reordering (the requested 30th repository is not forced in the first
cycle with a budget of 10) and passes with it, still forcing exactly 10.

## Two-phase content

Snapshotting collects content file *metadata* first (bodies are temporary), then
re-reads each body from disk at emit time. Memory stays proportional to a single
file rather than the whole repository. `model.ContentFileMeta` is the phase-A
record; `model.ContentFileSnapshot` carries a body.

## Verification

```bash
cd go && go test ./internal/collector/... -count=1
cd go && go build ./... && go vet ./...
```

Anything that changes emitted facts also needs the golden-corpus gate (B-7) and
a byte-identical B-12 snapshot — see
`docs/public/reference/local-testing/golden-corpus-gate.md`.
