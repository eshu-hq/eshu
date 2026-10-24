# npmrc Parser

## Purpose

`internal/parser/npmrc` answers one question for an `.npmrc` file: which package
registry does this repository install from? It emits one registry-URL row per
`registry=` or `@scope:registry=` line and nothing else. An `.npmrc` carries
those URLs next to credentials (`_authToken`, `_auth`, `_password`,
`//host/:username`, `email`), so the parser is built to be unable to emit a
credential.

## Ownership boundary

This package owns reading the two registry key shapes and sanitizing each value.
It does not own admission (the `npmrc` exact-name definition in
`../registry_definitions.go`), the content-store policy (`metadata`, applied at
the collector write boundary by `internal/collector/repo/git`), or any use of the
rows downstream. It does not parse any other `.npmrc` key.

## Why the parser carries the safety, not the policy

The `metadata` content-store policy (#8812) stores an empty-body `content_files`
row, so no body, search copy, or entity `source_cache` carries a credential. It
governs raw-text copies only. Values a parser extracts are parser output and are
outside the policy (`TestParserOutputIsNotGovernedKnownLeak`,
`docs/internal/design/8813-credential-file-exposure.md`). A parser that echoed a
credential would leak it into facts regardless of the policy. This parser reads
only the registry key shapes, strips userinfo, query, and fragment, and drops a
line it cannot prove clean.

## Row contract

Rows go in the `variables` bucket, so they project as Variable entities with
these keys:

| Key | Value |
| --- | --- |
| `name` | `registry` or `@scope:registry`, as written |
| `value` | `scheme://host[:port]/path`, with no userinfo, query, or fragment |
| `registry_scheme` | `http` or `https` |
| `registry_host` | lower-case host, with `:port` when present; an IPv6 literal keeps its brackets |
| `registry_path` | the escaped URL path on a documented layout, possibly empty; empty when withheld |
| `registry_path_withheld` | `true` only when the path is off the allowlist and was dropped; the scheme and host stay |
| `registry_scope` | `@scope`, only on a scoped row |
| `config_kind` | `registry_url` |
| `section` | `registry` or `scope_registry` |
| `line_number` | the source line, counting blank, comment, and credential lines |
| `package_manager` | `npm` |
| `lang`, `lockfile` | `npmrc`, `false` |

A repeated key keeps every line, in file order. The payload never carries
`source`, whatever `Options.IndexSource` says.

## A line is skipped, never partly emitted

The parser skips the whole line when the key is not `registry` or
`@scope:registry`; the line sits under an ini `[section]`; the value is empty,
holds `${...}`, a control byte, or a non-ASCII byte, or is longer than the 8 KiB
line bound; the value is not an absolute `http` or `https` URL with a DNS-style
or IPv6 host; the path is longer than 512 bytes; the shared detector
(`internal/secret/detect`) flags the sanitized scheme and host; or the userinfo
split is suspect.

The userinfo rule exists because `net/url` ends the authority at the first `/`,
`?`, or `#` and accepts an empty or all-digit port. `https://user:/pw@host/` then
parses with the user name as the host and the password in the path. The parser
counts the `@` in the whole value text, before comment truncation and quote
stripping. It keeps the line only when that count is 0, or is 1 and `net/url`
read it as the userinfo delimiter. Any other count drops the line, including an
`@` in a path, query, fragment, or trailing comment.

One exception keeps Azure Artifacts feed views. Every Azure feed has `@Local`,
`@Prerelease`, and `@Release` views that serve npm, so `_packaging/my-feed@Release`
is a real registry path. On `pkgs.dev.azure.com` and `*.pkgs.visualstudio.com`,
with no port, query, or fragment and no `@` in the authority, a single `@` in the
path is accepted. The parser writes it as `%40`, so no emitted field holds an
`@`, and the feed and the view must both read as names or the path is withheld. Parse errors never reach
the caller or a log, because `net/url` errors quote the input.

An unquoted value follows the npm `ini` reader: it ends at the first unescaped
`;` or `#`, and `\;`, `\#`, and `\\` are literal. A backslash inside quotes drops
the line.

## Path allowlist and residual risk

A token can sit in a URL path as a bare segment with no key and no known value
shape. Gemfury documents exactly that: `https://npm-proxy.fury.io/<token>/<user>/`.
No heuristic sees such a token reliably, so the path is an allowlist.
`registryPathAllowed` (`path_guard.go`) keeps the path only when the host is not
`*.fury.io` and the path is empty, `/`, or one of these documented layouts, with
every `*` slot reading as a name:

| Layout | Host |
| --- | --- |
| `/artifactory/api/npm/*` | any |
| `/repository/*`, `/nexus/repository/*` | any |
| `/*/_packaging/<feed>/npm/registry`, `/*/*/_packaging/<feed>/npm/registry` | any |
| `/_packaging/<feed>/npm/registry` | `*.pkgs.visualstudio.com` |
| `/api/v4/projects/*/packages/npm`, `/api/v4/groups/*/-/packages/npm`, `/api/v4/packages/npm` | any |
| `/api/packages/*/npm` | any |
| `/*/*` | `npm.cloudsmith.io` and `*-npm.pkg.dev` |
| `/npm/*` | AWS CodeArtifact hosts (`.amazonaws.com`, `.amazonaws.com.cn`, and the dualstack `.on.aws`) |

Any other path, and every path on a Gemfury host, is dropped: the row keeps the
scheme and host and sets `registry_path_withheld`. A `<feed>` slot is a name, or a name and a view joined by `%40`. A slot reads as a
name when it is at most 64 bytes of `[A-Za-z0-9._~-]` made of words of at most 16
bytes, each all lower case, all upper case, or Capitalized, with no
letter-and-digit mix of 8 bytes or more. A PascalCase word (`FabrikamFiber`,
`MyFeed`, `DataweaversPublic`) of at most 24 bytes and four Capitalized parts also
reads as a name; `AbCdEfGhIj` has five parts and does not. A word that mixes letters and
digits at 8 bytes or more fails, and so do UUID groups; a draw that happens to be
all letters or all digits in each word passes, which is where the rates below come from.

Residual: a token that a vendor puts in a documented slot would pass if it is a
short lower-case, upper-case, or Capitalized word, digits only, or a PascalCase
word of up to four parts and 24 bytes. Random tokens pass the slot check at the
rates below, measured on 2,000,000 uniform draws per cell with one seed:

| Token alphabet | 8 bytes | 12 | 16 | 20 | 24 | 32 |
| --- | --- | --- | --- | --- | --- | --- |
| base62 | 1.4e-2 | 2.2e-3 | 2.2e-4 | 1.7e-5 | about 5e-7 | 0 |
| base64url | 3.9e-2 | 6.5e-3 | 9.2e-4 | 1.2e-4 | 1.9e-5 | 0 |
| letters only | 5.9e-2 | 1.8e-2 | 3.9e-3 | 5.3e-4 | 8.3e-5 | 0 |
| hex | 2.4e-2 | 3.6e-3 | 5.7e-4 | 0 | 0 | 0 |
| base36 (lower case and digits) | 7.4e-2 | 2.0e-2 | 5.5e-3 | 0 | 0 | 0 |

Hex and base36 tokens pass up to 16 bytes and fail from 17 bytes on, because they
have no separator, so the token is one word, and a word over 16 bytes
(`maxSlotWordBytes`) is rejected. Up to 16 bytes it passes only when it is all
letters or all digits. The documented vendors above put the credential in
`_authToken` or `_auth`, not in the path. A host the table does not know loses its
path, so a private registry on a custom layout reports the host only. Userinfo
and query, the positions npm documents for credentials, are always removed.

## Dependencies

Imports `internal/parser/shared` (`Options`, `BasePayload`, `ReadSource`) and
`internal/secret/detect`. It must not import `internal/parser`, collector,
storage, query, projector, or reducer packages.

## Telemetry

None of its own. The collector's `eshu_dp_collector_content_store_files_total`
counter (`policy=metadata`) and the `git content store policy applied` log line
cover the metadata rows. A missing registry row means the line was skipped by one
of the rules above.

## Gotchas / invariants

- Never read, log, or copy a value from any other key.
- Never add `source` to the payload.
- Build error text without the input.
- `FuzzParseNeverEmitsACredential` appends seeded credential lines to a fuzzed
  body and fails if a seed reaches the payload. Run it for a while after a
  change here.
