#!/usr/bin/env bash
#
# test-verify-scanner-digest-pins.sh - hermetic tests for
# scripts/verify-scanner-digest-pins.sh (#9491). No network, no Docker, no gh
# auth: `crane` and `gh` resolve to PATH stubs under mktemp, and git runs
# against tmp fixture repos that mirror the scanner source layout.
#
# The seeded RED/GREEN pair:
#   GREEN a fixture manifest whose pins equal what the stub registry serves,
#         with no scanner Go input newer than the anchored SHA: exit 0.
#   RED   a one-character typo in any pin, a MANIFEST_UNKNOWN tag (404), a
#         failing attestation, or a scanner Go input committed after the
#         anchor: exit 1 naming the component and the expected digest.
# Fail-closed cases: a missing or malformed Scanner-Publish-SHA line, an
# anchor that is not history, a missing pin, and an unreadable manifest must
# all exit 2, never 0. A network-shaped crane failure (not MANIFEST_UNKNOWN)
# also exits 2.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-scanner-digest-pins.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

PIN_IMAGE="d2967a1ab642747ec5bafed484b4a5384839b90b59c9562ee38e67b3fa547d49"
PIN_SBOM="cbafeeeb2bae99902838e5159bd4003e210f5cfe41891b50b0caf502b2d99dec"
PIN_OS="e302cf4e398f23974210f69988a3c53510738100ae7525ead6d11d992d7be3a8"

# --- PATH stubs -----------------------------------------------------------
stubbin="${tmp_root}/stubbin"
mkdir -p "${stubbin}"
# Stub crane serves digests from $CRANE_STUB_DIR/digest-<comp> files holding
# "sha256:<hex>"; a $CRANE_STUB_DIR/fail-<comp> file makes that component
# 404 with a MANIFEST_UNKNOWN message like the real crane; a
# $CRANE_STUB_DIR/broken file fails every call without that marker.
cat >"${stubbin}/crane" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == digest && $# -eq 2 ]] || exit 2
comp="$(printf '%s' "$2"|sed -n 's|.*/scanner-\([a-z]*\):sha-[0-9a-f]*$|\1|p')"
[[ -n "${comp}" ]] || exit 2
dir="${CRANE_STUB_DIR:?need CRANE_STUB_DIR}"
[[ -e "${dir}/broken" ]] && { echo dial tcp: connection refused >&2; exit 1; }
[[ -e "${dir}/fail-${comp}" ]] && { echo "MANIFEST_UNKNOWN: ${comp}" >&2; exit 1; }
cat "${dir}/digest-${comp}"
STUB
# Stub gh passes attestation unless GH_STUB_ATTEST_FAIL names the component.
cat >"${stubbin}/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == attestation && "${2:-}" == verify ]] || { echo "stub gh: want 'attestation verify ...'" >&2; exit 2; }
ref="$3"
comp="$(printf '%s' "${ref}" | sed -n 's|.*scanner-\([a-z]*\)@sha256:[0-9a-f]*|\1|p')"
[[ -n "${comp}" ]] || { echo "stub gh: cannot parse component from ${ref}" >&2; exit 2; }
if [[ "${GH_STUB_ATTEST_FAIL:-}" == "${comp}" ]]; then
	echo "stub gh: no attestation found for ${ref}" >&2
	exit 1
fi
exit 0
STUB
chmod +x "${stubbin}/crane" "${stubbin}/gh"

