# regexp.MustCompile Audit — go/internal/parser

**Date:** 2026-06-26 &emsp; **Epic:** #3531 (parser regex-to-AST migration)
**Scope:** All `regexp.MustCompile` sites in `go/internal/parser/**/*.go` excluding `_test.go` and `shared/`.
**Sites audited:** 121 across 33 files.

---

## Classification Scheme

| Class | Meaning |
|-------|---------|
| **keep** | Permanent justified exception. Regex is correct and appropriate — no migration needed. |
| **migrate** | Language grammar adapter where tree-sitter AST should/could handle this. |
| **out-of-scope** | Too complex, deferred, or requires an ADR before migration. |

---

## Audit Table

### 1. go/internal/parser/c/dead_code_roots.go (6 sites)

The C package already migrated its `cTypedefAliasPattern` from `parser.go` to AST (see `c/AGENTS.md`). The remaining regexes are all in `dead_code_roots.go` and operate on external header text or within-node initializer text — not primary symbol extraction.

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 25 | `cHeaderPrototypePattern` | Extract function prototypes from external `.h` files read via `os.ReadFile` | **keep** | Out-of-AST evidence: scans bytes of external local headers not part of the current tree-sitter parse. Bounded — no transitive include resolution. |
| 29 | `cBlockCommentPattern` | Strip `/* */` comments from external header text | **keep** | Text-strip helper for the out-of-AST header scan. |
| 31 | `cLineCommentPattern` | Strip `//` comments from external header text | **keep** | Same as above. |
| 33 | `cFunctionPointerTypedefPattern` | Extract function-pointer typedef names from raw source | **keep** | Whole-file scan to build alias set for `cDeclarationHasFunctionPointerTarget`. Dead-code-root evidence — not primary symbol extraction. |
| 37 | `cDirectInitializerTargetPattern` | Extract bare identifiers from `= &foo` initializers on declaration node text | **keep** | Within-node initializer evidence for function-pointer targets. Call-site evidence, not symbol extraction. |
| 41 | `cBraceInitializerPattern` | Extract brace-initializer contents for function-pointer table targets | **keep** | Same category as above. Initializer evidence over bounded node text. |

### 2. go/internal/parser/cpp/parser.go (1 site)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 15 | `cTypedefAliasPattern` | Fallback regex extraction of typedef alias name when tree-sitter field-based `declarator`/node-child walk fails | **migrate** | The C package already migrated its equivalent to AST (see `c/AGENTS.md`, issue #3573). The C++ `cTypedefBucket` and `cTypedefName` functions use this regex as a fallback when tree-sitter `type_definition` field access does not find the alias. Should follow the C migration pattern: walk `declarator` field → `type_identifier`/`function_declarator`/`array_declarator` instead. |

### 3. go/internal/parser/cpp/header_roots.go (5 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 27 | `cppFreeHeaderPrototypePattern` | Extract free function prototypes from external `.h` files | **keep** | Out-of-AST evidence: scans bytes of external local headers read via `os.ReadFile` in `AnnotatePublicHeaderRoots`. Bounded — only directly `#include`d headers inside repo root. |
| 34 | `cppClassBlockPattern` | Match class/struct bodies in external header text | **keep** | Same rationale. Scans external header text not part of the current translation unit's tree-sitter parse. |
| 40 | `cppClassMethodPrototypePattern` | Match member function prototypes within class body lines | **keep** | Same rationale. |
| 47 | `cppBlockCommentPattern` | Strip `/* */` comments from external header text | **keep** | Text-strip helper for the out-of-AST header scan. |
| 51 | `cppLineCommentPattern` | Strip `//` comments from external header text | **keep** | Same as above. |

### 4. go/internal/parser/cpp/dead_code_roots.go (3 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 34 | `cppFunctionPointerAliasPattern` | Extract function-pointer alias names from raw source (`using Alias = R (*)(...)` or `typedef R (*Alias)(...)`) | **keep** | Whole-file scan building alias set for dead-code roots. Tree-sitter declarator nesting varies across alias spellings — recovering only the alias identifier from bounded source text is owner-confirmation evidence, not primary extraction. |
| 44 | `cppDirectInitializerTargetPattern` | Extract bare identifiers from `= &foo` initializers on declaration node text | **keep** | Within-node initializer evidence scoped to a single declaration node. Call-site evidence. |
| 53 | `cppBraceInitializerPattern` | Extract brace-initializer contents for function-pointer table targets | **keep** | Same as above — initializer evidence scoped to one declaration node. |

