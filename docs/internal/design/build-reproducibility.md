# Build Reproducibility

Status: design and implementation for Epic Y (#3750). Dockerfile and CI
workflow changes in Y-1 (#3836); this document in Y-2 (#3837).

Owners: release, build, and CI maintainers.

## 1. Decision

Eshu Docker builds must produce the same Go binary bits from the same source
commit. A release tag must point to one specific image digest.

Build reproducibility is achieved through three mechanisms applied together:

1. Base images pinned by content digest, not floating tag.
2. Go compilation driven by `SOURCE_DATE_EPOCH` so embedded timestamps are
   deterministic.
3. OCI image labels anchored to the commit timestamp, not the build time.

A CI verification job proves the contract on every PR and push that changes
Dockerfile, Go source, or the publish workflow itself.

## 2. Problem

Before this change, Eshu's Dockerfile referenced base images by tag:

```dockerfile
FROM tonistiigi/xx:1.5.0
FROM golang:1.26-alpine
FROM alpine:3.21
```

These tags float. A rebuild days later can pull different base image layers even
though the tag text has not changed. Within Go compilation, the linker embeds
the build timestamp (`_cgo_libc.so`, PE headers, Go `runtime.buildVersion`
metadata), which changes on every run. The result: a rebuild of the same commit
on a different day can produce a different image digest.

For a release that carries `cosign` signatures and SLSA provenance attestations,
a non-deterministic build means a downstream verifier cannot independently
reconstruct the same digest and confirm the attestation chain. This violates the
supply-chain expectation that a tag represents one fixed artifact.

## 3. Design

### 3.1 Base Image Digest Pinning

All three `FROM` directives in `Dockerfile` carry a `@sha256:...` suffix.
Buildx resolves the tag to a manifest-list (index) digest, so each platform
pull gets the correct platform image without ambiguity.

| Stage | Image | Pinned digest |
|-------|-------|---------------|
| xx helper | `tonistiigi/xx:1.5.0` | `sha256:0c6a569797744e45955f39d4f7538ac344bfb7ebf0a54006a0a4297b153ccf0f` |
| builder | `golang:1.26-alpine` | `sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648` |
| production | `alpine:3.21` | `sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d` |

Digests are verified against the Docker Hub API at PR time. A periodic workflow
should refresh these digests when upstream images release security patches.

### 3.2 Deterministic Go Compilation

`Dockerfile` declares an `ARG SOURCE_DATE_EPOCH` and exports it as an
environment variable before the Go build step:

```dockerfile
ARG SOURCE_DATE_EPOCH
ENV SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH}
```

Go's toolchain (1.18+) reads `SOURCE_DATE_EPOCH` and uses it in place of the
wall-clock time when embedding the build timestamp in the binary. Combined with
the existing `-trimpath` flag, this makes the Go compiler output fully
deterministic for a given source tree, Go version, and dependency graph.

The CI workflow derives the epoch from the HEAD commit timestamp:

```bash
git log -1 --format=%ct
```

This ties the binary timestamp to the source commit, not the build machine's
clock.

### 3.3 Image Label Determinism

`docker/metadata-action@v5` generates OCI annotations including
`org.opencontainers.image.created` with the current build time. To make labels
deterministic, a follow-up step replaces the wall-clock `created` timestamp with
the commit timestamp (the same `SOURCE_DATE_EPOCH` value).

### 3.4 Provenance Separation

`docker/build-push-action@v5` is invoked with `provenance: false`. This
prevents the action from attaching a built-in SLSA provenance layer to the
image, which would embed non-deterministic metadata (build invocation ID,
timestamp, builder info) in the image config.

Provenance attestation is still generated — by the separate
`actions/attest-build-provenance@v1` step, which pushes it as an OCI referrer
(a distinct artifact keyed to the image digest). This external attestation does
not affect the image digest itself.

## 4. CI Verification Gate

A new `verify-reproducibility` job in `.github/workflows/docker-publish.yml`
runs on every PR and push that triggers the image build lane. It:

1. Checks out the source and computes `SOURCE_DATE_EPOCH` from the HEAD commit.
2. Builds the Docker image for `linux/amd64` with `--no-cache` and
   `--output type=local,dest=/tmp/out1`.
3. Builds again with the same arguments to `/tmp/out2`.
4. Extracts sha256 digests of all files in `/usr/local/bin/` from both outputs
   and diffs them.

If any Go binary differs, the job fails and blocks the merge. A summary of
total file comparison (including system files from `apk add`) is printed for
visibility; variance in Alpine package metadata is accepted (see §5).

## 5. Acceptable Variance

The verification gate enforces byte-identical Go binaries. The full filesystem
comparison prints non-binary differences for operator visibility but does not
block on them. Known sources of acceptable variance:

- **Alpine package metadata**: `apk add` writes installation timestamps and
  database entries that can differ between runs even when the same package
  versions are installed.
- **File metadata**: Docker layer export preserves file timestamps and
  ownership from the build context, which may differ between builders.
- **apk index freshness**: If the two builds in the verification job pull
  Alpine package indexes from different mirrors or different points in time
  (extremely unlikely within the same CI run), package versions can drift.

When full-image reproducibility is required (e.g., for downstream rebuild
verification), pinning specific Alpine package versions or using a fixed apk
cache is the next step. That work is out of scope for this PR.

## 6. Evidence

Reproducibility proof (local, 2026-06-26):

```text
Platform: linux/arm64 (OrbStack, macOS)
SOURCE_DATE_EPOCH: 1782467207
ESHU_VERSION: repro-test

docker buildx build --platform linux/arm64 \
  --build-arg SOURCE_DATE_EPOCH=1782467207 \
  --build-arg ESHU_VERSION=repro-test \
  --provenance=false --no-cache \
  --output type=local,dest=/tmp/out1 .

docker buildx build --platform linux/arm64 \
  --build-arg SOURCE_DATE_EPOCH=1782467207 \
  --build-arg ESHU_VERSION=repro-test \
  --provenance=false --no-cache \
  --output type=local,dest=/tmp/out2 .

Result: All 31 Go binaries in /usr/local/bin/ are byte-identical (sha256 match).
Alpine system files: xx files differ (acceptable — apk metadata variance).
```

## 7. Non-Goals

This PR does not:

- Pin Alpine package versions inside `apk add` invocations.
- Add a periodic workflow for digest refresh (Dependabot or similar).
- Verify reproducibility for `linux/arm64` in CI (CI builds `linux/amd64` only;
  local proof on arm64 completed).
- Change the Go toolchain version, `go.mod` go directive, or any Go package.
- Alter `.github/workflows/security-scan.yml`.

## 8. Sources

- [Dockerfile](../../../Dockerfile)
- [Docker Publish Workflow](../../../.github/workflows/docker-publish.yml)
- [Reproducible Builds — SOURCE_DATE_EPOCH](https://reproducible-builds.org/docs/source-date-epoch/)
- [Go 1.18 Release Notes — Build Reproducibility](https://go.dev/doc/go1.18#go-build-reproducibility)
- [docker/build-push-action provenance](https://github.com/docker/build-push-action)
