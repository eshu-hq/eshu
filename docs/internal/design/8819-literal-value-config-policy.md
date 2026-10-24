# Keep mise config out of the content store until `metadata` exists, then admit it as `metadata` (#8819)

Status: Proposed. No arbiter ruling exists on #8819 yet. This record gives a
recommendation and lists the points only the arbiter can settle under
[Decisions for the arbiter](#decisions-for-the-arbiter). It approves no code,
registry change or migration.

**Decision (recommended).** No `mise` configuration file is admitted today, and
none is admitted as plain text. Once #8812 ships the `metadata` content-store
policy and a reader for the `[tools]` table exists, the `mise` config family is
admitted as `metadata`: the collector parses the file, emits runtime
declarations, and stores an empty-body row. A file whose value map is open and
meant to hold environment values is never stored as `store` or as a key
denylist `redact`.

## Context

### What is true on `main` today

I looked up each path with `Registry.LookupByPath` in a throwaway test, removed
before commit, at the base commit `c664b72cb0`.

| Path | Resolves | Parser key | Note |
| --- | --- | --- | --- |
| `mise.toml`, `.mise.toml` | no | | pinned false by `go/internal/parser/registry_version_pins_test.go` (`TestDefaultRegistryKeepsCredentialBearingDotfilesOut`) |
| `mise.local.toml`, `.mise.local.toml`, `mise.dev.toml` | no | | no `.toml` extension is registered |
| `.config/mise.toml`, `.config/mise/config.toml`, `mise/config.toml`, `.mise/config.toml`, `mise/conf.d/a.toml` | no | | same |
| `.miserc.toml`, `mise.lock` | no | | |
| `.tool-versions` | yes | `raw_text` | admitted by #8882 as a version pin file |
| `.envrc` | no | | |
| `devbox.json`, `.devcontainer/devcontainer.json`, `renovate.json` | yes | `json` | admitted through the `.json` extension |
| `renovate.json5`, `tox.ini`, `phpunit.xml`, `.mvn/maven.config` | no | | #8922 admits the first two and withholds the others |
| `Taskfile.yml` | yes | `yaml` | admitted through `.yml` |

So the question for mise is not a stored-row cleanup, which is the #8813 case.
Nothing is stored. The question is what to admit later, and under which rule.

The registry comment and the parser scoped guide already state the rule for new
names: a name that is conventionally a credential or literal-environment carrier
(`.npmrc`, `.pypirc`, `.netrc`, `.env*`, `mise.toml`) stays out because the
content store has no write-time redaction (`go/internal/parser/registry_definitions.go`,
comment above the `raw_text` exact-name list; raw-text row of the language table
in `go/internal/parser/AGENTS.md`). The epic #8807 ruling says the same.

### What mise config can hold

From the official mise documentation
([configuration](https://mise.jdx.dev/configuration.html),
[environments](https://mise.jdx.dev/environments/),
[dev tools](https://mise.jdx.dev/dev-tools/)), read for this record:

- **Files.** Project files are `mise.toml`, `mise.local.toml`, `mise/config.toml`
  with `mise/conf.d/*.toml`, `.mise/config.toml`, `.config/mise.toml`,
  `.config/mise/mise.toml` and `.config/mise/config.toml`, plus `.tool-versions`.
  Dotfile variants are `.mise.toml` and `.mise.local.toml`. Environment files are
  `mise.<env>.toml` and `mise.<env>.local.toml`. `.miserc.toml` sets discovery
  options. `mise.lock` is the lockfile. The `*.local.toml` files are described as
  personal overrides kept out of version control.
- **Tables.** `[tools]`, `[env]`, `[tasks]`, `[settings]`, `[vars]`, `[hooks]`,
  and others including `[secrets.*]`.
- **`[env]`.** Accepts literal values (`NODE_ENV = "development"`), `_.file`,
  `_.path` and `_.source`. The names are arbitrary, so the table is an open
  schema.
- **`[tools]`.** A version string (`node = "24"`) or a table with a `version` key
  and options. `latest` appears as a request. The documentation page I read does
  not describe a list-of-versions form, `lts`, `system` or `sub-1`, so this
  record does not rely on them.
- **Secrets.** The documentation points to a separate secrets mechanism for
  secret values and offers `redact` to mask values in output. Neither stops a
  user from writing a literal secret into `[env]`.

### Why a key denylist cannot make it safe

The #8813 ruling for tfvars gave the reason: an open-schema file has no closed
set of secret keys, so a denylist cannot make a redacted body safe. `[env]` has
the same shape. I ran the migration 138 detector pattern (`hardcodedSecretSQLPattern`,
Go RE2 as a proxy for the Postgres engine, invented values) over lines shaped
like a mise file:

| Line | Detector matches |
| --- | --- |
| `NPM_TOKEN = "abcdef123456"` | yes |
| `STRIPE = "sk_live_abcdefgh1234"` | yes |
| `run = "curl -H 'Authorization: Bearer abcdef123456' ..."` (task body) | yes |
| `AWS_SECRET_ACCESS_KEY = "abcdef1234567890"` | **no** |
| `SERVICE_KEY = "abcdef1234567890"` | **no** |
| `DATABASE_URL = "postgres://app:hunter2hunter2@db.example.test/app"` | **no** |
| `run = "curl -u bot:hunter2hunter2 https://api.example.test"` | **no** |
| `GITHUB_TOKEN = "{{ env.GITHUB_TOKEN }}"` (template reference) | no |
| `node = "24"`, `API_BASE = "https://api.example.test"` | no (correct) |

The detector needs a keyword directly before the `=` or `:`. A compound name
such as `AWS_SECRET_ACCESS_KEY` slips past it. A mise `[env]` table is also the
file's main purpose, so compound names are the normal case. The `run` strings in
`[tasks]` are free shell text, which no key list covers either.

## Options

| Option | What it costs | Verdict |
| --- | --- | --- |
| A. Leave the files out for good | Teams that keep versions in `mise.toml` and not in `.tool-versions` give no runtime declaration. | Interim state only. |
| B. Admit as plain text under the scanner alone | Same bar #8922 applies to `tox.ini` and `Makefile`. The scanner misses compound names and userinfo URLs (table above), and `get_file_content` would return the body. | Rejected for mise, see decision 1 below. |
| C. `redact`, marker mode (key denylist) | `[env]` is open-schema, so the denylist is not exhaustive. | Rejected. |
| D. `redact`, skeleton mode | Needs a TOML skeleton redactor and a differential, as for tfvars (#8833). It keeps the committed-secret finding, but the finding already misses compound names, so the gain is small. The `[tools]` versions are all a declaration needs. | Rejected for v1. Reopen if a user needs text search of mise tasks. |
| E. `metadata`: parse, emit facts, store an empty-body row | Loses `get_file_content`, text search and the committed-secret finding for these files. Keeps the path in the tree and counts. | **Recommended**, once #8812 and a `[tools]` reader exist. |

## Decision

1. **Now.** Admit no mise file. `.tool-versions` stays the only mise-compatible
   source (it is a documented mise project file), so mise users who keep
   versions there are covered. The `mise.toml` and `.mise.toml` negative pin
   stays.
2. **After #8812 and a reader.** Admit these names under `metadata`, in one PR
   that also updates the negative pin on purpose:

   | Name or path | Treatment |
   | --- | --- |
   | `mise.toml`, `.mise.toml`, `mise.local.toml`, `.mise.local.toml` | `metadata`, read `[tools]` only |
   | `mise.<env>.toml`, `mise.<env>.local.toml` | `metadata`, read `[tools]` only |
   | `mise/config.toml`, `mise/conf.d/*.toml`, `.mise/config.toml`, `.config/mise.toml`, `.config/mise/mise.toml`, `.config/mise/config.toml` | `metadata`, read `[tools]` only |
   | `.miserc.toml` | not admitted: it holds discovery options, not versions |
   | `mise.lock` | not admitted in v1: resolved versions are not declarations, and I did not verify that it is free of URLs and credentials |
   | global and system config | out of scope: not in a repository |

3. **Scope of the reader.** It reads `[tools]` and ignores `[env]`, `[tasks]`,
   `[vars]`, `[settings]` and `[hooks]`. It never resolves `latest` or any alias
   offline. An alias or an unresolved spec is reported as such, as the epic
   already rules.
4. **Binding to #8812.** The policy value is `metadata`. The collector parses
   the source bytes, then writes the content record with an empty body and the
   source digest, with no `content_file_secret_lines`, `content_file_references`
   or `source_cache` for the path. The acceptance tests of #8812 for `metadata`
   (seeded credential never reaches a stored column, digest before policy,
   failure goes to an empty row) are the tests for these names too.
5. **Binding to the runtime-declaration sources.** The `source_kind` is a new
   open-set string, for example `mise_toml`, carried on `runtime_declaration.version`
   (#8815). A mise declaration needs `relative_path`, `scope_dir`, `line_number`,
   `raw_spec`, `spec_kind` and `runtime` as for every other source.
6. **General rule for similar files.**

   | Class of file | Rule |
   | --- | --- |
   | Closed-schema file with a few credential fields (`renovate.json` `hostRules[].token`, `.mvn/maven.config`) | `redact`, marker mode, when a body is worth storing |
   | Open-schema file meant to hold environment values (mise `[env]`, `.envrc`, `.env*`, `devbox.json` `env`) | never `store`; `metadata` if a fact source needs it; otherwise not admitted |
   | Open-schema file that cannot be a fact source but is worth searching (tfvars) | `redact`, skeleton mode, after a differential |
   | No credential field and no value map | `store` |

### Revised runtime-declaration source list

This is the update the acceptance criteria ask for. #8807 and #8815 carry the
v1 list, which I have not edited (issue bodies are the coordinator's to change).

| Source | Phase | Content-store policy |
| --- | --- | --- |
| Pin files (`.nvmrc`, `.node-version`, `.tool-versions`, `.python-version`, `.ruby-version`, `.java-version`, `.go-version`, `.sdkmanrc`, `.terraform-version`, `rust-toolchain*`) | v1 | `store` (admitted by #8882) |
| `package.json` `engines`, `volta`, `packageManager` | v1 | `store` |
| `go.mod` `go` and `toolchain` | v1 | `store` |
| **mise `[tools]`** | **later, after #8812** | **`metadata`** |
| Dockerfile `FROM`, CI setup inputs | later (#8818) | `store` |

## Arbiter ruling of 2026-10-24

The epic #8807 policy ruling keeps this record in line with #8922: `mise.toml`
stays unadmitted, and `phpunit.xml` and `.mvn/maven.config` are routed to #8812.
It also fixes the order: #8922 merges, then #8812, then #8833. #8812 waits for
#8922 because both edit `go/internal/parser/registry.go` and
`registry_definitions.go`. Any later PR that admits a mise name changes the
bindings, so it rebases onto the then-current content version, takes the next
free one, reruns `go test ./go/internal/collector/repo/git -run TestGenerationContentVersionPinsCollectorOutput -update-content-version-pin`
and `scripts/generate-parser-admission-doc.sh`, and commits the regenerated
files in its own commit. The ruling does not settle the three open points below.

## Consistency with the open #8922 (#8814)

PR #8922 was read at head `c80484cfe9`, state open and mergeable. This record
agrees with it on every name it covers and differs on none, with two points the
arbiter should see.

| Point in #8922 | This record |
| --- | --- |
| `mise.toml` stays unadmitted | Same. The pin that enforces it is in `registry_version_pins_test.go` from #8882. The #8922 diff only keeps the registry comment that names `mise.toml`. |
| `phpunit.xml`, `phpunit.xml.dist`, `.mvn/maven.config`, `.mvn/jvm.config` withheld, routed to #8812 | Same. They stay out until a policy exists. See decision 2 on their `redact` mode. |
| `Taskfile.yml` unchanged (already L2 YAML) | Same. Its `env` and `vars` maps are already stored through `.yml`; this is an existing exposure of the kind decision 1 asks about. |
| `renovate.json5` admitted, `renovate.json` already admitted | Both are closed-schema `redact` candidates (#8812 comment). Both are stored verbatim today, so they carry the #8813 stored-row exposure until #8812 binds them. |
| `tox.ini`, `pytest.ini`, `Makefile` admitted on the scanner alone | This is the bar decision 1 asks about. |

## Consequences

Good:

- No new body of an environment file reaches the store, and no negative pin
  changes before a policy exists.
- A mise user gets declared runtime versions without exposing `[env]`.
- One rule table covers `.envrc`, `devbox.json` and similar names without a
  fresh review each time.

Bad:

- `get_file_content` and text search never serve mise config.
- A literal secret committed in a mise `[env]` table is not reported by
  `investigate_hardcoded_secrets`, because no body or secret line is stored.
  The detector would miss compound names anyway (table above).
- A mise-only team has no runtime declaration until the reader and #8812 land.

Follow-up issues to file when the arbiter agrees: the `[tools]` reader and the
registry admission change (depends on #8812 and #8815); the existing
`devbox.json` and `.devcontainer/devcontainer.json` value maps, stored today
through `.json`, under the rule table.

## Implementation plan

When the dependencies land:

| Path | Change |
| --- | --- |
| `go/internal/parser/registry_definitions.go` | New definition with `ContentStore: metadata` and the name list above |
| `go/internal/parser/registry_version_pins_test.go` | Replace the `mise.toml` negative pin with the new positive and `metadata` pin, in the same commit |
| New reader under the parser tree | `[tools]` only, string and table forms, with fixtures that hold a seeded `[env]` secret |
| `docs/public/languages/admitted-files.md` | Regenerate with `bash scripts/generate-parser-admission-doc.sh` |

Avoid reading `[env]` or `[tasks]` into any fact, and avoid resolving `latest`.
Telemetry is the one #8812 defines: rows written by policy and redactor or
parser failures by file kind.

## Acceptance criteria

From #8819:

- [x] The decision names the files it covers and the rule for each: the table
  under Decision, item 2, and the class table in item 6.
- [x] It depends on the content-store admission issue and says how: #8812
  provides `metadata`, and no mise name is admitted before it lands (items 1, 2
  and 4).
- [x] The runtime-declaration source list is updated to match: the revised
  table above. Applying it to the #8807 and #8815 bodies is left to the owner.

For the later implementation:

- [ ] A fixture `mise.toml` with a seeded `[env]` secret and a `[tools]` table
  yields `runtime_declaration.version` facts and a `content_files` row whose
  `content`, `content_lower` and `line_count` are empty, with no secret-line,
  reference or `source_cache` row.
- [ ] `latest` and any alias in `[tools]` report `alias` or `unresolved`, never a
  guessed version.
- [ ] `mise.toml` resolves `metadata` and no other mise-named path resolves
  `store` (a table test over the names above).
- [ ] The registry fingerprint moves and the PR body says every repository
  re-collects once.

## Decisions for the arbiter

1. **Is scanner-only enough for `[env]`-style maps already admitted?** #8922
   admits `tox.ini` (it can set variables with `setenv`), `pytest.ini` and
   `Makefile` on the scanner alone, calling them the same class as `.cfg` and
   `.conf`. The scanner missed compound key names and userinfo URLs in my sample
   above. Options: (a) accept the scanner bar for those names and treat mise as
   the exception because its `[env]` table is its main purpose; (b) apply
   decision 6's rule to them too and move them to `metadata` or `redact` after
   #8812; (c) accept both, and record that the existing admissions carry a known
   gap until #8833. I recommend (a) with a note on #8812, but this record cannot
   say whether the difference in purpose is enough.
2. **`phpunit.xml` in marker mode.** The #8812 comment lists `phpunit.xml` as a
   `redact` (marker mode) candidate gated on the #8833 userinfo detector. Its
   `<php><env name="..." value="..."/>` elements are an open-name map, which is
   the property that ruled out a key denylist for tfvars and for mise. Options:
   (a) keep marker mode; (b) use `metadata`; (c) use skeleton mode. No fact
   source reads the file today, so (b) costs nothing now.
3. **Is `metadata` right for the `*.local.toml` names?** They are described as
   kept out of version control, so one that reaches the index was committed by
   mistake and probably holds the personal values. Options: treat like the rest
   (`metadata`) or exclude them so no row exists. A committed-secret finding is
   lost either way.

<details>
<summary>Evidence and measurements</summary>

| Command | Result |
| --- | --- |
| Throwaway Go test calling `DefaultRegistry().LookupByPath` on 25 paths, removed after the run | Table in Context. |
| Scratch Go program applying `(?i)` plus the `hardcodedSecretSQLPattern` text to 11 invented lines | Table in Context. RE2, not Postgres ARE. |
| `gh pr view 8922`, `gh pr diff 8922` | Head `c80484cfe9`, open, mergeable. Mise appears only in a kept comment. |
| Fetches of the three mise documentation pages | Facts in Context. |

Not checked: a parse of a real `mise.toml`, the documentation of `[tasks]`
and `[secrets.*]` in depth, and whether `mise.lock` can hold URLs or
credentials.

</details>