### 5. go/internal/parser/rust/helpers.go (2 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 23 | `rustWhereClausePattern` | Split a signature-header string on ` where ` keyword | **keep** | Text helper over already-extracted node-text slice from an AST-located node. Not a raw-source symbol scanner. Works on `rustTrimWhereClause` which operates on signature text from a `function_item` node. |
| 24 | `rustIdentifierPattern` | Validate that a candidate token (`criterion_group!` target, etc.) is a bare Rust identifier | **keep** | Validation of a single token extracted from `rustMacroBody` output. Used only in `rustIdentifierOnly`. Not a source scanner. |

### 6. go/internal/parser/rust/macro_declarations.go (2 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 15 | `rustMacroModDeclarationPattern` | Extract `mod name;` declarations from unexpanded `macro_invocation` body text | **keep** | Tree-sitter does not expand macros — the body is an unparsed `token_tree`, not parsed AST. These extract `mod`/`use` from text the grammar does not model as symbol nodes. Rows tagged `macro_expansion_unavailable`. Same judgment class as Python embedded-shell work. |
| 16 | `rustMacroUseDeclarationPattern` | Extract `use path;` imports from unexpanded `macro_invocation` body text | **keep** | Same rationale as above. |

### 7. go/internal/parser/golang/embedded_shell.go (4 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 13 | `goExecImportPattern` | Detect `"os/exec"` imports with optional alias | **keep** | Behavioral evidence: detects os/exec import aliases in Go source text. Not primary symbol extraction — this is a bounded scan for a specific well-known package. |
| 14 | `goExecCallPattern` | Detect `alias.Command/CommandContext` call sites bounded to known aliases | **keep** | Behavioral evidence: matches only after confirming the alias is from `os/exec`. Bounded call-site scan. |
| 95 | `shortDeclaration` (dynamic `regexp.MustCompile`) | Check if a Go identifier was shadowed by `:=` before a given offset | **keep** | Shadow-check for os/exec alias. Performance note: compiles a new regex per call in `goIdentifierShadowedBeforeOffset`. Consider compiling/pre-compiling into a sync.Map cache keyed by `identifier` to avoid per-call compilation. |
| 96 | `varDeclaration` (dynamic `regexp.MustCompile`) | Check if a Go identifier was shadowed by `var` before a given offset | **keep** | Same as above. |

### 8. go/internal/parser/golang/embedded_sql.go (6 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 12 | `goFunctionPattern` | Find Go function boundaries in raw source for SQL-literal localization | **keep** | Behavioral evidence: locates function bodies to bound the SQL-literal scan. Not primary symbol extraction — structural traversal uses tree-sitter in the main Go adapter; this is the companion behavioral scanner. |
| 13 | `goSQLCallPattern` | Detect `db.Exec/Query/...` call sites near string literals | **keep** | Behavioral evidence: matches known SQL API method names on `.db`/`.tx` receiver calls. |
| 20 | `FROM/JOIN` pattern | Extract table names from SELECT/FROM clauses in SQL string literals | **keep** | Bounded SQL table-name extraction from string literal content. This is content analysis of SQL text embedded in Go strings — not Go parsing. Tree-sitter Go grammar has no SQL node type. |
| 26 | `UPDATE` pattern | Extract table names from UPDATE clauses in SQL string literals | **keep** | Same as above. |
| 31 | `INSERT INTO` pattern | Extract table names from INSERT clauses in SQL string literals | **keep** | Same as above. |
| 37 | `DELETE FROM` pattern | Extract table names from DELETE clauses in SQL string literals | **keep** | Same as above. |

### 9. go/internal/parser/haskell/helpers.go (1 site)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 18 | `haskellCallTokenPattern` | Match qualified and bare lower-case identifiers in a right-hand-side expression | **keep** | Documented permanent evidence exception per `haskell/AGENTS.md`. Operates on RHS text already extracted from the tree-sitter AST (after the `=` in a binding line), with strings and comments stripped. Reports bounded lexical call tokens, not resolved Haskell name binding. |

### 10. go/internal/parser/javascript/javascript_semantics_ast.go (2 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 29 | `javaScriptAWSClientServiceRe` | Extract AWS service slug from `@aws-sdk/client-<slug>` import specifier strings | **keep** | Within-string-content exception per `javascript/AGENTS.md`. Runs only against string values already isolated by `javaScriptImportModuleSpecifiers` (AST walk). The grammar has no sub-node for the slug portion of a scoped npm package name. |
| 34 | `javaScriptGCPServiceRe` | Extract GCP service slug from `@google-cloud/<slug>` import specifier strings | **keep** | Same justification category as the AWS regex. Input is AST-isolated string literal content. |