# make_fixture DIR [SHA_LINE_MODE] writes a fixture repo whose manifest pins
# the three PIN_* digests. SHA_LINE_MODE selects the anchor line: valid
# (anchor = the inputs commit), missing, malformed, or foreign (valid hex,
# not history). Prints the fixture manifest path.
make_fixture() {
	local dir="$1" mode="${2:-valid}"
	mkdir -p "${dir}/go/internal/collector/scannerworker/component"
	mkdir -p "${dir}/go/cmd/scanner-component-image" "${dir}/go/cmd/scanner-component-sbom"
	mkdir -p "${dir}/go/cmd/scanner-component-osruntime" "${dir}/go/cmd/scanner-worker"
	printf 'package scannerworker\n' >"${dir}/go/internal/collector/scannerworker/analyzer.go"
	for comp in image sbom osruntime; do
		printf 'package main\n' >"${dir}/go/cmd/scanner-component-${comp}/main.go"
	done
	printf 'package main\n' >"${dir}/go/cmd/scanner-worker/main.go"
	# Non-Go scanner inputs (#9503): Dockerfile base pins + scanner stages,
	# the publish workflow env mirrors + scanner job, the ci-images digests
	# for the three base images, and the module files. Committed with the
	# inputs so the anchor covers them.
	cat >"${dir}/Dockerfile" <<'DOCKER'
ARG ESHU_XX_IMAGE=tonistiigi/xx:1.5.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
ARG ESHU_GO_IMAGE=golang:1.26.10-alpine@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
ARG ESHU_ALPINE_IMAGE=alpine:3.21@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc

FROM --platform=$BUILDPLATFORM ${ESHU_GO_IMAGE} AS builder
ARG TARGETPLATFORM
ARG ESHU_VERSION=dev
RUN cd go \
    && export CGO_ENABLED=1 \
    && LDFLAGS="-s -w" \
    && xx-go build -o /go-bin/eshu ./cmd/eshu \
    && xx-go build -o /go-bin/eshu-scanner-component-image ./cmd/scanner-component-image \
    && xx-go build -o /go-bin/eshu-scanner-component-sbom ./cmd/scanner-component-sbom \
    && xx-go build -o /go-bin/eshu-scanner-component-osruntime ./cmd/scanner-component-osruntime

FROM ${ESHU_ALPINE_IMAGE} AS scanner-image
COPY --from=builder /go-bin/eshu-scanner-component-image /usr/local/bin/eshu-scanner-component-image
RUN adduser -D -u 10001 scanner
USER scanner
CMD ["eshu-scanner-component-image"]

FROM ${ESHU_ALPINE_IMAGE} AS scanner-sbom
COPY --from=builder /go-bin/eshu-scanner-component-sbom /usr/local/bin/eshu-scanner-component-sbom
RUN adduser -D -u 10001 scanner
USER scanner
CMD ["eshu-scanner-component-sbom"]

FROM ${ESHU_ALPINE_IMAGE} AS scanner-osruntime
COPY --from=builder /go-bin/eshu-scanner-component-osruntime /usr/local/bin/eshu-scanner-component-osruntime
RUN adduser -D -u 10001 scanner
USER scanner
CMD ["eshu-scanner-component-osruntime"]

FROM ${ESHU_ALPINE_IMAGE} AS runtime
RUN apk add --no-cache git curl
COPY --from=builder /go-bin/ /usr/local/bin/
CMD ["eshu-api"]
DOCKER
	mkdir -p "${dir}/.github/workflows" "${dir}/specs"
	cat >"${dir}/.github/workflows/docker-publish.yml" <<'YAML'
name: Publish Image
env:
  ESHU_XX_IMAGE: ghcr.io/eshu-hq/ci-mirror-xx@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  ESHU_GO_IMAGE: ghcr.io/eshu-hq/ci-mirror-golang@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  ESHU_ALPINE_IMAGE: ghcr.io/eshu-hq/ci-mirror-alpine@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
  IMAGE_PLATFORMS: linux/amd64,linux/arm64
jobs:
  build-and-push-scanner-images:
    name: Build and push scanner
    steps:
      - name: Build and push scanner image
        with:
          target: scanner-${{ matrix.component }}
          build-args: |
            ESHU_XX_IMAGE=${{ env.ESHU_XX_IMAGE }}
            ESHU_GO_IMAGE=${{ env.ESHU_GO_IMAGE }}
            ESHU_ALPINE_IMAGE=${{ env.ESHU_ALPINE_IMAGE }}
          platforms: ${{ env.IMAGE_PLATFORMS }}
  build-and-push-image:
    name: Build and push runtime
    steps:
      - name: Build and push runtime image
        with:
          target: runtime
YAML
	cat >"${dir}/specs/ci-images.v1.json" <<'JSON'
{
  "version": 1,
  "images": [
    {"id": "xx", "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
    {"id": "golang", "digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
    {"id": "alpine", "digest": "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
  ]
}
JSON
	printf 'module example.com/fixture\n\ngo 1.26.10\n' >"${dir}/go/go.mod"
	printf 'example.com/mod v1.0.0 h1:abc=\n' >"${dir}/go/go.sum"
	git -C "${dir}" init -q -b main
	git -C "${dir}" config user.name CI
	git -C "${dir}" config user.email ci@example.invalid
	git -C "${dir}" add -A
	git -C "${dir}" commit -qm inputs
	local anchor
	anchor="$(git -C "${dir}" rev-parse HEAD)"
	local sha_line="// Scanner-Publish-SHA: ${anchor}"
	case "${mode}" in
		missing) sha_line="// no anchor here" ;;
		malformed) sha_line="// Scanner-Publish-SHA: xyz" ;;
		foreign) sha_line="// Scanner-Publish-SHA: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ;;
	esac
	cat >"${dir}/go/internal/collector/scannerworker/component/manifest.go" <<EOF
package component

${sha_line}

var artifactDigests = map[string]string{
	IDImage:     "${PIN_IMAGE}",
	IDSBOM:      "${PIN_SBOM}",
	IDOSRuntime: "${PIN_OS}",
}
EOF
	git -C "${dir}" add -A
	git -C "${dir}" commit -qm pins
	git -C "${dir}" update-ref refs/remotes/origin/main HEAD
	printf '%s\n' "${dir}/go/internal/collector/scannerworker/component/manifest.go"
}

