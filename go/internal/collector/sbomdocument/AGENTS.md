# sbomdocument package agent rules

The `sbomdocument` package parses CycloneDX and SPDX JSON documents into
SBOM source facts. Agents working under this directory MUST follow the
root [AGENTS.md](../../../../AGENTS.md) AND the rules below.

## Mandatory skills

- `golang-engineering` — for any code change.
- `eshu-correlation-truth` — when adjusting subject identity, dependency
  resolution, or anything the reducer relies on.
- `eshu-folder-doc-keeper` — when touching `doc.go`, `README.md`, or this
  file.

## Non-negotiable rules

- MUST NOT mark a parser-emitted fact as verified. Parser facts always
  carry `verification_status = ""` and `SourceConfidence = reported`.
  Only the attachment reducer may promote a document to
  `attached_verified`, and only when attestation/signature evidence
  exists.
- MUST keep fact payload keys in sync with the consumer in
  `go/internal/reducer/sbom_attestation_attachment_index.go`. Adding a
  payload key without updating the reducer is a contract break.
- MUST keep parser warnings explicit and machine-routable. New warning
  reasons go into `types.go` as a `WarningReason` constant; never emit
  ad-hoc reason strings.
- MUST preserve deterministic output: hashes, licenses, anchors, and
  external references are sorted before they hit the payload.
- MUST keep files under 500 lines. Split per-format projection into a
  dedicated `<format>_components.go` when a `<format>_fixture.go` is at
  risk of growing.
- MUST NOT introduce sync I/O. The package only consumes a byte slice.

## TDD expectations

Add or update a fixture under `testdata/` before changing the parser.
Cover at least: subject-present, missing-subject, ambiguous-subject,
duplicate-identity, unsupported-field, and malformed-body cases.

The reducer integration test in `reducer_integration_test.go` is the
correlation gate. Any change that affects subject derivation, parse
status, or fact kinds emitted MUST keep that test green or extend it
with a new case.