### 11. go/internal/parser/javascript/javascript_names.go (1 site)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 13 | `javaScriptStaticComputedMemberNameRe` | Validate that an unquoted computed-property-name string is a static member name (identifier chain or decimal integer) | **keep** | Content-classification over already-AST-isolated node text per `javascript/AGENTS.md`. The grammar does not distinguish static vs. dynamic computed properties as separate node types; the validator must run over the extracted string value after AST isolation. |

### 12. go/internal/parser/sql/migrations.go (6 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 18 | `/prisma/migrations/...` | Detect Prisma migration directory paths | **keep** | Path-based detection: matches file path patterns, not SQL source text. Tree-sitter is the wrong tool for path classification. |
| 19 | `/liquibase/` | Detect Liquibase changelog paths | **keep** | Same rationale. |
| 20 | `/changelog/` | Detect changelog paths | **keep** | Same rationale. |
| 21 | `/migrations/...`  `.up.sql` | Detect golang-migrate migration paths | **keep** | Same rationale. |
| 22 | `/migrations/` | Generic migration path fallback | **keep** | Same rationale. |
| 24 | `sqlFlywayFilename` | Detect Flyway migration filenames (`V1__description.sql`) | **keep** | Path/filename pattern for file classification. |

### 13. go/internal/parser/maven/parser.go (1 site)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 140 | `propertyReferencePattern` | Resolve `${property}` references in Maven dependency version strings | **keep** | Runs on already-XML-decoded string values from `pom.xml`. Tree-sitter cannot help — the input is a decoded XML document tree, not Maven source. Part of the Maven permanent manifest-parser exception (see `AGENTS.md` permanent exceptions table). |

### 14. go/internal/parser/nuget_project_language.go (1 site)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 16 | `msbuildPropertyReferencePattern` | Resolve `$(Property)` references in MSBuild/CSPROJ version strings | **keep** | Runs on already-XML-decoded string values. MSBuild `.csproj` is an XML manifest, not C# source. Part of the permanent manifest-parser exception. |

### 15. go/internal/parser/cloudformation/parser.go (2 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 15 | `awsResourceTypePattern` | Validate `AWS::Service::Resource` resource types in already-decoded YAML/JSON documents | **keep** | Runs on decoded map values, not raw source. Permanent exception — CloudFormation is declarative data decoded by YAML/JSON libraries. |
| 16 | `samResourceTypePattern` | Validate `AWS::Serverless::Resource` SAM resource types | **keep** | Same rationale. |

### 16. go/internal/parser/ruby/bundler_lockfile.go (3 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 13 | `bundlerLockSpecPattern` | Parse Gemfile.lock spec lines (`name (version)`) | **keep** | Line-oriented lockfile parsing. Gemfile.lock is a generated manifest, not Ruby source. |
| 14 | `bundlerLockDependencyPattern` | Parse Gemfile.lock dependency lines | **keep** | Same rationale. |
| 15 | `bundlerLockDirectPattern` | Parse Gemfile.lock DEPENDENCIES section lines | **keep** | Same rationale. |

### 17. go/internal/parser/ruby/bundler_gemfile.go (3 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 14 | `bundlerGroupBlockPattern` | Detect `group :name do` blocks in Gemfile | **keep** | Line-oriented DSL parsing. Gemfile is a Bundler manifest, not Ruby-grammar input per `ruby/AGENTS.md`. |
| 15 | `bundlerSourceBlockPattern` | Detect `source/git/path/github "..." do` blocks in Gemfile | **keep** | Same rationale. |
| 16 | `bundlerGroupValuePattern` | Parse `:symbol` or `"string"` group values | **keep** | Same rationale. |

### 18. go/internal/parser/ruby/bundler_blocks.go (1 site)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 14 | `rubyOpaqueBlockPattern` | Detect Ruby control-flow and DSL block openers (`if`, `unless`, `do`, etc.) for `end`-balancing in Gemfile parsing | **keep** | Part of the Bundler manifest scanner. Keeps its own line-oriented recognizers separate from the AST source parser. |

