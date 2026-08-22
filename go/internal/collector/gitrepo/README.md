# gitrepo

## Purpose

`go/internal/collector/gitrepo` is the git repository collector. It answers
"which repositories should we look at, what is in them right now, and what facts
does that produce" — the `sync -> discover -> parse -> emit facts` span of the
pipeline for git sources.

The root `collector` package keeps only the seam every collector kind shares:
`Service`, `Source`, `Committer`, `CollectedGeneration`, and the
`claimed_service*` machinery that roughly fifteen other collector kinds use.
Everything git-specific lives here.

## Layout

```
gitrepo/                 selection + snapshot + source (the tangled core)
  gitmodel/              types and the fact-stream writer everything below shares
  gitdocs/               documentation extraction (markdown, OOXML, diagrams, ...)
  gitobs/                observability route and metric facts
  gitcodeowners/         CODEOWNERS ownership facts
  gitsubmodule/          .gitmodules facts and pinned-SHA resolution
  gitsvccatalog/         service catalog manifest facts
  gittfstate/            Terraform backend-expression warnings
  workflowimage/         CI workflow container image evidence
```

Imports run one way: `gitrepo -> leaf -> gitmodel`. No leaf imports `gitrepo`.

## Why the core is one package

`git_snapshot_*`, `git_selection_*` and `git_source*` reference each other in
production code in all three directions. Splitting them into separate packages
does not compile without first inverting those dependencies, which is a
behaviour-changing refactor rather than a file move. They stay together until
that refactor is done and measured.

Five of the leaf names are deliberately disambiguated — `gitsubmodule`,
`gittfstate`, `gitsvccatalog`, `gitcodeowners`, and `gitdocs` — because
`collector/submodule`, `collector/terraformstate`, `collector/servicecatalog`
and `collector/codeowners` already exist and these packages import them. The
directory gate also rejects a file whose name matches a sibling subpackage.

## Directory size

This directory is over the 40-file cap and carries a row in
`scripts/lib/dirgate-grandfather.tsv`. That row is a monotonic ratchet: the gate
fails if the count grows OR shrinks without the row being re-pinned in the same
change, so every future extraction has to lower it. Recompute with:

```bash
bash scripts/dev/precommit-go.sh dirgate-digest internal/collector/gitrepo
bash scripts/generate-dirgate-grandfather-go.sh
```

## Two-phase content

Snapshotting collects content file *metadata* first (bodies are temporary), then
re-reads each body from disk at emit time. Memory stays proportional to a single
file rather than the whole repository. `gitmodel.ContentFileMeta` is the phase-A
record; `gitmodel.ContentFileSnapshot` carries a body.

## Verification

```bash
cd go && go test ./internal/collector/... -count=1
cd go && go build ./... && go vet ./...
```

Anything that changes emitted facts also needs the golden-corpus gate (B-7) and
a byte-identical B-12 snapshot — see
`docs/public/reference/local-testing/golden-corpus-gate.md`.
