# secret/detect

Go source of truth for the hardcoded-secret detector behind
`content_file_secret_lines` (migration 138, #7125). Two consumers bind to it:

- `go/internal/query` aliases `SQLPattern` as `hardcodedSecretSQLPattern`, and
  its binding test compares `SQLPattern` and `ArmSources` with the migration
  text, so a Go-only edit fails the build.
- The collector's marker redactor (`go/internal/collector/git/content`, #8812)
  uses `Classify` for the `REDACTED_<finding_kind>` marker and `Spans` to find
  the values it replaces, so redaction and the stored findings use one pattern.

## Contract

- `SQLPattern` is the case-insensitive Postgres ARE pattern of migration 234
  (#8833). It concatenates the key-prefix, value, shape, URL-userinfo, and
  HCL block-opener parts, so the RE2 forms here cannot drift from it.
- `MarkerPrefix` (`REDACTED_`) is the substring migration 234 OR's with
  `SQLPattern` at file and line level: the marker arm's place in the gate.
- `ArmSources` lists the CASE arms in migration order: one marker arm per
  kind first (so a `REDACTED_<kind>` line keeps its kind; `REDACTED_value`
  matches none), then the six migration 138 arms unchanged, then the extras'
  arms. `Kinds` returns the six kinds in order of first appearance.
- `Classify` returns the CASE kind of one line or `""`.
- `Finding` is the stored finding: the gate (marker prefix or a `Spans` match)
  and then `Classify`. The redactors and the differential tests use it.
- `Spans` returns the value of a key match, or the whole shape; a userinfo span
  starts at the URL scheme. A marker is not a span.

## History

Migration 138 did not detect `npmAuthIdent`, `user:pass@` userinfo, JSON-quoted
keys, or secret-named block openers, and it lost a redacted
`aws_access_key`, `slack_token`, or `private_key` line. Migration 234 adds all
of them; the engine-parity live test compares this package with the migration
over the gap-table corpus.