### 19. go/internal/parser/hcl/expression_helpers.go (6 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 15 | `localConfigFunctionStartPattern` | Detect `file()`/`templatefile()` calls in Terragrunt local assignments for config asset path extraction | **keep** | Bounded expression resolution over already-extracted HCL locals text. HCL uses the official `hcl/v2` library for primary parsing; these regexes handle Terragrunt expression-level convention text. |
| 16 | `localStringAssignmentPattern` | Parse `name = "value"` lines within locals blocks | **keep** | Part of `collectTerragruntLocalAssignments` — reads locals text to resolve `${local.name}` interpolations. |
| 17 | `localAssignmentStartPattern` | Detect start of multi-line `name = expression` assignments in locals blocks | **keep** | Same rationale. |
| 18 | `pathRelativeToIncludeSplitPattern` | Detect `split("/", path_relative_to_include())` patterns for indexed directory assignments | **keep** | Same rationale. |
| 19 | `quotedStringPattern` | Extract quoted-string values from Terragrunt expressions | **keep** | Same rationale. |
| 20 | `localInterpolationPattern` | Resolve `${local.name}` interpolations to their assigned values | **keep** | Same rationale. |

### 20. go/internal/parser/hcl/helpers.go (3 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 12 | `terragruntReadConfigPattern` | Extract paths from `read_terragrunt_config("...")` calls including nested `find_in_parent_folders` variants | **keep** | Terragrunt helper-path extraction from `.hcl` source text. Bounded static evidence. HCL primary parsing uses `hcl/v2`. |
| 13 | `terragruntFindInParentFoldersPattern` | Extract paths from `find_in_parent_folders("...")` calls | **keep** | Same rationale. |
| 14 | `terragruntIncludePathPattern` | Extract paths from `path = find_in_parent_folders("...")` include blocks | **keep** | Same rationale. |

### 21. go/internal/parser/gradle/blocks.go (1 site)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 16 | `blockHeaderAtCursor` | Match Gradle build-script block headers (`blockName {`) for dependency-block extraction | **keep** | Permanent exception: bounded regex/string scanner over Groovy/Kotlin DSL text. Gradle dependency extraction does not execute Gradle or evaluate the DSL. |

### 22. go/internal/parser/gradle/coordinate.go (3 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 18 | `coordinatePattern` | Validate Maven coordinate format (`group:artifact:version`) | **keep** | Coordinate validation over extracted string values. Combined with `strings.SplitN` for actual parsing; regex only guards validity. |
| 19 | `mapFormIndicator` | Detect map-form dependency declarations (`group:` or `name:` keywords) | **keep** | Content classification to avoid mis-treating `group: 'foo'` as a coordinate string. |
| 118 | Dynamic regex in `extractMapEntry` | Extract map entry values (`key: 'value'` or `key = 'value'`) | **keep** | Dynamic pattern construction from templates in `mapEntryPatterns`. Compiled per call in a bounded loop (2 calls per key lookup). Performance note: pre-compile the two template patterns and use `FindStringSubmatch` with substituted key, or cache compiled patterns keyed by map-key. |

### 23. go/internal/parser/gradle/scanner.go (7 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 12 | `defPattern` | Extract Groovy `def x = 'value'` scalar property declarations | **keep** | Bounded property extraction from preprocessed Gradle build-script text (comments and string interiors handled by `stripCommentsAndStringInteriorsKept`). |
| 13 | `extDotPattern` | Extract `ext.propertyName = 'value'` declarations | **keep** | Same rationale. |
| 14 | `extBlockHeader` | Locate `ext { ... }` blocks for property extraction | **keep** | Same rationale. |
| 15 | `extBlockEntry` | Extract `property = 'value'` entries within `ext {}` blocks | **keep** | Same rationale. |
| 16 | `valPattern` | Extract Kotlin `val/var x = 'value'` scalar declarations | **keep** | Same rationale. |
| 17 | `interpolationCurly` | Resolve `${property}` interpolations in version strings | **keep** | Version interpolation over already-extracted property map. |
| 18 | `interpolationBare` | Resolve `$property` bare interpolations in version strings | **keep** | Same. |

### 24. go/internal/parser/dbtsql/lineage.go (3 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 33 | `dbtSelectClauseRe` | Extract SELECT clause from compiled SQL for projection analysis | **keep** | Permanent exception: bounded SQL-lineage scanner over compiled dbt model text. Extracts bounded column lineage — this is lineage extraction, not source-grammar parsing. |
| 34 | `dbtAsAliasRe` | Parse `expression AS alias` in SELECT projections | **keep** | Same rationale. |
| 35 | `dbtFromRelationRe` | Parse relation references (`table`/`schema.table`/`db.schema.table`) and aliases from FROM/JOIN clauses | **keep** | Same rationale. |

