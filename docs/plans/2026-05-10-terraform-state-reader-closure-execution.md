# Terraform State Reader Closure Execution Split

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development for implementation and review loops.

**Goal:** Close the remaining #46 reader-stack gaps without mixing unrelated Terraform-state work into one hard-to-review PR.

**Architecture:** Keep the reader stack facts-first and exact-source only. The collector runtime may open local or S3 state, but it must not persist raw state, infer local files from Git, crawl S3 prefixes, or make cloud write calls. Reducer and correlation work stay out of this branch.

**Tech Stack:** Go, Postgres workflow claims, Terraform-state streaming JSON parsing, AWS SDK adapters behind local interfaces, OpenTelemetry.

---

## Chunk Order

### Chunk A: Conditional S3 Freshness

**Owner:** S3/runtime worker.

**Files:**
- `go/internal/collector/terraformstate/source_s3.go`
- `go/internal/collector/terraformstate/source_s3_test.go`
- `go/internal/collector/terraformstate/discovery.go`
- `go/internal/collector/tfstateruntime/source.go`
- `go/internal/collector/tfstateruntime/source_test.go`
- `go/cmd/collector-terraform-state/aws_s3.go`
- `go/cmd/collector-terraform-state/aws_s3_test.go`

**Scope:**
- Carry prior ETag metadata into exact S3 reads.
- Treat not-modified reads as an explicit workflow outcome, not a raw collector failure.
- Keep ETags opaque. Do not trim quotes or label them as secrets.

**Do not touch:** parser memory tests, DynamoDB lock metadata, telemetry docs.

### Chunk B: Parser Memory Proof

**Owner:** parser worker.

**Files:**
- `go/internal/collector/terraformstate/parser.go`
- new focused parser memory test files under `go/internal/collector/terraformstate`

**Scope:**
- Add a generated large-state regression test.
- Prove the parser path still avoids full-payload `json.Unmarshal`.
- Add a skipped or benchmark-style 100 MiB proof if the normal test would be too slow.

**Do not touch:** S3 source/runtime behavior, AWS adapters, telemetry docs.

### Chunk C: DynamoDB Lock Metadata

**Owner:** lock metadata worker. Start after Chunk A, because both chunks touch S3 source shape.

**Files:**
- `go/internal/collector/terraformstate/types.go`
- `go/internal/collector/terraformstate/source_s3.go`
- `go/internal/collector/terraformstate/source_s3_test.go`
- `go/cmd/collector-terraform-state/aws_s3.go`
- new AWS DynamoDB adapter tests under `go/cmd/collector-terraform-state`

**Scope:**
- Add read-only lock metadata through a narrow interface.
- Copy only safe metadata into `SourceMetadata`.
- Keep AWS SDK types out of `terraformstate` and `tfstateruntime`.

### Chunk D: Reader Telemetry And Docs

**Owner:** telemetry/docs worker. Start after Chunks A-C define the behavior.

**Files:**
- `go/internal/telemetry/instruments.go`
- `go/internal/telemetry/instruments_test.go`
- `go/internal/telemetry/contract.go`
- `go/internal/telemetry/contract_test.go`
- `go/internal/collector/terraformstate`
- `go/internal/collector/tfstateruntime`
- `docs/docs/reference/telemetry/metrics.md`
- `docs/docs/reference/telemetry/traces.md`
- `go/internal/collector/terraformstate/README.md`
- `go/cmd/collector-terraform-state/README.md`

**Scope:**
- Register and record the #46 reader metrics with bounded labels.
- Confirm spans cover source open, parser stream, and fact emission batches.
- Update docs with the exact safe operational signals.

---

## Initial Parallel Work

Start Chunk A and Chunk B in parallel. They are independent enough to work at the same time if both workers keep to their file ownership.

Do not start Chunk C until Chunk A lands. Do not start Chunk D until Chunks A-C land.

## Verification

For each chunk, run the smallest focused test first. Before opening the PR, run:

```bash
cd go
go test ./internal/collector/terraformstate ./internal/collector/tfstateruntime ./cmd/collector-terraform-state ./internal/telemetry -count=1
golangci-lint run ./internal/collector/terraformstate ./internal/collector/tfstateruntime ./cmd/collector-terraform-state ./internal/telemetry
git diff --check
```
