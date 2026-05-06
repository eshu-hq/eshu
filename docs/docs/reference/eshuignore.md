# .eshuignore Guide

The `.eshuignore` file is the simple, `.gitignore`-style exclusion contract for
Eshu indexing. For performance tuning that needs auditable
reasons and skip telemetry, prefer `.eshu/discovery.json`.

## What Eshu already ignores

You do not need to add common cache trees just to protect the indexer from them.
Eshu already prunes hidden and configured cache directories before descent,
including:

- `.git/`
- `.terraform/`
- `.terragrunt-cache/`
- `.terramate-cache/`
- `.pulumi/`
- `.crossplane/`
- `.serverless/`
- `.aws-sam/`
- `cdk.out/`

Eshu also excludes built-in dependency roots before parse by default:

- JavaScript and TypeScript: `node_modules/`, `bower_components/`,
  `jspm_packages/`
- Python: `site-packages/`, `dist-packages/`, `__pypackages__/`
- PHP and Go: `vendor/`
- Ruby: `vendor/bundle/`
- Elixir: `deps/`
- Swift ecosystem: `Carthage/Checkouts/`, `.build/checkouts/`, `Pods/`

These directories do not enter checkpoints, Neo4j, Postgres, or finalization.
If you need dependency internals, load them explicitly with a `.eshu` bundle
instead of relying on routine repo indexing.

Use `.eshuignore` for repo-local choices that are specific to your project,
team, or indexing goals when a plain ignore pattern is enough.

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
workspace indexing by default (`ESHU_HONOR_GITIGNORE=true`).

- Only `.gitignore` files inside the target repo are used.
- Parent workspace `.gitignore` files do not leak into sibling repos.
- Nested `.gitignore` files inside the repo still apply within their subtree.
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
- **Syntax:** Standard `.gitignore`-style glob patterns.

When Eshu indexes a directory, it walks upward to find the nearest `.eshuignore` and applies patterns relative to that file.

## Recommended Example

Create a file named `.eshuignore` in your project root with content like this:

```text
# Python
__pycache__/
*.py[cod]
.venv/
.pytest_cache/
.mypy_cache/
.ruff_cache/

# JavaScript / TypeScript
node_modules/
.pnpm-store/
.parcel-cache/
*.tsbuildinfo

# Elixir / Dart / Haskell
_build/
.elixir_ls/
.dart_tool/
.stack-work/

# Minified and bundled assets
*.min.js
*.min.css
*.min.json
*.bundle.js
*.chunk.js
*.js.map
*.css.map

# Terraform and local state
.terraform/
*.tfstate
*.tfstate.*
*.tfvars
*tfplan*
charts/*.tgz

# General generated output
dist/
build/
out/
coverage/
*.log
*.tmp
```

Prefer cache, build, minified, and local-state artifacts. Be careful with broad
top-level names like `vendor/`, `bin/`, `charts/`, or lockfiles unless you are
certain they are generated in your repo. In many ecosystems those can be real,
tracked source inputs.

## IaC Note

If you work in Terraform, Terragrunt, Pulumi, Crossplane, CDK, or serverless
repos, Eshu already avoids the major local cache and build directories listed
above. Add `.eshuignore` entries only for files that are valid repo content but
still not useful to index, such as generated manifests, rendered templates, or
local state files.

## Related docs

- [CLI: Indexing & Management](cli-indexing.md)
- [Configuration & Settings](configuration.md)
- [Troubleshooting](troubleshooting.md)
