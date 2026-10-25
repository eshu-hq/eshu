# gitcontent

## Purpose

`go/internal/collector/git/content` owns the durable fact envelopes the git
collector emits for content: the per-file content fact, the per-entity
content-entity fact, the per-generation repository fact, and the per-file file
fact (with the fingerprint wall-clock observation collapsed to zero so durable
facts stay byte-deterministic).

## Where this fits

```
sync -> discover -> parse -> emit facts -> enqueue -> reducer -> projection -> query
                                    ^
                              this package
```

`gitrepo` drives the repository snapshot and the fact stream. It calls into
this package during emission; this package never calls back into `gitrepo`.
Anything both sides need lives in
`go/internal/collector/repo/git/model`.

## Exported surface

- `ContentFactEnvelope` — durable content fact for one snapshot file.
- `ContentEntityFactEnvelope` — durable content-entity fact for one parsed
  entity, including parser `entity_metadata` (fingerprint hashes and sketches).
- `RepositoryFactEnvelope` — durable repository fact for one generation. The
  caller passes the precomputed default branch and ref payload; ref selection
  stays in `gitrepo` with the `GitRef` type.
- `FileFactEnvelope` — durable file fact for one parsed file.
- `ApplyContentStore` — applies a definition's content-store policy (`store`,
  `metadata`, `redact`) to a source body at the write boundary (#8812) and
  returns the body the content store may hold plus a `StoreOutcome`. The
  `marker` redactor replaces credential values with `REDACTED_<finding_kind>`,
  binds to the migration 138 detector in `go/internal/secret/detect`, and fails
  closed: a redactor error or non-UTF-8 body returns an empty body with
  `StoreOutcomeRedactorFailed`, never the source. The `skeleton` redactor
  (#8833) keeps an allowlist skeleton of an open-schema file such as `*.tfvars`.
  The caller keeps the content digest on the SOURCE bytes.

## Notes

Stable keys keep re-emission idempotent: `content:<repo>:<path>`,
`content_entity:<uid>`, `repository:<repo>`, `file:<repo>:<path>`. Changing
what is emitted — new fact kind, changed payload shape, different counts —
changes projected truth, so it needs the cassettes and the B-12 snapshot
updated in the same change.

## Marker redactor: limits and bounds

- **Size cap.** A `redact` body over `MaxGovernedBodyBytes` (4 MiB) is not
  redacted: the path becomes a metadata row with reason `too_large`. The
  redactor is linear, but a hostile 50 MB body measured 4 to 14 s and it runs
  twice per collection (snapshot and emit); governed files are small
  configuration files.
- **Not text.** A body that is invalid UTF-8 or holds a NUL byte (BOM-less
  UTF-16 or UTF-32 is NUL-interleaved) becomes a metadata row. The WARN log
  names the repo-relative path and a bounded reason class: `invalid_utf8`,
  `not_text`, `too_large`, `redactor_error`, `redactor_panic`,
  `unknown_policy`, `unknown_mode`. It never carries error text.
- **PEM blocks.** A BEGIN header is replaced through its END footer on the same
  line, or to the end of the line when the footer is on a later line, and every
  following line is replaced until the footer. All header types are replaced, so
  a second pass changes nothing.
- **Detector matches redact to the end of the line.** A detector key match on a
  line without a listed key (`Authorization: Bearer ...`, `db_password=a,b`, a
  value followed by a comment) is treated like a listed key: everything from the
  separator to the end of the line becomes `REDACTED_<kind>`, with the opening
  quote restored around the marker. A value-shape match (an AWS key, a Slack
  token, a Stripe key) is replaced from the shape to the end of the line. One
  marker per line (arbiter ruling B1, 2026-10-24).
- **Known limits (denylist).** Marker mode redacts the values of the listed
  keys and the lines the detector matches. It does not see a listed key spelled
  as a YAML complex key (`? npmAuthToken` then `: value`), with an escaped
  spelling (`"npmAuth\x54oken"`), or with a homoglyph, and it does not redact a
  value under an unlisted key that the detector does not flag (an anchor
  definition `base: &t value`). Marker mode is for closed-schema files, where
  these shapes do not occur; `TestRedactMarkerKnownLimits` pins them.
- **URL userinfo keys.** A definition's `URLUserinfoKeys` (for `.yarnrc.yml`:
  `npmRegistryServer`, `npmPublishRegistry`, `httpProxy`, `httpsProxy`) lose
  only the userinfo of their URL value (`marker_url_userinfo.go`). The userinfo
  runs to the last `@` of the authority. A value that is not exactly one URL, a
  host that is not a plain name or IPv6 literal with a numeric port, an `@`
  after the host, a backslash in the value (YAML escapes), or any other detector
  match on the line falls back to the credential-key rule and the whole value
  goes. A URL key and a
  credential key on one line (a flow mapping) are both covered by the
  credential rule from the leftmost key.
- **Parity with migration 138.** The Go detector uses a superset whitespace
  class (`detect.SpaceClass`) because Postgres `[[:space:]]` matches U+2003 and
  U+3000 and RE2's does not, and Go `(?i)` folds U+017F and U+212A where Postgres
  in a C collation does not. #8833's parity corpus must include both.

## Skeleton redactor: rules and bounds

`redactSkeleton` (`skeleton_redactor.go`) is line-wise and keeps every line
number, the trailing newline, and `\r` line endings.

- **Kept:** empty lines, structural lines (`{`, `}`, `[`, `]`, `key = {`,
  `key = [`, heredoc openers and closers), and a `key = value` or
  `"key": value` value that is a boolean, `null`, a number, a `var.`, `local.`,
  `module.`, or `data.` reference, or a quoted version or constraint whose
  prerelease is a known word (`alpha`, `beta`, `rc`, `pre`, `dev`, `snapshot`).
- **Finding first:** when the raw line is a migration 138 finding (the line
  gate matches and the `CASE` names a kind), the value becomes
  `REDACTED_<finding_kind>`, even for a reference. A gate match with no kind
  (an `sk_live_` key alone) is no finding, so it becomes `REDACTED_value` and the
  #8833 marker arm adds no finding the raw body lacked. Any detector match still
  blocks the allowlist, so `x = var.sk_live_...` is not kept.
- **Whole line:** a line it cannot place (comment, bare list element, heredoc
  body, PEM block line, unterminated string, a detector match inside the key, a
  value that starts with `=` or `:` such as the base64 padding of `TOKEN==`)
  becomes `REDACTED_<finding_kind>` when it is a finding, else `REDACTED_value`,
  with its indent kept and no trailing comma.
- **State across lines:** a PEM `BEGIN` header with no `END` after it redacts
  every line through the footer, or to the end of the file; an unclosed heredoc
  redacts to the end of the file. A heredoc opened at the end of a line
  that ends in code (`key = <<EOF`, `keys = [<<EOF`) redacts its body; when its
  opener line was redacted, its closer is redacted too, which keeps the fixed
  point. A `<<EOF` inside a `#` or `//` comment or an unterminated string is
  text and opens nothing. A `${` or `%{` left open at the end of a line carries
  over: every line inside it is redacted whole until its `}`, so an unclosed `${`
  (a literal one in a JSON string, say) redacts to the end of the file. A `/*`
  outside a string or line comment with no `*/` after it redacts every line
  through its `*/`, whether it starts the line or follows a value.
- **Fixed point:** an exact marker, as a value or a whole line, is kept, so a
  second pass changes nothing.
- **Keys are kept when they are names** (8813 record): an identifier or a quoted
  key of letters, digits, `_`, `.`, and `-`. A quoted key holding other text (a
  URL, a CIDR, an ARN, symbols) takes the whole line. A credential spelled as a
  plain name survives as a key unless the detector flags it. `REDACTED_value` is
  used only by this mode.
- **Fail closed:** the same `MaxGovernedBodyBytes`, UTF-8, and NUL checks as the
  marker redactor; the caller stores a metadata row.
- **Proof:** `TestRedactSkeletonLineRules` pins the table,
  `TestRedactSkeletonPropertyCorpus` plants a credential-shape corpus (AWS key,
  `sk_live_`, Slack token, PEM block in a comment and in a heredoc, base64, hex,
  word with symbols, URL userinfo) and checks the stored-line grammar, line
  count, fixed point, and that every raw finding keeps its kind marker, and
  `FuzzRedactSkeleton` runs the same checks on arbitrary input.