### 25. go/internal/parser/dbtsql/identifiers.go (3 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 12 | `dbtIdentifierRe` | Extract bare SQL identifiers from expressions for unqualified reference resolution | **keep** | Bounded SQL identifier scanner. Runs on already-sanitized text (string literals blanked out). |
| 13 | `dbtSingleQuotedStringRe` | Match single-quoted string literals for sanitization | **keep** | String-literal sanitization helper. |
| 25 | Dynamic `regexp.MustCompile(".", ...)` | Replace all characters in string literals with spaces for sanitization | **keep** | Trivial "replace any character" pattern. Performance note: could use `strings.Repeat(" ", len(value))` for a speedup, but the regex `.` (without `s` flag) does not match `\n` while `strings.Repeat` would blank all bytes — verify equivalence before substituting. |

### 26. go/internal/parser/dbtsql/expressions.go (16 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 12 | `dbtBareIdentifierRe` | Validate that a string is a bare SQL identifier (`name`) | **keep** | Content classification in the bounded SQL lineage scanner. |
| 13 | `dbtQualifiedReferenceRe` | Validate `alias.column` or `alias.*` format | **keep** | Same rationale. |
| 14 | `dbtQualifiedReferenceScanRe` | Find all `alias.column` references in an expression | **keep** | Same rationale. |
| 15 | `dbtFunctionCallRe` | Parse `function(args)` shape at expression boundaries | **keep** | Same rationale. |
| 16 | `dbtFunctionCallScanRe` | Find `name(` patterns for unsupported-function detection | **keep** | Same rationale. SQL function-call detection — not a grammar rule tree-sitter would model independently from the surrounding SQL statement. |
| 17 | `dbtWindowFunctionRe` | Parse `function(args) OVER (window)` window function expressions | **keep** | Same rationale. |
| 18 | `dbtSingleQuotedLiteralRe` | Validate that a string is a single-quoted SQL literal | **keep** | Same rationale. |
| 19 | `dbtSingleQuotedLiteralScan` | Find all single-quoted literals for sanitization in collapsed-shape analysis | **keep** | Same rationale. |
| 20 | `dbtNumericLiteralRe` | Validate that a string is a numeric literal | **keep** | Same rationale. |
| 21 | `dbtNumericLiteralScan` | Find numeric literals for sanitization (3 capture groups: prefix, literal, suffix) | **keep** | Same rationale. |
| 22 | `dbtTypeIdentifierRe` | Find type identifiers in CAST expressions for exclusion from source-column detection | **keep** | Same rationale. |
| 23 | `dbtCaseExpressionRe` | Validate that an expression is a CASE ... END block | **keep** | Same rationale. |
| 24 | `dbtCaseKeywordRe` | Strip SQL keywords from collapsed-shape analysis | **keep** | Same rationale. |
| 25 | `dbtQualifiedMacroCallRe` | Parse `package.function(args)` macro call expressions | **keep** | Same rationale. |
| 241 | Dynamic `regexp.MustCompile("^[\s()=<>!,+\-*/%]*$", ...)` | Validate that collapsed expression shape contains only operators and whitespace (CASE expression shape check) | **keep** | Dynamic regex compiled per call to `isSupportedCaseExpression`. Pattern is constant; could be pre-compiled as a package-level var. |
| 253 | Dynamic `regexp.MustCompile("^[\s()+\-*/%]*$", ...)` | Validate that collapsed expression shape contains only arithmetic operators and whitespace | **keep** | Dynamic regex compiled per call to `isSupportedArithmeticExpression`. Same pre-compilation opportunity. |

### 27. go/internal/parser/dbtsql/expression_helpers.go (1 site)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 110 | Dynamic `regexp.MustCompile("\b" + token + "\b", ...)` | Replace reference tokens with `REF` in collapsed-shape analysis | **keep** | Dynamic regex in `replaceReferenceTokens` — compiles one regex per reference token per call. Performance note: the pattern is `\b` + `regexp.QuoteMeta(token)` + `\b`; could use `strings.ReplaceAll` with word-boundary-aware replacement or a single-pass replacer to avoid per-token compilation in the inner loop of `collapsedShape`. |

### 28. go/internal/parser/java/metadata.go (1 site)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 24 | `metadataClassNamePattern` | Validate fully-qualified Java class names in `META-INF/services/`, `spring.factories`, and Spring Boot auto-configuration files | **keep** | Permanent exception documented in `java/AGENTS.md`. Operates on plain-text resource files, not Java source — the tree-sitter Java grammar has no model for property-style or newline-delimited class-name list formats. |

