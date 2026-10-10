# CI service image mirrors

Docker Hub's anonymous rate limit can stop hosted jobs while GitHub Actions
initializes PostgreSQL and Neo4j service containers, and while Docker builds
pull their base images, before Eshu tests run. This work follows from #7886.
`docker-publish.yml` has a manual-only mirror mode that copies eight pinned
upstream OCI indexes to dedicated Eshu GHCR packages. It does not rebuild them.
Three packages hold CI service images. Five hold build bases: the Dockerfile
stages, the query-plan Neo4j profile image, and the BuildKit image that
`docker/setup-buildx-action` starts. A copy keeps the upstream index digest, so
every `@sha256:` pin in the tree keeps its value when a consumer switches
registry.

| Upstream index | Eshu GHCR reference | Index digest |
| --- | --- | --- |
| `mirror.gcr.io/library/postgres:18-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873` | `ghcr.io/eshu-hq/ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873` | `sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873` |
| `mirror.gcr.io/library/postgres:18.6-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c` | `ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c` | `sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c` |
| `mirror.gcr.io/library/neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` | `ghcr.io/eshu-hq/ci-neo4j-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` | `sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` |
| `mirror.gcr.io/library/neo4j@sha256:6c162e2432f861f2c4e3da77a6ba478e7f10e2160b870541f85294532bc6ff5f` | `ghcr.io/eshu-hq/ci-neo4j-query-plan@sha256:6c162e2432f861f2c4e3da77a6ba478e7f10e2160b870541f85294532bc6ff5f` | `sha256:6c162e2432f861f2c4e3da77a6ba478e7f10e2160b870541f85294532bc6ff5f` |
| `mirror.gcr.io/library/golang:1.26.9-alpine@sha256:cdfd4fe2da6b225d8b40c6b7a105736e548e83ff56d5d8f9394446eeb5eb84e0` | `ghcr.io/eshu-hq/ci-golang-alpine@sha256:cdfd4fe2da6b225d8b40c6b7a105736e548e83ff56d5d8f9394446eeb5eb84e0` | `sha256:cdfd4fe2da6b225d8b40c6b7a105736e548e83ff56d5d8f9394446eeb5eb84e0` |
| `mirror.gcr.io/library/alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d` | `ghcr.io/eshu-hq/ci-alpine@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d` | `sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d` |
| `mirror.gcr.io/tonistiigi/xx:1.5.0@sha256:0c6a569797744e45955f39d4f7538ac344bfb7ebf0a54006a0a4297b153ccf0f` | `ghcr.io/eshu-hq/ci-buildx-xx@sha256:0c6a569797744e45955f39d4f7538ac344bfb7ebf0a54006a0a4297b153ccf0f` | `sha256:0c6a569797744e45955f39d4f7538ac344bfb7ebf0a54006a0a4297b153ccf0f` |
| `mirror.gcr.io/moby/buildkit:buildx-stable-1@sha256:cec9f139f45e93c5c69c60f8b07cfad9f43f4ef6b6a6cd917527fea5ff2e3dea` | `ghcr.io/eshu-hq/ci-buildkit@sha256:cec9f139f45e93c5c69c60f8b07cfad9f43f4ef6b6a6cd917527fea5ff2e3dea` | `sha256:cec9f139f45e93c5c69c60f8b07cfad9f43f4ef6b6a6cd917527fea5ff2e3dea` |

The digest-qualified `ci-neo4j-community` source is an earlier
`2026-community` index. The unqualified tag has since moved; the publisher does
not follow it.

## What each package is for

| Package | Used by | How the source was chosen |
| --- | --- | --- |
| `ci-postgres-alpine`, `ci-neo4j-community` | CI service containers and Compose files | The digests of the first publication. |
| `ci-postgres-bookworm` | CI service containers and Compose files, and `scripts/ci/live-postgres-standby-fixture.sh`, which runs the `postgres:18.6-bookworm` index with `docker run` | The digest of the first publication. |
| `ci-neo4j-query-plan` | `scripts/verify-query-plan-profile.sh` | The exact digest the query-plan profiles are bound to. Keep it; a newer Neo4j index would need new profiles. |
| `ci-golang-alpine` | Dockerfile `golang:1.26.9-alpine` builder stage | The digest already pinned in the Dockerfile. |
| `ci-alpine` | Dockerfile `alpine:3.21` runtime stages | The digest already pinned in the Dockerfile. |
| `ci-buildx-xx` | Dockerfile `tonistiigi/xx:1.5.0` cross-build helper stage | The digest already pinned in the Dockerfile. |
| `ci-buildkit` | The BuildKit container `docker/setup-buildx-action` starts for `docker-publish.yml` | The index `moby/buildkit:buildx-stable-1` resolved to on 2026-10-10. |

