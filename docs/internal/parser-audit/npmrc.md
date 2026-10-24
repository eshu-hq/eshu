# Npmrc Parser Audit

## Overview
Parses npm `.npmrc` files (#8978) into registry-URL rows only. This is a **config** parser, not a language parser. An `.npmrc` mixes registry URLs with credentials, so the parser reads only `registry=` and `@scope:registry=` lines, removes userinfo, query, and fragment from each value, and drops a line it cannot prove clean. The file is admitted under the `metadata` content-store policy, which stores an empty-body row; parser output is outside that policy, so the parser carries the safety. Source files: `doc.go`, `parser.go`. Test files: `parser_test.go`, `parser_fuzz_test.go`. The two regular expressions are bounded (`scopeKeyPattern`, `hostPattern`) and run on lines already capped at 8 KiB.

## Claimed Constructs
From `doc.go`, `README.md`, `parser.go`:
- **Default registry**: `registry=<url>` becomes one `config_kind=registry_url` row.
- **Scoped registry**: `@scope:registry=<url>` becomes a row with `registry_scope`.
- **Userinfo, query, fragment removal**: `user:pass@`, `?token=`, and `#frag` never reach a field.
- **Value forms**: quoted values, inline `;` and `#` comments, spaces around `=`, CRLF, and a leading byte order mark.
- **Hosts**: DNS-style hosts (lower-cased), explicit ports, and bracketed IPv6 literals.
- **Skipped lines**: other keys, ini sections, `${...}` expansion, non-http schemes, scheme-less or host-less values, control or non-ASCII bytes, lines over 8 KiB, paths over 512 bytes, and URLs the shared detector flags.
- **Repeated keys**: every line is kept, in file order.

## Verified-by-Test Constructs
- `TestParseEmitsRegistryRows`: twelve accepted shapes with exact name, value, scheme, host, path, scope, and line.
- `TestParseEmitsNothingForLinesThatAreNotARegistryURL`: 29 rejected shapes, including detector shapes in the path.
- `TestParseKeepsLineNumbersAcrossSkippedLines`: line numbers count skipped lines.
- `TestParseNeverEmitsSource`: no `source` key with `IndexSource`.
- `TestParseNeverEmitsACredentialValue`: nine seeds across credential keys and the registry line's userinfo, query, and fragment; none reaches the payload, and no credential key name does either.
- `FuzzParseNeverEmitsACredential`: seeded credential tail on a fuzzed body; every emitted URL re-parses with no userinfo, query, or fragment.
- `TestEngineParsesNpmrcRegistryURLsThroughTheRegistry` and `TestDefaultRegistryAdmitsNpmrcUnderTheMetadataPolicy` (parent package): exact-name admission at any depth, the metadata binding, no shared-definition binding.
- `TestNpmrcSeededCredentialReachesNoFactUnderTheProductionBinding` (`internal/collector/repo/git`): full-stream scan of every fact for the root and a nested `.npmrc`, empty-body content row with the source digest, registry-URL entities beside it, empty `source_cache`.

## Known Gaps
- A token carried as a plain URL path segment with no `key=` or known value shape is emitted as path; the shared detector cannot recognize it.
- `${NPM_REGISTRY}` and other environment-expanded values yield no row.
- `registry` keys under an ini `[section]` are skipped.
- No `.npmrc` key other than the two registry shapes is parsed, so settings such as `always-auth` or `strict-ssl` are not reported.
