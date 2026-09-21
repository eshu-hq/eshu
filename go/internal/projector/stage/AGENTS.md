# AGENTS.md — per-family projection stage guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../decode/README.md` for the fact-kind selectors these stages call.
4. `../runtime/README.md` for who invokes the stages and in what order.

## Invariants

- A stage is a pure function: envelopes in, result value out. No writes, no
  queue, no telemetry, no store reads.
- Import `../decode` for fact-kind selection. Do not import `../canonical` or
  the root `projector` package.
- A stage **skips** a fact it does not recognize. That is deliberately the
  opposite of the decode contract in `../decode`, where an unparseable payload
  must be quarantined or returned fatally — a fact belonging to another family
  is not a decode failure. Do not carry one rule into the other.
- Deduplication is per-stage and within-family, and it fails silently by
  inflating the graph rather than erroring. Each stage's test pins it
  directly; keep that coverage when changing selection.
- A new stage is wired by `../runtime`, not by a stage calling another stage.

## Verification

```bash
cd go && go test ./internal/projector/stage/... -count=1
cd go && go test ./internal/projector/runtime/... -count=1
```
