# Fact Schema Contracts Agent Rules

This directory is a standalone public Go module for versioned
collector-reducer payload contracts (Contract System v1 §3.1). It must
remain independent from Eshu internals, mirroring `sdk/go/collector`'s
`AGENTS.md`.

## Required Checks

- Read the root `AGENTS.md` and `docs/internal/agent-guide.md` before edits.
- Keep `go.mod` as a standalone module.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`.
- Update `README.md`, `doc.go`, the generated JSON Schema under `schema/`,
  and `decode_test.go` / `schema_gen_test.go` when changing a payload
  struct's shape.
- Run `go generate ./...` and commit the result whenever a payload struct
  changes; `schema_gen_test.go` fails the build on drift.
- Run `go test ./... -count=1` from this directory.
- Run `gofmt` for changed Go files and `git diff --check` from the repo
  root.

## Contract Rules

- Required payload fields are non-pointer struct fields with no `omitempty`
  tag; optional fields are pointers or carry `omitempty`. Both the schema
  generator (`internal/schemagen`) and the decode seam's required-field
  check (`decode.go`) must keep deriving from the same struct shape.
- A required field **absent** from a payload map is a classified
  `*DecodeError` (`ClassificationInputInvalid`) naming the field, never a
  zero-value struct. A present-but-empty required field decodes
  successfully — do not conflate "absent" with "empty."
- `ClassificationInputInvalid` is this module's own constant. Do not import
  `go/internal/projector`'s dead-letter triage classes; the reducer maps by
  string value instead.
- The reducer only ever decodes the **latest** struct for a fact kind;
  version shims for older schema majors live in this module's decode
  functions, never in reducer handler code.
- Do not add envelope unification (aliasing/generating `Envelope` from
  `go/internal/facts.Envelope` or `sdk/go/collector.Fact`) here — it is
  documented follow-up work in `README.md` and design §3.1/§7, out of scope
  for this scaffold.
- One fact kind (`aws.resource`) is intentionally the only kind in this
  scaffold. Adding a second kind or migrating a real fact family is
  follow-on epic work, not a change to make in this directory casually.
