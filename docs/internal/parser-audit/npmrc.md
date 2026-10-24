# Npmrc Parser Audit

## Overview
Parses npm `.npmrc` files (#8978) into registry-URL rows only. This is a **config** parser, not a language parser. An `.npmrc` mixes registry URLs with credentials, so the parser reads only `registry=` and `@scope:registry=` lines, removes userinfo, query, and fragment from each value, and drops a line it cannot prove clean. The file is admitted under the `metadata` content-store policy, which stores an empty-body row; parser output is outside that policy, so the parser carries the safety. Source files: `doc.go`, `parser.go`, `path_guard.go`. Test files: `parser_test.go`, `parser_path_test.go`, `parser_userinfo_test.go`, `parser_fuzz_test.go`. The two regular expressions are bounded (`scopeKeyPattern`, `hostPattern`) and run on lines already capped at 8 KiB.

## Claimed Constructs
From `doc.go`, `README.md`, `parser.go`:
- **Default registry**: `registry=<url>` becomes one `config_kind=registry_url` row.
- **Scoped registry**: `@scope:registry=<url>` becomes a row with `registry_scope`.
- **Userinfo, query, fragment removal**: `user:pass@`, `?token=`, and `#frag` never reach a field.
- **Value forms**: quoted values, inline `;` and `#` comments, spaces around `=`, CRLF, and a leading byte order mark.
- **Hosts**: DNS-style hosts (lower-cased), explicit ports, and bracketed IPv6 literals.
- **Skipped lines**: other keys, ini sections, `${...}` expansion, non-http schemes, scheme-less or host-less values, control or non-ASCII bytes, lines over 8 KiB, paths over 512 bytes, and URLs the shared detector flags.
- **Repeated keys**: every line is kept, in file order.
- **Path allowlist**: the path is kept only on a documented token-free layout (Artifactory, Nexus, Azure Artifacts, GitLab, Gitea, Cloudsmith, Artifact Registry, CodeArtifact) with every variable slot shaped like a name, or when it is empty. Any other path and every `*.fury.io` path is dropped; the row keeps scheme and host and sets `registry_path_withheld`.
- **Split userinfo**: a line whose value text holds an `@` that `net/url` did not consume as the userinfo delimiter is dropped whole.
- **ini escapes**: `\;`, `\#`, and `\\` in an unquoted value are literal, as in npm's reader.

## Verified-by-Test Constructs
- `TestParseEmitsRegistryRows`: twelve accepted shapes with exact name, value, scheme, host, path, scope, and line.
- `TestParseEmitsNothingForLinesThatAreNotARegistryURL`: 26 rejected shapes.
- `TestParseKeepsDocumentedTokenFreeLayouts`: 18 documented vendor URLs keep their path.
- `TestParseWithholdsEveryTokenInAPathSlot`: seven token shapes, including the no-digit and underscore-split 20-byte tokens, in the Gemfury first segment and in every variable slot of every allowlisted layout; every one drops to a host-only row.
- `TestParseWithholdsAPathOffTheAllowlist`: 13 paths off the allowlist, including detector and encoded shapes.
- `TestParseDropsALineWhoseUserinfoSplitsOutsideTheAuthority`, `TestParseKeepsAWellFormedUserinfoLine`, and `TestParseReadsIniBackslashEscapes`: 13 split-userinfo forms drop whole, five ordinary userinfo forms keep a clean row, and the ini escapes follow npm.
- `TestParseKeepsLineNumbersAcrossSkippedLines`: line numbers count skipped lines.
- `TestParseNeverEmitsSource`: no `source` key with `IndexSource`.
- `TestParseNeverEmitsACredentialValue`: nine seeds across credential keys and the registry line's userinfo, query, and fragment; none reaches the payload, and no credential key name does either.
- `FuzzParseNeverEmitsACredential`: seeded credential tail on a fuzzed body; no emitted URL field holds an '@', '?', or '#', every emitted path is on the allowlist for its host, and the seeds include the split-userinfo forms and a token in the Gemfury and slot positions.
- `TestEngineParsesNpmrcRegistryURLsThroughTheRegistry` and `TestDefaultRegistryAdmitsNpmrcUnderTheMetadataPolicy` (parent package): exact-name admission at any depth, the metadata binding, no shared-definition binding.
- `TestNpmrcSeededCredentialReachesNoFactUnderTheProductionBinding` (`internal/collector/repo/git`): full-stream scan of every fact for the root and a nested `.npmrc`, empty-body content row with the source digest, registry-URL entities beside it, empty `source_cache`.

## Known Gaps
- A token that a vendor puts in a documented path slot, drawn as a short word or digits only, would be emitted. No documented vendor does.
- A private registry on a custom path layout reports its host only.
- `${NPM_REGISTRY}` and other environment-expanded values yield no row.
- `registry` keys under an ini `[section]` are skipped.
- No `.npmrc` key other than the two registry shapes is parsed, so settings such as `always-auth` or `strict-ssl` are not reported.
