# AGENTS.md — go/internal/collector/repo/git/epoch

## Read first

1. `go/internal/collector/repo/git/epoch/README.md` — the ledger and the re-pin flow
2. `go/internal/collector/repo/git/epoch/doc.go` — the package contract
3. `go/internal/collector/repo/git/snapshot_freshness.go` — where the epoch is
   folded into the freshness hint
4. `go/internal/collector/repo/git/snapshot_content_version_gate_test.go` — the
   gate that computes the buckets and writes `ledger.tsv`

## Rules

- This package is a leaf. `git` imports it; it must never import `git` or any
  other Eshu package.
- Never edit `ledger.tsv` by hand, and never type an epoch. Re-pin with
  `-update-content-version-pin`, plus `-advance-content-epoch` when the ledger
  cannot see the output change (see the README).
- On a merge conflict in `ledger.tsv`, take `origin/main`'s copy
  (`git checkout origin/main -- go/internal/collector/repo/git/epoch/ledger.tsv`)
  and re-pin. Do not keep both sides' bucket lines: the re-pin would compare
  against lines no deployed binary ran, keep the epoch, and ship new output
  under an old hint. The seal check refuses such a ledger.
- Keep the advance rule in `Repin`: advance only when output moved on unchanged
  input, or when forced. Advancing on a fixture-only change re-stores one
  generation per repository for no output change; not advancing on an output
  change drops new facts as duplicates.
- Keep the value folded into the hint one global string. Per-bucket folding is
  not ruled in (#9097 arbiter ruling, decision 1).
- `Current` panics on a malformed embedded ledger. Do not turn that into a
  fallback value: a wrong epoch silently skips new output.

## Verification

```bash
cd go && go test -race ./internal/collector/repo/git/epoch
cd go && go test -race ./internal/collector/repo/git -run 'ContentVersion|ContentEpoch'
```
