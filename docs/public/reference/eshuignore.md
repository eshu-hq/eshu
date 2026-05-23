# .eshuignore

`.eshuignore` is the repo-local ignore file for Eshu indexing. Use it when a
path is valid repository content but should not enter discovery, parsing, fact
emission, or graph projection for this repository.

Use `.eshu/discovery.json` instead when the ignore decision needs an operator
reason, a preserve override, or skip metrics that show up as `user:<reason>`.
The older `.eshu/vendor-roots.json` shape is still accepted for compatibility.

## Default skips

Most generated dependency and build trees do not need a `.eshuignore` entry.
The Git collector already prunes common cache, dependency, virtualenv, build,
coverage, and package-manager directories such as `.git`, `.terraform`,
`.terragrunt-cache`, `node_modules`, `vendor`, `dist`, `build`, `target`,
`coverage`, `Pods`, and `__pycache__`.

It also skips generated or binary-like suffixes such as `.log`, `.out`,
`.min.js`, `.min.css`, `.map`, `.pyc`, `.class`, `.dll`, `.so`, `.exe`, `.o`,
`.a`, and `.wasm`.

Before adding a broad ignore, check whether the path is already covered. Broad
names such as `vendor/`, `bin/`, `charts/`, and lockfiles can be real source
inputs in some repositories.

## Syntax

Place `.eshuignore` at the root of the repository or inside a subtree. Eshu
uses the same matcher for `.gitignore` and `.eshuignore`:

- blank lines and `#` comments are ignored
- `!` negates a previous match
- leading `/` anchors a rule to the directory containing the ignore file
- trailing `/` marks a directory-only rule
- literal path segments, `*`, `?`, and `[]` glob patterns are supported

Eshu evaluates ignore files from the repo root down to the file's parent
directory. Patterns are relative to the directory that contains the ignore file.
Later matching patterns can override earlier matches.

## .gitignore interaction

Eshu honors the target repository's own `.gitignore` files during repo and
workspace indexing.

- Only `.gitignore` files inside the target repository apply.
- Parent workspace `.gitignore` files do not leak into sibling repositories.
- Nested `.gitignore` and `.eshuignore` files apply only within their subtree.
- Ignore-only file changes do not force filesystem repo re-selection unless the
  ignore rules themselves change.

Use `.gitignore` for normal generated files that should also be ignored by Git.
Use `.eshuignore` for Eshu-specific indexing choices.

## Examples

Plain `.eshuignore`:

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

Reasoned discovery map:

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

## IaC note

Terraform, Terragrunt, AWS SAM, CDK, and Serverless local cache directories are
already part of default discovery pruning. Add `.eshuignore` or
`.eshu/discovery.json` entries for other tool-local trees such as `.pulumi/`,
`.crossplane/`, or `.terramate-cache/` only when those paths are valid repo
content but not useful graph input.

## Verification

Relevant focused tests:

```bash
cd go
go test ./internal/collector/discovery \
  -run 'TestResolveRepositoryFileSetsHonorsRepoLocalEshuIgnoreScopingAndNestedNegation|TestResolveRepositoryFileSetsHonorsRepoLocalGitignoreScopingAndNestedNegation' \
  -count=1
go test ./internal/collector \
  -run 'TestNativeRepositorySelectorSelectRepositoriesFilesystemIgnoresEshuignoredManifestChurn|TestNativeRepositorySelectorSelectRepositoriesFilesystemIgnoresGitignoredManifestChurn|TestNativeRepositorySelectorSelectRepositoriesFilesystemReselectsWhenIgnoreRulesChange' \
  -count=1
```

## Related docs

- [CLI indexing](cli-indexing.md)
- [Configuration](configuration.md)
- [Local testing discovery advisory](local-testing/discovery-advisory.md)
