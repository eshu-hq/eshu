# .eshuignore Guide

The `.eshuignore` file is the plain ignore-file contract for Eshu indexing. Use
it for repo-local generated files, local state, fixtures, or other paths that
should not enter discovery. For repo-scale tuning that needs explicit reasons,
preserve rules, and `user:<reason>` skip metrics, prefer
`.eshu/discovery.json`.

## What Eshu already ignores

You do not need `.eshuignore` entries for the default discovery skips. Eshu
prunes these directory names before parser matching:

```text
.git
.svn
.hg
.eshu
.terraform
.terragrunt-cache
.tox
.mypy_cache
.pytest_cache
.aws-sam
cdk.out
.serverless
node_modules
bower_components
jspm_packages
.yarn
.next
.nuxt
site-packages
dist-packages
__pypackages__
__pycache__
.venv
venv
.eggs
vendor
wp-admin
wp-includes
bundle
deps
_build
Pods
.build
Carthage
.gradle
.m2
.ivy2
.stack-work
.cabal-sandbox
dist-newstyle
.dart_tool
.pub-cache
blib
local
packages
obj
bin
.ansible
ansible_collections
.jenkins
dist
build
target
out
coverage
.nyc_output
htmlcov
```

Eshu also skips these suffixes before parser matching:

```text
.log
.out
.min.js
.min.mjs
.min.css
.bundle.js
.chunk.js
.min.map
.map
.pnp.cjs
.pnp.loader.mjs
.pyc
.pyo
.class
.dll
.so
.dylib
.exe
.o
.a
.wasm
```

These paths do not enter normal snapshot parsing, fact emission, graph writes,
or finalization.

Use `.eshuignore` for repo-local choices that are specific to your project,
team, or indexing goals when a plain ignore pattern is enough. Be careful with
broad names like `vendor/`, `bin/`, `charts/`, or lockfiles; in some
repositories those are real, tracked inputs.

For checked-in third-party source trees that live outside the conventional
dependency directories above, prefer the reasoned discovery map at
`.eshu/discovery.json`. It prunes matching subtrees before descent and emits
`user:<reason>` skip metrics, which makes repo-scale performance tuning easier
to audit than a silent broad ignore pattern. Eshu still accepts the older
`.eshu/vendor-roots.json` shape for compatibility.
For the full evidence, config, and rerun loop, see
[Local Testing — Discovery Advisory Playbook](local-testing.md#discovery-advisory-playbook).

Example `.eshu/discovery.json`:

```json
{
  "ignored_path_globs": [
    {"path": "_old/**", "reason": "archived-site-copy"},
    {"path": "public/js/fotorama.js", "reason": "static-browser-library"}
  ],
  "preserved_path_globs": [
    {"path": "_old/custom-authored/**"}
  ]
}
```

## `.gitignore` Interaction

Eshu also honors the target repository's own `.gitignore` files during repo and
workspace indexing by default.

- Only `.gitignore` files inside the target repo are used.
- Parent workspace `.gitignore` files do not leak into sibling repos.
- Nested `.gitignore` files inside the repo apply within their subtree.
- Matching files are hard-excluded from repo/workspace ingest.

This means `.gitignore` is still useful for repo-local generated or published
assets, while `.eshuignore` remains the ESHU-specific override for additional
indexing choices. Dependency trees no longer need `.gitignore` or `.eshuignore`
entries just to keep them out of the default index.

## Why use it?

- **Performance:** Skip large generated trees that are not part of the code or infrastructure you want to analyze.
- **Relevance:** Keep the graph focused on the source, manifests, and configuration that matter.
- **Privacy:** Exclude local secrets, generated configs, or internal-only documents from the graph.

## File Specification

- **Filename:** `.eshuignore`
- **Location:** Place it at the root of the repository or mono-folder you index.
- **Syntax:** Eshu's ignore parser supports blank lines, `#` comments,
  `!` negation, leading `/` root anchoring, trailing `/` directory-only rules,
  literal path segments, and `*`, `?`, or `[]` glob patterns.

When Eshu indexes a file, it evaluates each `.eshuignore` from the repo root
down to the file's parent directory. Patterns are relative to the directory that
contains the ignore file, and later matching patterns can override earlier ones
with `!`.

## Example

Create a file named `.eshuignore` in your project root with only the
project-specific paths you want Eshu to skip:

```text
# Local state and generated outputs that are valid repo files but not useful graph input.
*.tfstate
*.tfstate.*
*.tfvars
*tfplan*
generated/
fixtures/large-vendor-copy/
public/assets/*.map

# Keep a hand-written fixture inside a broader generated directory.
!generated/fixture-source/
```

## IaC Note

If you work in Terraform, Terragrunt, Pulumi, Crossplane, CDK, or serverless
repos, Eshu already avoids the major local cache and build directories listed
above, such as `.terraform/`, `.terragrunt-cache/`, `.aws-sam/`, `cdk.out/`,
and `.serverless/`. Add `.eshuignore` or `.eshu/discovery.json` entries for
other tool-local trees such as `.pulumi/`, `.crossplane/`, or
`.terramate-cache/` when those paths are valid repo content but still not useful
to index.

## Related docs

- [CLI: Indexing & Management](cli-indexing.md)
- [Configuration & Settings](configuration.md)
- [Troubleshooting](troubleshooting.md)