# serve_all DIR writes stub digest files matching the fixture pins.
serve_all() {
	mkdir -p "$1/stub"
	printf 'sha256:%s\n' "${PIN_IMAGE}" >"$1/stub/digest-image"
	printf 'sha256:%s\n' "${PIN_SBOM}" >"$1/stub/digest-sbom"
	printf 'sha256:%s\n' "${PIN_OS}" >"$1/stub/digest-osruntime"
	printf '%s/stub\n' "$1"
}

# run_verifier MANIFEST STUBDIR -> sets rc and out_file. The stubs resolve
# via PATH; CRANE_BIN/GH_BIN default to the stub names.
run_verifier() {
	out_file="${tmp_root}/out.txt"
	set +e
	env PATH="${stubbin}:${PATH}" CRANE_BIN=crane GH_BIN=gh \
		CRANE_STUB_DIR="$2" GH_STUB_ATTEST_FAIL="${GH_STUB_ATTEST_FAIL:-}" \
		bash "${verifier}" --manifest "$1" >"${out_file}" 2>&1
	rc=$?
	set -e
}

expect_rc() {
	local want="$1" label="$2"
	if [[ "${rc}" -eq "${want}" ]]; then
		record_pass "${label} (rc=${rc})"
	else
		record_fail "${label}: want rc=${want}, got rc=${rc}"
		cat "${out_file}" >&2
	fi
}

expect_output() {
	local needle="$1" label="$2"
	if [[ "$(<"${out_file}")" == *"${needle}"* ]]; then
		record_pass "${label}"
	else
		record_fail "${label} (expected to find: ${needle})"
		cat "${out_file}" >&2
	fi
}

# --- GREEN: pins match the stub registry, inputs predate the anchor. -------
fx="${tmp_root}/green"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
run_verifier "${manifest}" "${stub}"
expect_rc 0 "GREEN: matching pins with fresh inputs pass"
expect_output "PASS" "GREEN prints the PASS summary"

# A pin-file-only change after the anchor is not staleness: bump a comment
# in the fixture manifest and move origin/main past it.
printf '// follow-up pin bump\n' >>"${manifest}"
git -C "${fx}" add -A
git -C "${fx}" commit -qm pin-bump
git -C "${fx}" update-ref refs/remotes/origin/main HEAD
run_verifier "${manifest}" "${stub}"
expect_rc 0 "GREEN: a pin-file-only change after the anchor stays fresh"

# A non-Go file under the scanner tree is not a scanner input either.
printf '# notes\n' >"${fx}/go/internal/collector/scannerworker/NOTES.md"
git -C "${fx}" add -A
git -C "${fx}" commit -qm notes
git -C "${fx}" update-ref refs/remotes/origin/main HEAD
run_verifier "${manifest}" "${stub}"
expect_rc 0 "GREEN: a non-Go file under the scanner tree is not an input"

# --- RED: one-character typo in any pin. ------------------------------------
for comp_pin in "image:${PIN_IMAGE}" "sbom:${PIN_SBOM}" "osruntime:${PIN_OS}"; do
	comp="${comp_pin%%:*}"
	fx="${tmp_root}/typo-${comp}"
	manifest="$(make_fixture "${fx}")"
	stub="$(serve_all "${fx}")"
	python3 - "${manifest}" "${comp_pin%%:*}" <<'PY'
