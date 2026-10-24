# AGENTS.md - internal/parser/npmrc

## Read first

1. `README.md` - purpose, row contract, the skip rules, the residual risk.
2. `doc.go` - godoc contract for parent parser callers.
3. `parser.go` - `Parse`, `registryRow`, `registryValue`, `sanitizeRegistryURL`;
   `path_guard.go` - `registryPathAllowed`, the path allowlist.
4. `parser_test.go` - accepted and rejected shapes, the seeded-credential scan.
5. `parser_fuzz_test.go` - the seeded-credential fuzz target.
6. Parent wrapper `parseNpmrc` in `../node_lockfile_language.go`, the registry definition in
   `../registry_definitions.go`, and the collector proof
   `go/internal/collector/repo/git/content_store_policy_npmrc_test.go`.

## Invariants this package enforces

- The parser emits registry URLs only. It MUST NOT read, copy, count, or log the
  value of `_authToken`, `_auth`, `_password`, `username`, `email`, or any other
  key. The `metadata` content-store policy does not govern parser output
  (`TestParserOutputIsNotGovernedKnownLeak`), so this package is the only guard.
- A row's URL MUST have no userinfo, query, or fragment, and MUST be an absolute
  `http` or `https` URL. A line that fails any check is skipped whole.
- The path is an allowlist of documented layouts (`pathLayouts`). A path off it,
  and every path on `*.fury.io`, MUST be dropped (host-only row with
  `registry_path_withheld`). A new layout needs the vendor documentation that
  puts the credential outside the path, a positive test, and a token-in-slot
  negative test.
- A line whose value text holds an `@` that `net/url` did not consume as the
  userinfo delimiter MUST be dropped whole (`sanitizeRegistryURL` `atCount`).
- The payload MUST NOT carry `source`, even with `Options.IndexSource`.
- No error or log may quote file text. `net/url` errors quote their input, so
  they are discarded.
- Do not import `internal/parser` or any collector, storage, or reducer package.
- Keep `.npmrc` its own definition. Never add the name to the `hcl`, `yaml`, or
  `raw_text` definitions, and never bind it to `store` (a default test pins the
  `metadata` policy).

## Common changes

- A new accepted key (for example `@scope:registry` variants) belongs in
  `registryRow` with a negative test that a credential key beside it still emits
  nothing, and a seed in `credentialSeeds`.
- A new row field belongs in `registryRow` and the README row table; confirm it
  cannot carry anything from a credential position.
- Re-run `go test ./internal/parser/npmrc -run xxx -fuzz FuzzParseNeverEmitsACredential -fuzztime 60s`
  after any parsing change.

## Failure modes

- Missing registry rows: the line failed a skip rule. Check for `${...}`, a
  scheme-less value, a `[section]` above it, a stray `@`, or a URL the detector
  flags.
- A host-only row: the path is off the allowlist; see `pathLayouts`.
- A seed in a fact: stop. Find which row field copied it, add the seed to
  `credentialSeeds`, and fix the field before anything else.
