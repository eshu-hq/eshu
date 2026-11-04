# AGENTS.md - collector-document-import guidance

## Read First

1. `README.md` for the runtime contract and environment table.
2. `go/internal/collector/document/ocr/README.md` for extraction, redaction,
   and caller-owned enablement.
3. `go/internal/collector/document/ocr/tesseract/README.md` before touching
   engine pins, caps, or sandbox assumptions.

## Invariants

- Disabled by default stays fourfold: no chart or IaC reference, exit 0
  without the enable flag, an explicit `qa` tier required when enabled, and
  every engine pin required when enabled.
- QA-tier-only injection: the engine is constructed only for tier `qa`. Never
  widen the accepted tier without an arbiter ruling and a gate-2 evidence
  update; no production caller beyond ops-qa is claimed.
- Never add a fallback engine: a missing or mismatched pin is a startup
  error, not a quiet substitution. The fixture `LocalEngine` is test-only.
- One capability per PR on this path: `document/ocr` only until its gate-1
  proof exists. Do not wire media, export, or the preflights as heads here.
- Identity derives from content bytes only. Never key generations or scopes
  on file names, timestamps, or run counters.
- Envelopes from `ocr.Extract` commit unchanged. No new fact kinds, no new
  payload shapes, no new metrics on this path.
- No polling loop, no status server, no claim mechanics: one-shot drain,
  then exit. If `collector.Service` semantics become necessary, prove it from
  `go/internal/collector/service.go` first.
- Keep the artifact cap positive and residency to one file: `io.LimitReader`
  at `maxBytes+1`, `Lstat` before open, `os.SameFile` after open, no bodies
  retained between scan and drain, no symlink following.
- File names are persisted verbatim: keep them inert; redaction covers
  contents only.

## Verification

Run:

```bash
go test ./cmd/collector-document-import -count=1
go vet ./cmd/collector-document-import
```

Plus the package-docs gate:

```bash
scripts/verify-package-docs.sh
```

A disabled-start smoke run must exit 0 with zero commits:

```bash
go run ./cmd/collector-document-import; echo $?
```
