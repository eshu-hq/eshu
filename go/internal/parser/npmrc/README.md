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
| `registry_path` | the escaped URL path, possibly empty |
| `registry_scope` | `@scope`, only on a scoped row |
| `config_kind` | `registry_url` |
| `section` | `registry` or `scope_registry` |
| `line_number` | the source line, counting blank, comment, and credential lines |
| `package_manager` | `npm` |
| `lang`, `lockfile` | `npmrc`, `false` |

A repeated key keeps every line, in file order. The payload never carries
`source`, whatever `Options.IndexSource` says.

## A line is skipped, never partly emitted

The parser skips the line when the key is not `registry` or `@scope:registry`;
the line sits under an ini `[section]`; the value is empty, holds `${...}`, a
control byte, or a non-ASCII byte, or is longer than the 8 KiB line bound; the
value is not an absolute `http` or `https` URL with a DNS-style or IPv6 host; the
path is longer than 512 bytes; or the shared detector
(`internal/secret/detect`) flags the sanitized URL. Parse errors never reach the
caller or a log, because `net/url` errors quote the input.

## Residual risk

A token placed in a URL path as a plain segment with no `key=` shape or known
value shape (`https://host/t/Zk93...`) is not recognizable by the detector and is
emitted as path. The userinfo and query string are the credential positions npm
documents, and both are removed.

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
