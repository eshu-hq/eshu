# #6693 checklist step 16: `admission/` leaf move

Moved `admission_decisions.go`, `admission_decisions_validation.go`, and
`schema_admission_decisions.go` (plus their 4 mapped tests) out of
`go/internal/storage/postgres` (root, package `postgres`) into
`go/internal/storage/postgres/admission/`, package `admissionstore`, per the
mapping in `docs/internal/design/6693-postgres-target-tree/control-plane.md`.
No SQL text, logic, or identifier renames beyond the package clause; all 4
test files stayed in-package as mapped (no annotation called for an external
test package or `export_test.go` shim).

`decisions_test_helpers_test.go`'s `admissionDecisionTestDB` fake used root's
private `result{}` type (defined in `schema_bootstrap_files_test.go`, not
part of this mapping and now inaccessible from the new package). Replaced its
three `result{}` returns with the shared `fake.Result{}` from
`internal/storage/postgres/fake`, which implements the identical
`sql.Result` interface; no test assertion reads the affected values
(`LastInsertId`/`RowsAffected`), so this is a like-for-like substitution, not
a behavior change.

## Callers repointed

- `go/internal/query/admission_decision_store.go` (API/MCP admission-decision
  read path)
- `go/cmd/reducer/admission_decision_wiring.go` and its test
  `admission_decision_wiring_test.go` (reducer write path)

All three now import `github.com/eshu-hq/eshu/go/internal/storage/postgres/admission`
unaliased and reference `admissionstore.*` (the package's own declared name),
matching the `graph/owner` (`ownerstore`) precedent. No other caller in `go/`
references `postgres.AdmissionDecision*` outside this set (confirmed by `rg`
across `go/`).

## Docs updated

- `go/internal/storage/postgres/doc.go`: removed the root package-doc
  paragraph describing `AdmissionDecisionStore` (the type no longer lives in
  root).
- `go/internal/storage/postgres/README.md` and `exported-surface-guide.md`:
  qualified every remaining `AdmissionDecisionStore` /
  `AdmissionDecisionSchemaSQL` mention with `admissionstore.`.
- New doc trio at `go/internal/storage/postgres/admission/{doc.go,README.md,AGENTS.md}`.
- `docs/internal/design/6693-postgres-target-tree.md`: ticked checklist item
  16. The counts table already listed `admission/` as 3 non-test / 4 test and
  needed no change; no mapped file stayed in root, so no tally adjustment.

## Verification (from `go/` unless noted, `GOCACHE` isolated per environment)

- `gofumpt -l -w` on every changed/moved Go file: exit 0, no files listed.
- `go build ./...`: exit 0.
- `go vet ./...`: exit 0.
- `go vet -tags "integration perf5854_ack perf5740_completion perf6785_wait" ./internal/storage/postgres/...`: exit 0.
- `go test ./internal/storage/postgres/... -race -count=1`: exit 0, includes
  `ok github.com/eshu-hq/eshu/go/internal/storage/postgres/admission`.
- `go test ./internal/query -run AdmissionDecision -count=1`: exit 0.
- `go test ./cmd/reducer -run AdmissionDecision -count=1`: exit 0.
- `go test -list '.*' ./internal/storage/postgres/admission/` lists all 7
  moved test names.
- Per-name repoint proof, for each of
  `TestAdmissionDecisionStoreCapsEvidenceLimit`, `TestAdmissionDecisionSchemaSQL`,
  `TestAdmissionDecisionStatesCoverClosedVocabulary`,
  `TestAdmissionDecisionStoreUpsertListAndEvidence`,
  `TestAdmissionDecisionStoreRequiresBoundedListFilter`,
  `TestAdmissionDecisionStoreClampsListLimit`,
  `TestAdmissionDecisionStoreRejectsUnknownStateBeforeWrite`:
  `go test ./internal/storage/postgres/admission/... -list "^<Name>$" -count=1 | rg -q "^<Name>$"`
  exits 0, and the same command against `./internal/storage/postgres` exits 1
  for every name (all 7 confirmed both ways).
- From repo root: `bash scripts/verify-dirgate.sh --all`: exit 0.
- From repo root: `bash scripts/verify-package-docs.sh`: exit 0.
- From repo root: `git diff --check`: exit 0.
- `rg` for `admission_decisions.go`, `admission_decisions_validation.go`,
  `schema_admission_decisions.go`, and every moved test's old base name
  across `go/`, `docs/`, `specs/`, `scripts/`, `.github/` returns nothing
  outside `docs/internal/design` and this evidence file.

No-Regression Evidence: `go test ./internal/storage/postgres/... -race -count=1`, `go test ./internal/query -run AdmissionDecision -count=1`, and `go test ./cmd/reducer -run AdmissionDecision -count=1` all exit 0 after the move, covering the same store, read, and write-path behavior the code had in root before the move.
No-Observability-Change: this is a path/package move only; no metric, span, log field, worker, queue, lease, retry, or durable write changed.