import sys
path, comp = sys.argv[1], sys.argv[2]
text = open(path).read()
old = {"image": "d2967a1a", "sbom": "cbafeeeb", "osruntime": "e302cf4e"}[comp]
new = {"image": "d2967a1b", "sbom": "cbafeeec", "osruntime": "e302cf4f"}[comp]
assert text.count(old) == 1, (comp, text.count(old))
open(path, "w").write(text.replace(old, new))
PY
	run_verifier "${manifest}" "${stub}"
	expect_rc 1 "RED: a typo in the ${comp} pin fails"
	expect_output "scanner-${comp}" "RED names the stale component scanner-${comp}"
	expect_output "expected digest:" "RED names the expected digest"
done

# --- RED: the tag is gone (404). --------------------------------------------
fx="${tmp_root}/gone"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
touch "${stub}/fail-sbom"
run_verifier "${manifest}" "${stub}"
expect_rc 1 "RED: a missing sha tag fails"
expect_output "scanner-sbom" "RED names the stale component scanner-sbom"
expect_output "expected digest: ${PIN_SBOM}" "RED names the expected sbom digest"

# --- RED: attestation re-verification fails. ---------------------------------
fx="${tmp_root}/attest"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
GH_STUB_ATTEST_FAIL=osruntime run_verifier "${manifest}" "${stub}"
expect_rc 1 "RED: a failing attestation fails"
expect_output "scanner-osruntime" "RED names the component with no attestation"
expect_output "expected digest: ${PIN_OS}" "RED names the expected osruntime digest"

# --- RED: a scanner Go input landed after the anchor. ------------------------
fx="${tmp_root}/stale"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
printf '// new analyzer input\n' >>"${fx}/go/cmd/scanner-component-image/main.go"
git -C "${fx}" add -A
git -C "${fx}" commit -qm analyzer-change
git -C "${fx}" update-ref refs/remotes/origin/main HEAD
run_verifier "${manifest}" "${stub}"
expect_rc 1 "RED: a scanner input newer than the anchor fails"
expect_output "STALE" "RED names the staleness verdict"
expect_output "go/cmd/scanner-component-image/main.go" "RED names the changed input"
expect_output "follow-up" "RED points at the follow-up pin PR flow"
expect_output "scanner-image" "RED still names the component"
expect_output "${PIN_IMAGE}" "RED still names the expected digest"

# --- RED: a base-image pin bumped after the anchor (#9503). ------------------
fx="${tmp_root}/basepin"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
sed -i.bak 's/^ARG ESHU_ALPINE_IMAGE=alpine:3.21@sha256:cccc.*/ARG ESHU_ALPINE_IMAGE=alpine:3.22@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd/' "${fx}/Dockerfile" && rm -f "${fx}/Dockerfile.bak"
git -C "${fx}" add -A
git -C "${fx}" commit -qm base-bump
git -C "${fx}" update-ref refs/remotes/origin/main HEAD
run_verifier "${manifest}" "${stub}"
expect_rc 1 "RED: a base-image pin bump fails"
expect_output "STALE" "RED names the staleness verdict for a base-pin bump"
expect_output "Dockerfile" "RED names the Dockerfile input class"

# --- RED: a scanner-stage Dockerfile change after the anchor. ----------------
fx="${tmp_root}/stage"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
sed -i.bak 's|^COPY --from=builder /go-bin/eshu-scanner-component-image /usr/local/bin/eshu-scanner-component-image$|COPY --from=builder /go-bin/eshu-scanner-component-image /usr/local/bin/eshu-scanner-component-image\nCOPY --from=builder /go-bin/eshu-scanner-component-image /usr/local/bin/eshu-scanner-component-image-debug|' "${fx}/Dockerfile" && rm -f "${fx}/Dockerfile.bak"
git -C "${fx}" add -A
git -C "${fx}" commit -qm scanner-stage-change
git -C "${fx}" update-ref refs/remotes/origin/main HEAD
run_verifier "${manifest}" "${stub}"
expect_rc 1 "RED: a scanner-stage Dockerfile change fails"
expect_output "STALE" "RED names the staleness verdict for a stage change"

# --- RED: the scanner publish job changed after the anchor. ------------------
fx="${tmp_root}/workflow"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
sed -i.bak 's/^  IMAGE_PLATFORMS: linux\/amd64,linux\/arm64$/  IMAGE_PLATFORMS: linux\/amd64/' "${fx}/.github/workflows/docker-publish.yml" && rm -f "${fx}/.github/workflows/docker-publish.yml.bak"
git -C "${fx}" add -A
git -C "${fx}" commit -qm scanner-platforms-change
git -C "${fx}" update-ref refs/remotes/origin/main HEAD
run_verifier "${manifest}" "${stub}"
expect_rc 1 "RED: a scanner-job workflow change fails"
expect_output "STALE" "RED names the staleness verdict for a workflow change"
expect_output "docker-publish" "RED names the workflow input class"

