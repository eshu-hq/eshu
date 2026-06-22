# AGENTS.md — cmd/proof-of-value guidance for LLM assistants

## Read first

1. `go/cmd/proof-of-value/README.md` — purpose, usage, honesty boundary.
2. `go/cmd/proof-of-value/doc.go` — command contract.
3. `go/cmd/proof-of-value/main.go` — orchestration and report printing.
4. `go/cmd/proof-of-value/corpus.go` — corpus and ground-truth loading.
5. `go/internal/proofofvalue/` — the scorer and harness this command drives.

## Invariants

- This command only orchestrates. Scoring logic lives in `internal/proofofvalue`
  and reachability logic in `internal/iacreachability`. Do not inline scoring or
  reachability heuristics here.
- The corpus loader must group files by top-level directory as the repo ID, the
  same shape the `iacreachability` product-truth test uses. Keep it in sync.
- Ground truth comes only from the curated expected-truth file. Never derive it
  from the analyzer being measured.
- The printed and JSON numbers must be exactly what the scorer returns. Do not
  round, reweight, or filter to make Eshu look better.

## Verification

```bash
cd go && go test ./cmd/proof-of-value ./internal/proofofvalue -count=1
cd go && go run ./cmd/proof-of-value
cd go && gofmt -l cmd/proof-of-value
cd go && golangci-lint run ./cmd/proof-of-value/...
scripts/verify-package-docs.sh
```