### 29. go/internal/parser/elixir/hex_dependencies.go (6 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 15 | `mixManifestHexDependencyPattern` | Parse `{:app, "version", ...}` Hex dependency tuples in `mix.exs` | **keep** | Manifest parsing for Elixir dependency manifests. `mix.exs`/`mix.lock` are structured-format manifests, not Elixir source per `elixir/AGENTS.md`. |
| 16 | `mixManifestVCSDependencyPattern` | Parse `{:app, github: "repo", ...}` VCS dependency tuples in `mix.exs` | **keep** | Same rationale. |
| 17 | `mixLockHexPackagePattern` | Parse `"name": {:hex, :package, "version"}` in `mix.lock` | **keep** | Same rationale. |
| 18 | `mixLockNestedDependencyPattern` | Parse nested dependency entries in `mix.lock` | **keep** | Same rationale. |
| 143 | `mixDependencyOrganizationPattern` | Extract `organization: :name` / `organization: "name"` from dependency options | **keep** | Same rationale. |
| 144 | `mixDependencyHexPackagePattern` | Extract `hex: :name` / `hex: "name"` from dependency options | **keep** | Same rationale. |

### 30. go/internal/parser/groovy/entities.go (1 site)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 23 | `groovyJenkinsEntrypointPattern` | Detect `pipeline {` / `node {` block openers for Jenkinsfile entrypoint root annotation | **keep** | Permanent exception per `groovy/AGENTS.md`. The Groovy grammar parses these as method calls or block closures without exposing a dedicated `pipeline`/`node` root node type. DSL idiom detection, not primary symbol extraction. |

### 31. go/internal/parser/groovy/metadata.go (8 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 12 | `groovyLibraryPattern` | Extract shared library names from `@Library('name')` annotations | **keep** | Permanent exception per `groovy/AGENTS.md`. The Groovy grammar parses `@Library(...)` as an annotation node but does not expose the string argument as a named field identifying a shared-library reference. |
| 13 | `groovyLibraryStepPattern` | Extract library names from `library identifier: 'name'` step calls | **keep** | Same rationale. |
| 14 | `groovyPipelineCallPattern` | Extract pipeline step call names (`pipelineXxx(`) for delivery evidence | **keep** | Delivery evidence — captures pipeline step identity from source text. The AST layer already records calls in the `calls` bucket. |
| 15 | `groovyShellCommandPattern` | Extract shell commands from `sh 'command'` calls | **keep** | Same rationale. Shell command text is delivery evidence; the grammar provides no shell-command-typed node. |
| 16 | `groovyAnsiblePattern` | Detect `ansible-playbook` invocations within shell commands | **keep** | Content classification over already-extracted shell command strings. |
| 17 | `groovyEntryPointPattern` | Extract `entry_point: 'value'` named arguments | **keep** | Jenkins convention named-argument extraction. No semantic node type in the grammar. |
| 18 | `groovyUseConfigdPattern` | Detect `use_configd: true/false` named argument | **keep** | Same rationale. |
| 19 | `groovyPreDeployPattern` | Detect presence of `pre_deploy:` key | **keep** | Boolean presence flag detection. No grammar node type exists. |

### 32. go/internal/parser/scip_parser.go (3 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 228 | `scipTrailingCallRe` | Strip trailing `().` from SCIP symbol names | **keep** | SCIP symbol name manipulation. SCIP indexes are protobuf-encoded precomputed indices, not source code. Tree-sitter is the wrong tool for SCIP symbol string processing. |
| 233 | Dynamic `regexp.MustCompile("[/#]", ...)` | Split SCIP symbols on `/` and `#` separators | **keep** | Same rationale — SCIP symbol string splitting. Could use `strings.FieldsFunc` but the regex split is clear and once-per-symbol. |
| 273 | `regexp.MustCompile("\(([^)]*)\)", ...)` | Extract parenthesized parameter list from SCIP display names | **keep** | Same rationale. SCIP display-name signature parsing. |

### 33. go/internal/parser/templated_detection.go (9 sites)

| Line | Pattern | Purpose | Classification | Rationale |
|------|---------|---------|----------------|-----------|
| 19 | `goExpressionRE` | Detect Go template expressions (`{{ ... }}`) | **keep** | Content classification over possibly-invalid-grammar templated text. Classifies Go template, Jinja, GitHub Actions, and Terraform interpolation/directive markers. Input is frequently not valid in any single grammar, so tree-sitter cannot parse it. Permanent exception per `AGENTS.md`. |
| 20 | `jinjaStatementRE` | Detect Jinja statements (`{% %}`) and comments (`{# #}`) | **keep** | Same rationale. |
| 21 | `githubActionsExprRE` | Detect GitHub Actions expressions (`${{ }}`) | **keep** | Same rationale. |
| 22 | `goContextRE` | Detect Go template context accessors (`{{ .X }}` or `{{ $X }}`) | **keep** | Same rationale. |
| 23 | `goLineControlRE` | Detect Go template control structures (`{{ if }}`, `{{ range }}`, etc.) | **keep** | Same rationale. |
| 24 | `goHintRE` | Detect Go template-specific functions (`include`, `toYaml`, `nindent`, `tpl`) | **keep** | Same rationale. |
| 25 | `tfInterpolationRE` | Detect Terraform interpolations (`${`) | **keep** | Same rationale. |
| 26 | `tfDirectiveRE` | Detect Terraform directives (`%{`) | **keep** | Same rationale. |
| 27 | `tfTemplatefileRE` | Detect Terraform `templatefile()` calls | **keep** | Same rationale. |

