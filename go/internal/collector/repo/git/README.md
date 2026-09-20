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

## Directory size

This directory is over the 40-file cap and carries a row in
`scripts/lib/dirgate-grandfather.tsv`. That row is a monotonic ratchet: the gate
fails if the count grows OR shrinks without the row being re-pinned in the same
change, so every future extraction has to lower it. Recompute with:

```bash
bash scripts/dev/precommit-go.sh dirgate-digest internal/collector/repo/git
bash scripts/generate-dirgate-grandfather-go.sh
```

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
