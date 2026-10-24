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

- `SQLPattern` is the case-insensitive Postgres ARE pattern. It is the
  concatenation of the key-prefix, value, and shape parts, so the RE2 forms
  here cannot drift from it.
- `Kinds` and `ArmSources` list the six finding kinds in the migration `CASE`
  order. The value-shape arms (AWS key, PEM header, Slack token) come first.
- `Classify` returns the kind of one line or `""`.
- `Spans` returns the value of a key match, or the whole shape.

## Gaps owned elsewhere

Migration 138 does not detect `npmAuthIdent`, `user:pass@` userinfo, JSON-quoted
keys, or secret-named block openers. #8833 adds them in one new migration and an
engine-parity test over the gap-table corpus (Go RE2 against Postgres ARE).
Until then the redactor's credential-key list, not this detector, is what
redacts those values.
