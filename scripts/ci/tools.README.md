# Static contract gate tools

`tools.Dockerfile` builds the Ubuntu 24.04/glibc tool environment for
`.github/workflows/static-contract-gates.yml`'s gate matrix. It contains no
Eshu source, binaries, credentials, or private paths. Actions mounts the
checkout and its JavaScript action runtime separately. The image therefore
needs no Node.js installation for `actions/checkout` or `docker/login-action`.
The job runs as UID/GID 1001 with a writable home directory; this keeps the
blocking tools container from running as root while leaving the host-provided
Actions JavaScript runtime unchanged.

The default base is the upstream Ubuntu 24.04 OCI index digest observed on
2026-10-10. Publication should pass `BASE_IMAGE` as the verified digest of the
approved GHCR Ubuntu mirror. Docker builds fail if the pinned direct Ubuntu
package versions disappear; this is preferable to silently switching tools.
The image records transitive package versions through `dpkg-query` at proof or
publication time, because Ubuntu security repository dependencies can advance.

```bash
empty_context="$(mktemp -d)"
docker buildx build --platform linux/amd64 \
  --build-arg BASE_IMAGE='ghcr.io/eshu-hq/ci-mirror-ubuntu@sha256:534baea6a22c03a63003dbc8dbe78fe34bc0d7e595d9a9dc9834884ff530eb55' \
  -f scripts/ci/tools.Dockerfile -t eshu-ci-tools:candidate --load "${empty_context}"
rmdir "${empty_context}"
bash scripts/ci/test-tools-image.sh eshu-ci-tools:candidate
docker run --rm --platform linux/amd64 eshu-ci-tools:candidate \
  cat /usr/local/share/ci-tools/packages.tsv
```

The same recipe supports Linux arm64 for native local proof (`--platform
linux/arm64`, `CI_TOOLS_PLATFORM=linux/arm64`). The static matrix Docker-related
tests inspect workflow and fixture text with fake Docker commands; they do not
start nested containers. If a later gate does, set `CI_TOOLS_DOCKER_SOCKET=1`
on a Linux Docker host to check socket access and a nested bind mount of the
checkout at the exact path visible inside the job container. Docker's daemon
resolves bind paths on the host, even when the client runs in a container.

The static workflow uses the ledger's `ci-tools` index only for selected gate
jobs. An idle merge-group cell receives an empty container expression and does
not pull the image. Go module and build caches live under `/tmp/eshu-ci` inside
the container and use `actions/cache`. `GOTOOLCHAIN=local` makes a `go.mod`
toolchain bump fail the blocking check until this image is rebuilt, published,
and pinned to the matching Go version.

Pinned upstream inputs: Go 1.26.10 Linux amd64 tar SHA-256
`ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5`;
ripgrep 15.2.0 musl tar SHA-256
`33e15bcf1624b25cdd2a55813a47a2f95dbe126268203e76aa6a585d1e7b149c`;
yq 4.54.1 Linux amd64 SHA-256
`8e34fc298390875de416e6a4afcb8cabeceb25d9aa8506c1a2f9353cf702ea5f`.
Minimum supported yq is 4.44.3 (#8397): the oldest release the gate scripts'
`strenv(TAB)` rows are proven on (4.44.3/4.49.2/4.54.1). CI runs the pin above;
`scripts/verify-dockerhub-login.sh` fails closed on anything older.
The Docker CLI 29.1.3 static tar and Compose 2.40.3 plugin are also
checksum-pinned for each architecture in the Dockerfile. Only the CLI binary
is extracted from Docker's tar; the image has no daemon or container runtime.

| Download | Versioned source and license |
| --- | --- |
| Go | [go1.26.10 source](https://github.com/golang/go/tree/go1.26.10), [BSD-3-Clause license](https://github.com/golang/go/blob/go1.26.10/LICENSE) |
| ripgrep | [15.2.0 source](https://github.com/BurntSushi/ripgrep/tree/15.2.0), [MIT license](https://github.com/BurntSushi/ripgrep/blob/15.2.0/LICENSE-MIT) |
| yq | [v4.54.1 source](https://github.com/mikefarah/yq/tree/v4.54.1), [MIT license](https://github.com/mikefarah/yq/blob/v4.54.1/LICENSE) |
| Docker CLI | [v29.1.3 source](https://github.com/docker/cli/tree/v29.1.3), [Apache-2.0 license](https://github.com/docker/cli/blob/v29.1.3/LICENSE) |
| Docker Compose | [v2.40.3 source](https://github.com/docker/compose/tree/v2.40.3), [Apache-2.0 license](https://github.com/docker/compose/blob/v2.40.3/LICENSE) |

The Ubuntu package versions are in `tools.Dockerfile`; apt verifies signed repository
metadata and `.deb` checksums. The image does not start a Docker daemon.
`/usr/local/share/ci-tools/packages.tsv` and `downloads.tsv` are the package
and checksum inventory embedded in each build. Ubuntu package copyright files
remain under `/usr/share/doc/*/copyright`. Go carries its LICENSE in
`/usr/local/go`, and the four other downloaded binaries have checksum-verified
license copies in `/usr/local/share/licenses/ci-tools`.