---

## Summary Counts

| Classification | Count | Percentage |
|----------------|-------|------------|
| **keep** | 120 | 99.2% |
| **migrate** | 1 | 0.8% |
| **out-of-scope** | 0 | 0% |
| **Total** | **121** | 100% |

---

## Migration Priority

Only one site is classified as **migrate**:

| Priority | File | Line | Site | Effort | Notes |
|----------|------|------|------|--------|-------|
| **P1** | `cpp/parser.go` | 15 | `cTypedefAliasPattern` | Low | The C package already migrated its equivalent in issue #3573. The C++ `cTypedefBucket` and `cTypedefName` functions use this regex as a fallback when tree-sitter field-based `declarator` access fails. The migration follows the proven C pattern: walk `declarator` field → `type_identifier`/`function_declarator`/`array_declarator` instead of regex-matching node text. After confirming the declarator field walk covers all real-world forms, replace each fallback with a `panic` temporarily to prove the regex branches are dead code, then remove them. |

---

## Performance Notes

Three sites compile regexes dynamically at call time rather than at init time. These are low-priority performance improvements (not correctness issues):

| File | Line | Site | Issue |
|------|------|------|-------|
| `golang/embedded_shell.go` | 95-96 | `goIdentifierShadowedBeforeOffset` | Compiles `\b<identifier>\s*:=` and `\bvar\s+<identifier>\b` per call. Consider a `sync.Map` cache keyed by identifier. |
| `dbtsql/expressions.go` | 241, 253 | `isSupportedCaseExpression`, `isSupportedArithmeticExpression` | Compiles `^[\s()=<>!,+\-*/%]*$` and `^[\s()+\-*/%]*$` per call. These patterns are constant — promote to package-level `var`. |
| `dbtsql/expression_helpers.go` | 110 | `replaceReferenceTokens` | Compiles `\b<token>\b` per token in an inner loop. Consider `strings.ReplaceAll` with word-boundary-aware scanning. |
| `dbtsql/identifiers.go` | 25 | String-literal sanitization | Uses regex to replace every character. Consider `strings.Repeat` after verifying newline equivalence (`.` without `s` flag preserves `\n`). |

---

## Per-File Recommendations

### Permanent Exception Parsers — No Migration Needed

These files contain regexes that are canonical for their domain. Tree-sitter migration is not applicable because the inputs are declarative data formats (XML, YAML/JSON, lockfile text), build-script DSLs, lineage text, or content-classification targets without a grammar model.

| File | Category | Recommendation |
|------|----------|----------------|
| `maven/parser.go` | XML manifest | No migration. Property resolution operates on already-XML-decoded values. |
| `nuget_project_language.go` | XML manifest | No migration. MSBuild property resolution on XML-decoded values. |
| `cloudformation/parser.go` | Decoded template | No migration. Operates on decoded YAML/JSON map values. |
| `ruby/bundler_lockfile.go` | Lockfile | No migration. Line-oriented Gemfile.lock parsing. |
| `ruby/bundler_gemfile.go` | Manifest DSL | No migration. Line-oriented Gemfile DSL parsing. |
| `ruby/bundler_blocks.go` | Manifest helper | No migration. Block-balancing helper for Gemfile scanner. |
| `hcl/expression_helpers.go` | Terragrunt expressions | No migration. HCL uses `hcl/v2` for primary parsing; these handle Terragrunt convention text. |
| `hcl/helpers.go` | Terragrunt helpers | No migration. Bounded helper-path extraction. |
| `gradle/blocks.go` | Build-script scanner | No migration. Block detection in Gradle DSL text. |
| `gradle/coordinate.go` | Build-script scanner | No migration. Coordinate pattern matching. Pre-compile dynamic regex in `extractMapEntry`. |
| `gradle/scanner.go` | Build-script scanner | No migration. Property extraction from Gradle DSL text. |
| `dbtsql/lineage.go` | SQL lineage | No migration. Bounded SQL-lineage scanner over compiled model text. |
| `dbtsql/identifiers.go` | SQL lineage | No migration. SQL identifier scanning. Consider `strings.Repeat` for sanitization after verifying newline equivalence. |
| `dbtsql/expressions.go` | SQL lineage | No migration. SQL expression classification. Pre-compile dynamic patterns. |
| `dbtsql/expression_helpers.go` | SQL lineage | No migration. Reference-token replacement. |
| `java/metadata.go` | Resource file | No migration. Validates class names in META-INF/services, spring.factories, etc. |
| `elixir/hex_dependencies.go` | Manifest | No migration. `mix.exs`/`mix.lock` manifest parsing. |
| `groovy/entities.go` | Jenkins DSL | No migration. Jenkinsfile entrypoint detection. |
| `groovy/metadata.go` | Jenkins DSL | No migration. All 8 sites are delivery evidence, not symbol extraction. |
| `sql/migrations.go` | Path detection | No migration. File-path-based migration tool classification. |
| `templated_detection.go` | Content classification | No migration. Templated-text detection for possibly-invalid-grammar files. |
| `scip_parser.go` | SCIP index | No migration. SCIP protobuf symbol manipulation, not source parsing. |

