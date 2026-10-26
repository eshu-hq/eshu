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
  binds to the migration 235 detector in `go/internal/secret/detect`, and fails
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
  `not_text`, `too_large`, `yaml_indirection`, `redactor_error`,
  `redactor_panic`, `unknown_policy`, `unknown_mode`. It never carries error
  text.
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
- **YAML indirection fails closed** (`marker_yaml.go`, #8833). A body that holds
  `*`, `?`, a backslash, `{`, `[`, a double quote, or a single quote is parsed
  with `gopkg.in/yaml.v3` after line redaction, and the check reads the
  redacted lines. Any alias anywhere, or
  a mapping key that decodes to a listed key on a source line where the key
  pattern does not match (an explicit `? npmAuthToken` entry, an escaped
  spelling such as `"npmAuth\x54oken"`), returns `errYAMLIndirection`. So does a
  listed key whose value can reach text the redactor left in the clear: the
  redactor treats the lines after a listed key as its value only while each is
  more indented than the key's line (or a same-indent `- ` or `-<tab>` item),
  but inside a flow collection a line break is white space at any indent, a
  plain scalar can continue at any indent, and a quoted scalar spans lines at
  any indent when its quote opens after the key's line. yaml.v3 reports only where a node
  starts, so for each listed key (not one inside another listed key's value) the
  value is taken to reach every line from the line after the key to the line
  before the next node starts, plus the text before that node on its own line.
  The text that could be value on those lines must be nothing (blank, a comment,
  flow brackets and commas, a `---` or `...` marker; on a line inside a quoted
  scalar, found by reading the source for the closing quote, only white space
  is nothing, so a line that starts with `#` there is text), or lie inside the one span
  the redactor replaced with a `REDACTED_<kind>` marker on that line (found by
  matching the unchanged text before and after the marker against the source),
  or be exactly a marker or `${NAME}` reference an earlier pass kept. Marker
  text beside other text, or a line with a replaced span that does not cover
  the tail, fails closed. The walk has no depth cap. An ordinary block file never
  trips it: the next key starts on the next line, and a block scalar spills
  only onto more-indented lines the redactor replaces whole. A quoted scalar
  that closes at or below the key's indent fails closed instead. A
  comment line at or below the key's indent does not end the continuation.
  yaml.v3 rejects a tab as flow indentation and a bare document after `...`,
  which Yarn's js-yaml reads; for a body it cannot parse, the lines after the
  separator of a line a listed key matches are followed with a lexical bracket
  and quote count when the body holds `*`, `?`, a backslash, `{`, or `[`, or
  when a quote-only body holds a `...` document marker (a bare document after
  `...` is what yaml.v3 rejects while js-yaml reads). A quote-only body
  without `...` is an ordinary non-YAML file the redactor covers line-wise.
  A quote opens a scalar only where YAML starts one (the
  start of the value, after a flow opener, comma, `: `, `? `, `- `, or a tag or
  anchor), so `a"b` and `it's` are plain text. Because the count cannot place
  every quote with certainty, once any quote has opened a `#` line is value
  text, not a comment. While a bracket or quote the value opened is open, every
  line is value and is held to the same text rule (a leading closer or comma
  does not end it). Once both are closed, a bracket-only line or an entry line
  ends it. A line check runs for every body: an
  explicit-key indicator (also in a flow collection or with CRLF endings) or
  value line, and a listed key whose value starts with an alias. The path then
  becomes a metadata row with reason `yaml_indirection`. A body with none of
  the seven bytes skips the parse.
- **What the YAML check guarantees.** For a body yaml.v3 parses, no byte a
  listed key's decoded value occupies after the key's own line is stored in the
  clear: the line rule replaces the key's line from the separator to the end of
  the line, and the text rule above covers every later line the value can reach.
  That is the whole class the check claims, and the tests sweep it against
  yaml.v3, js-yaml, and PyYAML. For a body yaml.v3 rejects, the lexical rule is
  a heuristic: swept the same way, not proven. A shape found there joins
  `flowHostileSeeds`. A value under a key the definition does not list is the
  denylist limit below.
- **Known limits (denylist).** Marker mode redacts the values of the listed
  keys and the lines the detector matches. It does not see a listed key spelled
  with a homoglyph, or split across lines inside quotes in a body that is not
  valid YAML, and it does not redact a value under an unlisted key that the
  detector does not flag. Marker mode is for closed-schema files, where these
  shapes do not occur; `TestRedactMarkerKnownLimits` pins them.
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
- **Parity with migration 235.** The Go detector uses a superset whitespace
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
- **Finding first:** when the raw line is a migration 235 finding (the line
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
  every line through the footer, or to the end of the file. Everything else
  comes from one lexical stack (`scanState`, `skeleton_scan.go`) that follows
  HashiCorp's HCL lexer: quoted templates, heredoc templates, `${ }` and `%{ }`
  interpolations, and braces inside them, plus an open `/* */` comment. A line
  that begins inside any of them is redacted whole.
  - A heredoc opens at the end of a line that ends in code, with `<<ID` or
    `<<-ID` and nothing after it (`key = <<EOF`, `keys = [<<EOF`, or inside an
    interpolation). It closes on a line whose text, after its last
    interpolation, is the marker. A heredoc nests: `${ <<INNER` inside a
    heredoc body opens a second one, and only the innermost marker closes.
    `<<~EOF` and `<<"EOF"` are not HCL heredocs and open nothing.
  - A `<<EOF` inside a `#` or `//` comment, a string, or a heredoc body is text.
    `$${` and `%%{` are text in strings and heredocs. A comment inside an
    interpolation does not end it.
  - When the opener line was redacted, its closer is redacted too, which keeps
    the fixed point. When a top-level `key = <<ID` line is kept, its closer is
    kept.
  - Lexing failure is sticky. Nesting past 32 levels, a line that ends inside
    a quoted template (HCL's `TokenQuotedNewline`, invalid HCL), or a bare
    `\r` inside a quoted template or heredoc text redacts every later line
    whole. A bare `\r` is one that is not the `\r` of a CRLF line ending; it
    is a `TokenQuotedNewline` in a string and a `TokenInvalid` in heredoc text,
    where HCL stops lexing. A CRLF ending is trimmed as usual, and a bare `\r`
    in code or in a comment is no failure. Nothing clears it. The body is still stored with
    its line count and its findings; what is lost is the key names and the kept
    values after the first failure, so an unclosed `${` in a JSON string value
    costs the rest of that file. An unterminated `/*` that never closes redacts
    to the end of the file.
- **No new finding:** a stored line is never a finding its raw line is not; a
  key-named variable with a short value goes whole, because
  `key = REDACTED_value` would match the detector's key arm.
- **Evidence shapes** (`skeleton_evidence.go`): `app_repo` and `app_name` names
  and plain URLs, normalized github.com URLs (values, list elements, and the URLs
  a comment names), and `repo:owner/name:` OIDC subjects survive, so relationship
  discovery keeps its Terraform evidence. Any detector match, with or without
  a kind, still comes first.
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
