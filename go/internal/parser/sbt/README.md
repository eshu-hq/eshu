# sbt Parser

## Purpose

This package owns repository-side `build.sbt` dependency parsing. It turns each
build definition into `content_entity`-shaped dependency rows the supply-chain
impact reducer reads, plus the literal Scala version. It exists because the
language ledger names `build.sbt` as the Scala manifest and the registry did
not admit it (#8938).

## Ownership boundary

The package owns `build.sbt` scanning only. The parent parser package owns
registry dispatch, file discovery, and the engine wrapper
(`go/internal/parser/gradle_language.go`, beside the Gradle wrapper). The reducer owns matching emitted rows
to package-registry identities.

## Exported surface

- `Parse(path string, isDependency bool, options shared.Options) (map[string]any, error)`
- `MaxDependencyRows`, `MaxFieldBytes`, `LanguageName`

The payload follows `shared.BasePayload(path, "sbt", isDependency)`. The
`variables` bucket holds one row per literal coordinate:

- `name`: `organization:name`, exactly as written. For `%%` and `%%%` this is
  the unsuffixed name, not the published artifact name.
- `value`: the literal revision, the revision a top-level string `val`
  resolves to, or the raw identifier or interpolation text when unresolved.
- `section`: `libraryDependencies`, with `:<Config>` appended when the
  declaration names a configuration.
- `config_kind`: always `dependency`; `package_manager` and `lang`: `sbt`.
- `dependency_scope`: `compile`, `test`, `provided`, or `runtime`, mapped as
  described under "Scope mapping". The raw spelling stays in
  `sbt_configuration`. On a `partial` row the scope is a placeholder.
- `dependency_resolution_state`: `resolved` only for a statement that matches
  the grammar below, else `partial`; `unresolved` for a non-literal revision,
  with `dependency_unresolved_keys`. An Ivy dynamic revision (`1.+`,
  `latest.release`, any range such as `[1.0,2.0)` or `]1.0,2.0]`) is `partial`.
- `direct_dependency`: `true`; `dependency_path_kind`: `manifest`;
  `dependency_optional`: `true` for an `Optional` configuration.
- `sbt_cross_version`: `none` (`%`), `binary` (`%%`), `scalajs` (`%%%`), or
  `custom` (a `cross` modifier).
- `sbt_binary_artifact`: `organization:name_<binary>` for a `%%` row when the
  file holds exactly one literal `scalaVersion`, so a consumer can match the
  published artifact. Absent otherwise.

`sbt_state` carries `state`, `scala_version` (the first `ThisBuild`-scoped
literal, else the first literal, else empty), `scala_version_count`,
`dependency_count`, `dependency_overflow_count`, and
`unresolved_declaration_count`.

## Grammar

A row is `resolved` only when its whole statement matches this closed grammar
token for token. Comments and whitespace are free; a line break may sit
anywhere inside `(` and `[`, and after an operator token (`+=`, `%`) in the bare
file.

```ebnf
statement   = key ( "+=" element | ( "++=" | ":=" ) collection ) end ;
key         = "libraryDependencies" ;            (* bare: no axis, no in/.in *)
collection  = ( "Seq" | "List" | "Vector" ) "(" [ element { "," element } [ "," ] ] ")" ;
element     = string coordOp string "%" version [ "%" config ] { call } ;
coordOp     = "%" | "%%" | "%%%" ;
version     = string | interpolated | ident { "." ident } ;
config      = string | "Compile" | "Test" | "Runtime" | "Provided"
            | "Optional" | "IntegrationTest" ;
call        = flag "(" ")" | "exclude" "(" string "," string ")"
            | "excludeAll" group | "classifier" ( string | "(" string ")" )
            | "cross" ( group | ident { "." ident } [ group ] ) ;
flag        = "intransitive" | "withSources" | "withJavadoc" | "force" | "changing" ;
end         = EOF | ";" | newline-before-statement   (* bare file *)
            | "," | ")" ;                             (* settings argument *)
```

`string` is a plain or triple-quoted literal with no backslash; organization and name are at most
`MaxFieldBytes` and hold no space or `:`; the revision is not empty. `group` is
a balanced `(...)`. A statement is in a grammar context only in two places:

- **The bare file.** The key starts a top-level statement on a line whose
  first token is at column 0: it is the first token, follows `;`, or starts a
  line after a token that can end a statement (not an operator, an opener, a
  keyword such as `else` or `then`, or the closed condition of an `if`, `while`
  or `for`). Rows belong to the root project.
- **A project's settings.** The key is the first token of an argument of a
  `.settings(` call, at depth zero of a `val x = project...` statement whose
  initializer starts with `project`, `(project ...)`, `Project(...)`,
  `crossProject(...)` or `projectMatrix`, with any `.in(...)`, `.dependsOn(...)`
  style calls between. Rows belong to that project.

`+=` takes one element and `++=` and `:=` take a collection, as sbt's `Append`
instances type them.

sbt 2.x reads `build.sbt` as Scala 3, where a `val` or `def` body, an
`if`/`then`/`else` branch, a `match` case, or a `for`/`while` `do` body can be a
braceless block: an indented region at bracket depth zero. A statement in it is
not a top-level setting, so the bare file is the column-0 lines only. An
indented line at depth zero is never a grammar context, and a `val` is tracked
only when it heads a column-0 line (after modifiers such as `lazy`, `private`,
`final`, `override`, `implicit` or `inline`, or after `;`). Inside `(` and `[`
indentation does not matter, so a braceless project whose `.settings(` list
continues on indented lines still resolves.

### Fail-closed fallback

Anything that does not match is never resolved. Every literal coordinate
(`"org" op "name" % revision`) found in the statement is emitted with
`dependency_resolution_state` `partial`, `dependency_scope` `compile` as a
placeholder, and its literal fields as read, and it is counted in
`sbt_state.unresolved_declaration_count`. A statement with no coordinate is
counted once. The fallback reads to the end of the statement (a `,`, `;` or
closer outside its own groups, or a line break that ends the expression) and
then hands back to the main loop, so a following statement is read normally.

| Accepted (resolved, exact scope) | Partial (fallback) |
|---|---|
| `libraryDependencies += "a" %% "b" % "1"` | any axis: `Test /`, `ThisBuild /`, `Global /`, `core /`, `in Test`, `.in(Test)` |
| `... % Test`, `% "test"`, `% "it,test"`, `% Provided`, `% Optional` | `inConfig(...)`, `inTask`, `inScope`, `inThisBuild`, any `in<Name>(...)` |
| `++= Seq(` / `List(` / `Vector(` elements, trailing comma | a brace block, a closure, a `val`/`def` body, an `if` body |
| `:= Seq(...)` | `.settings { ... }`, `.settings({ ... })`, `.jvmSettings(...)`, a settings `Seq` |
| `% Test` or a call on the next line inside `(` | `% Test` leading the next line in the bare file (Scala 3 leading infix) |
| infix `exclude("o", "n")`, `excludeAll(...)`, `classifier "c"` | any dotted call, `("a" % "b" % "1") % Test`, a tuple |
| `intransitive()`, `withSources()`, `withJavadoc()`, `force()`, `changing()` | `withConfigurations`, `withRevision`, `withName`, `configure`, `from`, `artifacts`, `notTransitive`, `withCrossVersion`, an extension method, an unknown operator |
| `cross CrossVersion.full`, `cross(...)` | a repeated `% config`, a config identifier off the list (`% Custom`, `% testConfig`) |
| `lazy val core = project.settings(libraryDependencies += ...)` | `.map`, `.flatMap`, `.distinct`, `.filter`, `.filterNot`, `.toSeq`, `++ Seq(...)`, a lambda, `Seq[ModuleID](`, `Set(` |
| `lazy val core = project` then an indented `.settings(` list | any indented line at depth zero: a Scala 3 `if`/`then`/`else`, `def`, `val`, `match` case, `for`/`while` `do` body, or a comment before the key |
| `% "test"`, `% "it"`, `% "provided"` (exact case) | `% "Test"`, `% "Compile"`, `% "IT"`: Ivy names are case sensitive |
| `"""org"""` (no backslash) | any string with a backslash, triple-quoted too |

A configuration string the scope table cannot map (`% "e2e"`, `% ""`, `% ","`,
`% "plugin->default(compile)"`, over `MaxConfigBytes`) matches the grammar but
leaves that row `partial` with an unknown scope. A non-literal revision
(`Versions.cats`, `s"${v}"`, an unknown identifier) matches the grammar too:
that row is `unresolved` and its siblings stay resolved, because a String-typed
revision cannot change a configuration. A revision identifier followed by a
call or a `.` on the next line (`v("x")`, `v\n  .trim`) does not match.

### Assignments that supersede rows

`:=` replaces the key and `~=`, `-=` and `--=` transform or remove what came
before, so they supersede earlier rows of the same context: those rows are
`partial` and counted. A setting method applied to the key
(`libraryDependencies.transform(...)`, `.set(...)`, any member but the reads
`.value`, `.key`, `.scopedKey` and `.taskValue`) at the start of an expression
supersedes the same way. A reset in the bare file applies to the root project,
which may be any project the file declares, so it supersedes every project row
wherever it appears. A reset outside a grammar context (in a settings `val`, a
wrapper, a block, or under an axis) may be applied anywhere, so it supersedes
every row in the file. The operands of `--=` and `-=` are not dependencies and
emit no rows.

### Scope mapping

The scope comes from the element's single `% config`; without one it is
`compile`. sbt allows one configuration per module: `% config` requires a
module whose configurations are empty (`DependencyBuilders.moduleIDConfigurable`
in sbt librarymanagement), and `% ""` fails its non-empty check, so a second
`% config` or an empty one is a load error, not a replacement.

A configuration is a list split on `,` and `;`, and each part is judged by the
left side of a `->` mapping, matched exactly: Ivy configuration names are case
sensitive and sbt names its standard ones in lower case, so `"Test"` is not
the test configuration. A Configuration identifier maps to the name sbt gives
it (`Test` to `test`, `IntegrationTest` to `it`). `test`, `it` and
`test-internal` mean `test`; `runtime` and `runtime-internal` mean `runtime`;
`provided` means `provided`; `compile`, `optional` and `compile-internal` mean
`compile`. A list resolves by this table:

| Parts | `dependency_scope` | Why |
|---|---|---|
| all one scope (`"it,test"`) | that scope | no disagreement |
| any `compile` part (`"compile;test"`) | `compile` | a compile dependency is on every classpath |
| `runtime` without `compile` (`"runtime,test"`, `"runtime->runtime;test->test"`) | `runtime` | runtime is also on the test classpath, so the dependency is not compile |
| `provided` with `test`, no compile or runtime | `provided` | provided is on the test classpath |
| `runtime` with `provided` | undetermined | compile-time only plus run-time only: not one scope |

The compiler-tool configurations (`plugin`, `scala-tool`, `scala-doc-tool`) and
the artifact-type configurations (`docs`, `sources`, `pom`) are not classpath
scopes, so they are undetermined, the same way Gradle gives `annotationProcessor`
and `kapt` their own scope instead of folding them into `compile`. `addCompilerPlugin`
expands to `"plugin->default(compile)"` and is therefore undetermined.

## Bounds

One forward pass, no recursion, one token of pushback: work and allocation are
linear in the input for a 100 KB single line, 200k nested parentheses, 100k
dependency lines, a 10 MB configuration list, 200,000 repeated `% "test"`
operands, or 20,000 chained `.map` calls over 5,000 dependencies. The fallback
reads each token once and keeps only the coordinate prefix it is matching. A
configuration is resolved once per `% config`, and a string over
`MaxConfigBytes` (256) is not split or stored: the row is `partial`, and
`sbt_configuration` and `section` carry `(overlong)` instead. Superseding
assignments are applied in one pass at the end of the file. The lexer keeps one
byte per open bracket to know whether a line break sits inside `(`. Real configurations are a few bytes and the longest
common mapping is under 50. Rows cap at `MaxDependencyRows` (10,000); the excess is counted, not
stored. A coordinate field over `MaxFieldBytes` (512) is not a coordinate, and
an identifier chain over that length (a version reference or a configuration)
is consumed without being buffered and is not a coordinate. At most 1,024 top-level string vals and 64 distinct Scala versions are
tracked. A leading UTF-8 byte order mark is ignored.

## What this parser never does

- Evaluate Scala, run sbt, or read `project/*.sbt`, `plugins.sbt`,
  `Build.scala`, `Dependencies.scala`, or any sibling file.
- Resolve `.value` settings, interpolated strings, helper objects, or
  conditional (`if`) and block-bodied collections. Their literal coordinates
  are partial and counted in `unresolved_declaration_count`.
- Resolve a version through a `val` declared inside a block, braced or
  indented; only literal vals that head a column-0 line resolve, at the end of
  the file, so declaration order does not matter.
- Emit a row for `addSbtPlugin`, `dependsOn`, resolvers, or `crossScalaVersions`.

## Known limits

- `sbt_binary_artifact` is a hint derived from the file's single literal
  `scalaVersion`, not sbt's resolved value. It is a file-wide guess: a literal
  inside one project's settings also applies to the `%%` rows of every other
  project in the file; `crossScalaVersions` is ignored, so a cross-built
  project gets one suffix; a suffixed version (`2.13.0-M5`, `3.0.0-RC1`,
  `2.13.12-bin-typelevel-4`) gets no hint, because sbt does not always shorten
  it to the x.y form; and a Scala before 2.10 gets no hint, because sbt
  cross-built those with the full version (`_2.9.2`).
- A `val` declared inside a block is not tracked. When it shares a name with a
  top-level `val`, a reference inside that block resolves to the top-level
  value, not the block-local one; the block's own rows are partial anyway. A
  top-level `val` whose literal continues on the next line (`"1"` then
  `.concat("-M1")` or `+ "-M1"`) is not a literal, and a `val` after an
  annotation or a qualified modifier (`private[core]`) is not tracked.
- The column-0 rule is fail-closed: a top-level statement written indented, or
  with a comment before the key on its line, is partial even when sbt would
  read it as top level.
- Scala 2.12 (sbt 1) processes unicode escapes everywhere in the source before
  lexing, comments included; the scanner reads them only as a backslash inside
  a string, which makes that string opaque. An escaped line break inside a
  comment is still read as part of the comment.
- A settings value defined outside the file (`.settings(Common.settings)` from
  `project/*.scala`) and applied after a row can supersede it; the scanner
  cannot see it. A reset defined in the file, anywhere, is seen.
- A bare-file reset makes every project row partial, even a project that is
  not the root, because the scanner does not decide which project is the root.
- `ThisBuild /` and `Global /` keys and a declared-project axis (`core /`) are
  partial. sbt can read them through delegation, but the grammar keeps only the
  forms it can prove.
- Sbt rows do not yet join a registry package identity (#8961) and keep the
  line-keyed entity id (#8962).
