# GoMod Parser Audit

## Overview
Parses Go module manifests (`go.mod`), module-checksum files (`go.sum`), and workspace files (`go.work`, #8823) using the official `golang.org/x/mod/modfile` parser. This is a **build-system manifest** parser — NOT a language parser. Emits dependency, replace, exclude, checksum, and workspace-use rows with explicit ambiguity states so the supply-chain reducer never admits checksum-only or workspace evidence as consumption. Source files: `doc.go`, `parser.go`, `gosum.go`, `gowork.go`. Test files: `parser_test.go`, `gowork_test.go`, `parsed_file_data_contract_test.go`. No regexp.MustCompile.

## Claimed Constructs
From `doc.go`, `README.md`, `parser.go`, `gowork.go`:
- **Module declaration**: module path, Go version from go.mod
- **require directives**: direct dependencies (config_kind=dependency), indirect dependencies (section=require-indirect)
- **replace directives**: replacement metadata on require rows (`resolved_module_path`, `resolved_version`, `replacement_path`, `replacement_version`), standalone replace rows (config_kind=dependency_replace)
- **exclude directives**: standalone exclude rows (config_kind=dependency_exclude)
- **go.sum checksums**: verbatim h1 hash, checksum_kind (module/gomod), always ambiguous=true
- **Local-path replaces**: `replace example.com/local => ../local` without inventing version
- **Version-specific replaces**: `replace foo v1.2.3 => ...` matches exact version
- **Malformed go.mod**: gomod_state envelope with state=malformed and parse_error string
- **Malformed go.sum**: scanner buffer overflow detected and surfaced as malformed state
- **go.work use directives**: one `config_kind=workspace_use` row per use entry, path kept verbatim (a blank path is named by its Go-quoted spelling), counted in `gomod_state.use_count`
- **go.work replace directives**: the go.mod `dependency_replace` row shape
- **go.work state**: `go_version`, `toolchain`, `use_count`, `replace_count`, `replaced_modules`, and no `module_path`
- **Malformed go.work**: same convention as go.mod (zero rows, malformed gomod_state with the upstream error)

## Verified-by-Test Constructs
- `TestParseGoModEmitsRequireDependencyRows` (`parser_test.go:16`): direct require, indirect require, section, package_manager, value, dependency_path, dependency_depth, lockfile flag
- `TestParseGoModResolvesReplaceDirectiveToEffectiveVersion` (`parser_test.go:80`): replace metadata on require row (replacement_path, replacement_version, resolved_module_path, resolved_version), standalone replace row with config_kind=dependency_replace
- `TestParseGoModResolvesLocalPathReplaceWithoutInventingVersion` (`parser_test.go:133`): local path replace, no version invented
- `TestParseGoModRecordsExcludeAsNonConsumptionRow` (`parser_test.go:169`): exclude row config_kind=dependency_exclude, value preserved, not admitted as dependency
- `TestParseGoModRecordsMalformedStateExplicitly` (`parser_test.go:203`): malformed go.mod surfaces gomod_state, no config_kind=dependency rows
- `TestParseGoSumEmitsAmbiguousChecksumRowsAndNoConsumption` (`parser_test.go:232`): go.sum rows have config_kind=dependency_checksum, ambiguous=true, checksum/checksum_kind preserved, no config_kind=dependency rows
- `TestParseGoSumSurfacesScannerErrorAsMalformedState` (`parser_test.go:283`): 2 MiB line triggers scanner error, gomod_state flips to malformed
- `TestParseRejectsUnknownFile` (`parser_test.go:311`): go.work.sum, go.mod.orig, and Gopkg.lock return an error
- `TestParseGoWorkListsMembersReplacesAndGoVersion` (`gowork_test.go:21`): use block and single-line use, comments, versioned and local-path replace, gomod_state values, no consumption rows
- `TestParseGoWorkEdgeShapes` (`gowork_test.go:121`): go/toolchain only, CRLF, empty file, godebug line and block (accepted, not recorded), and malformed input (require, unknown verb, ignore, extra use argument, unterminated block, invalid go version)
- `TestParseGoWorkKeepsUsePathsVerbatim` (`gowork_test.go:244`): `use ""`, quoted paths with spaces, and blank paths keep a row and count in use_count
- `TestGoWorkStateHasNoModulePath` (`gowork_test.go:294`) and the go.work case of `TestGomodStateRoundTripThroughTypedContract` (`parsed_file_data_contract_test.go:23`): no module_path and no module_declaration row
- Parent-level: `dependency_coverage_engine_test.go`, `registry_test.go`

## Unverified / Claimed-but-Untested Constructs
- **retract directives**: mentioned in README but no dedicated test
- **go.mod without module directive** (moduleRow is nil, but what does payload look like?)
- **Version-specific replace filter** (`replace foo v1.2.3 => bar v2.0.0`): the replace resolution logic is tested but "version-specific match wins over path-wide match" ordering is not explicitly tested
- **go directive in go.mod** (parsed but not tested for specific values)

## Edge Cases Considered
- `replace` with local filesystem path (no version invented)
- `exclude` not admitted as consumption (config_kind=dependency_exclude)
- go.sum rows always ambiguous (never admitted as consumption)
- Scanner buffer overflow (2 MiB line) detected as malformed
- Malformed go.mod returns payload with envelope, not error/panic
- Unsupported files (go.work.sum and other names) return an explicit error
- go.work with a blank or space-padded use path keeps the path verbatim
- go.work with CRLF line endings, an empty body, or only go/toolchain lines

## Edge Cases NOT Considered
- Empty go.mod
- go.mod with only a module directive and no require
- go.mod with only indirect dependencies
- Duplicate replace directives
- go.sum with mixed valid/invalid lines
- Very large go.sum files (performance)
- Toolchain directive (`toolchain go1.22.0` in newer go.mod format)
- godebug directive in go.mod (go.work godebug is pinned: accepted, not recorded)

## Verdict
**deep** — the named tests above cover the full surface: require (direct+indirect), replace (version+local path), exclude, malformed go.mod, go.sum (correct checksum rows, scanner errors), go.work (use, replace, state, malformed input), and file rejection. Tests verify config_kind distinctions that protect the consumption reducer. Parent-level coverage in dependency_coverage_engine_test.go confirms ecosystem wiring.

## Recommended Actions
- Document that GOMOD is a **permanent exception** — it uses `golang.org/x/mod/modfile`, not tree-sitter
- Add a test for `retract` directive handling
- Add a test for empty/minimal go.mod (only module directive)
- Add a test for version-specific replace winning over path-wide replace ordering