### Language Adapters — Documented Permanent Exceptions

These files are for programming language parsers whose primary symbol extraction uses tree-sitter. Their remaining regexes are all documented within-node text, external-file text, or behavioral-evidence exceptions.

| File | Category | Recommendation |
|------|----------|----------------|
| `c/dead_code_roots.go` | Out-of-AST evidence | No migration. All 6 sites audit per issue #3573. External header text scans + within-node initializer evidence. |
| `cpp/header_roots.go` | Out-of-AST evidence | No migration. All 5 sites audit per issue #3574. External header text scans. |
| `cpp/dead_code_roots.go` | Within-node evidence | No migration. All 3 sites are within-node function-pointer/initializer evidence. |
| `cpp/parser.go` | **MIGRATE** | **`cTypedefAliasPattern` should follow C migration (#3573). See Migration Priority above.** |
| `rust/helpers.go` | Text helpers | No migration. Both sites are text helpers over already-AST-located node text. Audit per issue #3572. |
| `rust/macro_declarations.go` | Macro body text | No migration. Both sites extract from unexpanded `token_tree` text — tree-sitter does not expand macros. Audit per issue #3572. |
| `golang/embedded_shell.go` | Behavioral evidence | No migration. Bounded os/exec call-site detection. Pre-compile dynamic regex. |
| `golang/embedded_sql.go` | Behavioral evidence | No migration. Bounded SQL-literal table-name extraction from Go source. |
| `haskell/helpers.go` | Behavioral evidence | No migration. Documented permanent exception per `haskell/AGENTS.md`. Call-token extraction over AST-bounded RHS text. |
| `javascript/javascript_semantics_ast.go` | Within-string | No migration. Both sites are within-string-content regex over AST-isolated import specifiers. Audit per issue #3590. |
| `javascript/javascript_names.go` | Within-string | No migration. Content classification over already-AST-isolated node text. Audit per issue #3590. |

---

## Verification

The audit was conducted by reading the full source context (10+ lines before and after each match) for all 121 sites across 33 files, cross-referencing against each package's `AGENTS.md` file and prior audit records for issues #3572 (Rust), #3573 (C), #3574 (C++), #3575 (Groovy), #3588 (Haskell), and #3590 (JavaScript).

Prior audit commitments that are reflected here:
- **Rust** (#3572): 2 regexes migrated to AST (lifetimes, macro-definition name); 4 remain as documented exceptions (2 helpers.go + 2 macro_declarations.go) — confirmed.
- **C** (#3573): 1 regex migrated to AST (`cTypedefAliasPattern`); 6 remain in `dead_code_roots.go` — confirmed.
- **C++** (#3574): 1 regex migrated to AST (`cppQualifiedFunctionPattern`); 9 remain (1 in parser.go via `cTypedefAliasPattern`, 3 in `dead_code_roots.go`, 5 in `header_roots.go`) — confirmed, with the 1 parser.go site flagged for migration.
- **Groovy** (#3575): All 9 sites documented as templated-detection/delivery-evidence exceptions — confirmed.
- **Haskell** (#3588): 1 site (`haskellCallTokenPattern`) documented as permanent exception — confirmed.
- **JavaScript** (#3590): 3 sites documented as within-string-content exceptions — confirmed.

No undocumented regexes introduced post-audit.
