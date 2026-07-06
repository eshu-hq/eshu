# Releasing The SDK Modules

This document covers tagging `sdk/go/collector` and `sdk/go/factschema` — the
two public Go subdirectory modules external collector authors pin. For the
core Eshu release (`vX.Y.Z` at the repository root: Docker image, Helm chart,
CLI binaries), use the `eshu-release` skill instead; that flow is unrelated to
the SDK module tags described here.

Both SDK modules are Go
[subdirectory modules](https://go.dev/ref/mod#vcs-branch): each has its own
`go.mod` inside the monorepo, and each is tagged independently with a
`<module-dir>/vX.Y.Z` git tag rather than a bare `vX.Y.Z` tag. This is the
standard mechanism the Go module proxy uses to resolve `go get
github.com/eshu-hq/eshu/sdk/go/collector@vX.Y.Z` to the right subtree at the
right commit, without requiring a whole-repository release.

## Who does this

Tagging is a coordinator-controlled, outward-facing action: once a tag is
pushed, the Go module proxy (`proxy.golang.org`) caches that module version
permanently, so a mistake cannot be un-pushed. An executor preparing an SDK
release change (changelog, docs, CI) does **not** run `git tag` or
`git push origin <tag>`. The executor's PR states the exact commands and
versions in its description; a maintainer runs them after the PR merges.

## Preconditions before tagging

1. The PR containing the release-worthy change (CHANGELOG entry, schema
   change, new decode function, etc.) is merged to `main`.
2. `.github/workflows/factschema-diff.yml` is green on `main` for a
   `sdk/go/factschema` release — it is this module's `buf breaking`
   equivalent and will catch an unbumped major before the tag is cut. Re-run
   it locally against the last release tag once one exists:
   `bash scripts/verify-factschema-diff.sh` (pass `-base-ref
   sdk/go/factschema/v<last>` once a prior tag exists; before the first tag,
   the default merge-base-against-`origin/main` behavior is correct).
3. The CHANGELOG's top `[Unreleased]` section is renamed to the chosen
   version with today's date, per the convention documented in
   `sdk/go/collector/CHANGELOG.md` and `sdk/go/factschema/CHANGELOG.md`.
4. `docs/public/extend/sdk-compatibility.md` gains a row for the new version
   combination (SDK module version, core release, wire protocol, fixture-pack
   version) before or in the same PR as the tag.

## Tag commands

From a clean, fast-forwarded `main` checkout (not a stale worktree):

```bash
git fetch origin
git checkout main
git pull --ff-only

# Confirm the exact commit being tagged.
git rev-parse HEAD
```

Tag each module independently, at the same commit if cutting both together,
using the Go subdirectory-module tag format:

```bash
git tag sdk/go/collector/vX.Y.Z <sha>
git tag sdk/go/factschema/vX.Y.Z <sha>
```

Push both tags (each tag push is a separate, irreversible action against the
public module proxy):

```bash
git push origin sdk/go/collector/vX.Y.Z
git push origin sdk/go/factschema/vX.Y.Z
```

## Verifying the tag is fetchable

Prove the tag resolves through the real Go module proxy from a scratch module
outside the repository (the acceptance criterion in #4583):

```bash
mkdir -p /tmp/eshu-sdk-proxy-check && cd /tmp/eshu-sdk-proxy-check
go mod init proxycheck
go get github.com/eshu-hq/eshu/sdk/go/collector@vX.Y.Z
go get github.com/eshu-hq/eshu/sdk/go/factschema@vX.Y.Z
```

A successful `go get` prints the resolved module version and updates
`go.mod`/`go.sum`; a failure means the tag path, `go.mod` module line, or
proxy cache disagree and must be fixed before announcing the release.

## Initial versions for the first tags

| Module | Chosen version | Reasoning |
| --- | --- | --- |
| `sdk/go/collector` | `v0.1.0` | `docs/public/extend/community-extension-authoring.md` has stated the collector SDK's semver line is `v0.1.x` since the SDK was scaffolded; this is the first tag on that already-documented line, so it starts at the first release inside it rather than `v0.1.1+` or a re-numbered line. |
| `sdk/go/factschema` | `v0.1.0` | The module is new (scaffolded in #4567) with no prior tag; following the same `v0.1.x` convention as `sdk/go/collector` for a first public release of a Go module keeps the two SDK modules' initial version schemes consistent, even though they version independently afterward. |

Both are `v0.x` releases: per Go module semantics, `v0` carries no
backward-compatibility guarantee between minor versions, which matches these
modules' actual maturity (in-tree consumers are the primary user; the
external-collector story is what #4583 and #4572 exist to prove). Move either
module to `v1.0.0` only after an external, out-of-tree consumer has
successfully pinned and upgraded across at least one release, per normal
Go module versioning guidance.

## After tagging

1. Confirm `go get ... @vX.Y.Z` succeeds from the scratch module above and
   paste the output in the release announcement or tracking issue.
2. Update `examples/collector-extensions/scorecard/go.mod`'s comment (see
   `examples/collector-extensions/scorecard/README.md`, "Pinning story") if
   the tagged version changes what an external consumer's `go.mod` should
   `require`.
3. Add the new row to `docs/public/extend/sdk-compatibility.md` if it was not
   already added pre-tag.