The BuildKit tag is not chosen by this repository.
`docker/setup-buildx-action@v3` passes no image to `docker buildx create`:
`getCreateArgs` adds only the `--driver-opt` values the workflow supplies
([`src/context.ts` lines 55-82 at `8d2750c`](https://github.com/docker/setup-buildx-action/blob/8d2750c68a42422c14e847fe6c8ac0403b4cbd6f/src/context.ts#L55-L82),
the commit `v3` pointed to on 2026-10-10).
The `docker-container` driver then uses `bkimage.DefaultImage`, which is
`moby/buildkit:buildx-stable-1`
([`driver/bkimage/bkimage.go` lines 3-14 at `v0.38.0`](https://github.com/docker/buildx/blob/0dbe87f36ed472fe36b6796c3f70b4210a731fc5/driver/bkimage/bkimage.go#L3-L14),
which holds `DefaultImage` and `TrustedRepo`; commit `0dbe87f`).
That image is a moving tag. The mirror pins the index it resolved to on
2026-10-10, so a later consumer switch sets
`driver-opts: image=ghcr.io/eshu-hq/ci-buildkit@sha256:...` and no longer follows
the tag. Buildx verifies a tag only inside the `docker.io/moby/buildkit`
repository, so an image from another registry needs no `allow-untrusted-image`
driver option. The BuildKit digest has no pin in the tree yet: this change adds
the package and no consumer.

The source references of the build-base rows keep the Dockerfile tag next to
the digest for provenance. The registry resolves the digest and ignores the tag.
On 2026-10-10 the upstream tags `golang:1.26.9-alpine`, `alpine:3.21`, and
`neo4j:2026-community` already resolved to different indexes than the pinned
digests. The pin is the digest. The tag is provenance only, so a check by tag
shows a different digest and that is expected.

The publisher accepts no image or destination input. It checks each source
digest, copies the complete index to its digest-qualified GHCR reference, and
checks the destination digest. It creates no mutable version tags. Repeated or
concurrent copies publish the same content. The manual mirror mode skips every
normal Eshu image, chart, and release job.

## Publication and public-access gate

1. Independently review the exact `main` commit and record its full SHA. Run
   the manual publisher from that commit:

   ```bash
   gh workflow run docker-publish.yml --ref main \
     -f mode=ci-mirrors-publish -f expected_sha=<reviewed-40-character-sha>
   ```

   The publisher rejects a moved branch when `GITHUB_SHA` differs from
   `expected_sha`. It allows only a manual run on `main` in `eshu-hq/eshu`.
   The initial three indexes were published from the reviewed bootstrap branch
   in [run 38000888647](https://github.com/eshu-hq/eshu/actions/runs/38000888647)
   before the `main`-only restriction. Do not reuse that branch for refreshes.
   The five build-base indexes are published by a later `main` run of the same
   command. A rerun is safe: an existing package with the approved digest is
   kept, and one with another digest stops the run.

2. New GHCR container packages start private. Before changing visibility, a
   package admin must inspect **every version** in each of the eight packages
   in the GitHub organization Packages UI. Confirm that the package contains
   only the approved upstream copies above, with the exact
   index digests. A 403 or 404 from a token without `read:packages` is not
   evidence that a package is absent or safe to expose. Stop if any version
   is unknown; do not delete or expose it as part of this workflow.
3. After that package-wide inspection, an authorized package admin changes
   each package's visibility to Public in its package settings. This is an
   external, irreversible visibility change; the workflow does not do it.
4. Run the anonymous check from `main`:

   ```bash
   gh workflow run docker-publish.yml --ref main \
     -f mode=ci-mirrors-verify-public
   ```

   The job has no package-write permission or GHCR login. Its script uses a
   fresh Docker config and requires each public digest to equal the table.
5. Record these checks for **each of the eight packages** before any consumer
   switches to it. The `verify-public` run covers the digest read; the other
   two are separate observations:
   - An anonymous manifest request for the digest returns HTTP 200 and a
     `docker-content-digest` header equal to the table. Use a fresh Docker
     config and no token.
   - An anonymous `docker pull ghcr.io/eshu-hq/<package>@<digest>` exits 0.

   Consumers must not switch to a reference until all three checks pass for
   its package. A start of the pulled image on an `amd64` hosted runner is a
   separate proof that the consumer switch records in its own wave.

## Not checked by this workflow

The workflow and these steps do not establish package retention or visibility.
Reading either needs a token with package scope. An anonymous manifest request
that returns 200 is the observable proxy for public visibility and nothing
more. An administrator must confirm both settings, and confirm that the
retention policy keeps every untagged index version, before a consumer depends
on a package.

## Maintenance

Consumers pin the verified index digest. Package deletion and visibility remain
administrator-controlled operations. Retain these untagged index versions and
their referenced content; exclude them from automatic untagged-version cleanup.
For an upstream refresh, review a new source digest, then update the script,
tests, table, and consumer pins together. Publish and verify the new digest
before changing consumers. Package cleanup requires a separate reviewed
retention decision. Do not substitute a moving Docker Hub tag, an unbounded
workflow input, or a cache-only registry reference for the verified GHCR digest.

The shape gate pins the full text of both mirror jobs, permissions and steps,
to `scripts/dev/fixtures/ci-image-mirror-publish-job.txt` and
`scripts/dev/fixtures/ci-image-mirror-verify-public-job.txt`. An added or
changed step needs the same change in its fixture and a security review,
because the publisher job holds package-write permission and a GHCR login.
The registry rows `ci-service-mirror-publish` and
`ci-service-mirror-verify-public` are manual and advisory: no merge waits on
either job.