# --- GREEN: a comment-only change inside a scanner stage. --------------------
fx="${tmp_root}/comment"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
sed -i.bak 's/^CMD \["eshu-scanner-component-sbom"\]$/# still the sbom entrypoint\nCMD ["eshu-scanner-component-sbom"]/' "${fx}/Dockerfile" && rm -f "${fx}/Dockerfile.bak"
git -C "${fx}" add -A
git -C "${fx}" commit -qm scanner-comment
git -C "${fx}" update-ref refs/remotes/origin/main HEAD
run_verifier "${manifest}" "${stub}"
expect_rc 0 "GREEN: a comment-only change inside a scanner stage passes"

# --- GREEN: an unrelated-stage change (runtime + non-scanner job). -----------
fx="${tmp_root}/unrelated"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
sed -i.bak 's/^RUN apk add --no-cache git curl$/RUN apk add --no-cache git curl htop/' "${fx}/Dockerfile" && rm -f "${fx}/Dockerfile.bak"
sed -i.bak 's/^          target: runtime$/          target: runtime\n          no-cache: true/' "${fx}/.github/workflows/docker-publish.yml" && rm -f "${fx}/.github/workflows/docker-publish.yml.bak"
git -C "${fx}" add -A
git -C "${fx}" commit -qm runtime-change
git -C "${fx}" update-ref refs/remotes/origin/main HEAD
run_verifier "${manifest}" "${stub}"
expect_rc 0 "GREEN: an unrelated-stage change passes"

# --- RED: go.mod advanced after the anchor (recorded as staleness). ----------
fx="${tmp_root}/gomod"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
printf '\nrequire example.com/extra v1.2.3\n' >>"${fx}/go/go.mod"
git -C "${fx}" add -A
git -C "${fx}" commit -qm gomod-bump
git -C "${fx}" update-ref refs/remotes/origin/main HEAD
run_verifier "${manifest}" "${stub}"
expect_rc 1 "RED: a go.mod bump fails"
expect_output "STALE" "RED names the staleness verdict for a go.mod bump"
expect_output "go.mod" "RED names the go.mod input"

# --- Fail closed: bad anchor lines, unknown anchors, missing pins. ----------
fx="${tmp_root}/noanchor"
manifest="$(make_fixture "${fx}" missing)"
stub="$(serve_all "${fx}")"
run_verifier "${manifest}" "${stub}"
expect_rc 2 "a missing anchor line exits 2"
expect_output "FAIL-CLOSED" "a missing anchor line says FAIL-CLOSED"

fx="${tmp_root}/badanchor"
manifest="$(make_fixture "${fx}" malformed)"
stub="$(serve_all "${fx}")"
run_verifier "${manifest}" "${stub}"
expect_rc 2 "a malformed anchor line exits 2"

fx="${tmp_root}/foreign"
manifest="$(make_fixture "${fx}" foreign)"
stub="$(serve_all "${fx}")"
run_verifier "${manifest}" "${stub}"
expect_rc 2 "an anchor outside history exits 2"

# A pin that is not 64-hex fails closed, never passes.
fx="${tmp_root}/shortpin"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
# -i.bak is the in-place form both GNU and BSD (macOS) sed accept (#9626).
sed -i.bak 's/"cbafeeeb2bae99902838e5159bd4003e210f5cfe41891b50b0caf502b2d99dec"/"cbaf"/' "${manifest}" && rm -f "${manifest}.bak"
run_verifier "${manifest}" "${stub}"
expect_rc 2 "a truncated pin exits 2"

# A network-shaped crane failure (no MANIFEST_UNKNOWN marker) fails closed.
fx="${tmp_root}/broken"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
touch "${stub}/broken"
run_verifier "${manifest}" "${stub}"
expect_rc 2 "a crane transport failure exits 2, never 0"

# The verifier must not edit the manifest.
before="$(cksum <"${manifest}")"
stub2="$(serve_all "${tmp_root}/readonly")"
run_verifier "${manifest}" "${stub2}" || true
after="$(cksum <"${manifest}")"
if [[ "${before}" == "${after}" ]]; then record_pass "the verifier leaves the manifest unchanged"; else record_fail "the verifier modified the manifest"; fi

printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
[[ "${FAIL}" -eq 0 ]]
