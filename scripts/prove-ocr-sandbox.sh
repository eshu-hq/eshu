#!/usr/bin/env bash
#
# prove-ocr-sandbox.sh - gate-2 sandbox proof for the tesseract OCR engine
# (#9728, design 1737 section 7.3).
#
# Builds the engine test binary (static, CGO off), bakes it with the pinned
# tesseract 5.5.0 binary, the pinned eng.traineddata, and rendered PNG/JPEG/
# GIF/WebP fixtures into scripts/ocr-sandbox/Dockerfile, verifies both pinned
# digests, then runs TestSandboxRealTesseract inside:
#
#   docker run --network none --read-only --cap-drop ALL
#     --security-opt no-new-privileges --user 65532:65532
#     --memory 512m --memory-swap 512m --pids-limit 64 --cpus 1
#     --tmpfs /tmp:rw,noexec,nosuid,nodev,size=16m
#
# The run needs no network. Building the image does (Alpine packages).
#
# Usage: prove-ocr-sandbox.sh [--platform linux/arm64|linux/amd64]
#
# Environment:
#   OCR_SANDBOX_WORKDIR  build-context parent (default: mktemp under TMPDIR)
#
# Exit codes: 0 proof passed; 1 proof or digest check failed; 2 usage or
# missing tool.
set -euo pipefail

# Pinned digests. eng.traineddata is architecture independent; the
# tesseract binary digest is per architecture for tesseract-ocr=5.5.0-r0.
EXPECTED_MODEL_SHA256="daa0c97d651c19fba3b25e81317cd697e9908c8208090c94c3905381c23fc047"
declare -A EXPECTED_BINARY_SHA256=(
	[arm64]="62a760cd1c23d95211e7c42dddc761c2c9cb9488628941e9be6cd9b6748e9c77"
	[amd64]="38b441bd41257037a03d6a15d9f849d8bd83da4c24dab1df428a7a4ad265332f"
)
ENGINE_VERSION="5.5.0"

usage_error() {
	printf 'prove-ocr-sandbox: %s\n' "$1" >&2
	exit 2
}

platform=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	--platform)
		[[ $# -ge 2 ]] || usage_error "--platform needs a value"
		platform="$2"
		shift 2
		;;
	*) usage_error "unknown argument: $1" ;;
	esac
done

command -v docker >/dev/null || usage_error "docker is required"
command -v go >/dev/null || usage_error "go is required"

if [[ -z "${platform}" ]]; then
	case "$(uname -m)" in
	x86_64) platform="linux/amd64" ;;
	aarch64 | arm64) platform="linux/arm64" ;;
	*) usage_error "unsupported host architecture $(uname -m)" ;;
	esac
fi
arch="${platform#linux/}"
case "$(uname -m)" in
x86_64) host_arch="amd64" ;;
aarch64 | arm64) host_arch="arm64" ;;
*) host_arch="unknown" ;;
esac
# A foreign-architecture run executes under binfmt emulation, where
# RLIMIT_AS lands on the qemu interpreter; the Go test logs that bound as
# not asserted instead of claiming it.
emulated=0
[[ "${arch}" == "${host_arch}" ]] || emulated=1
[[ -n "${EXPECTED_BINARY_SHA256[${arch}]:-}" ]] || usage_error "no pinned tesseract digest for ${arch}"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workdir="$(mktemp -d "${OCR_SANDBOX_WORKDIR:-${TMPDIR:-/tmp}}/eshu-ocr-sandbox.XXXXXX")"
trap 'rm -rf "${workdir}"' EXIT
image="eshu-ocr-sandbox:${arch}"

printf '== build engine test binary (linux/%s, CGO off)\n' "${arch}"
(cd "${repo_root}/go" && CGO_ENABLED=0 GOOS=linux GOARCH="${arch}" \
	go test -c -trimpath -o "${workdir}/ocr-sandbox.test" ./internal/collector/document/ocr/tesseract)
cp "${repo_root}/scripts/ocr-sandbox/Dockerfile" "${workdir}/Dockerfile"

printf '== build proof image %s\n' "${image}"
docker build --quiet --platform "${platform}" -t "${image}" "${workdir}" >/dev/null

printf '== verify pinned digests\n'
digests="$(docker run --rm --platform "${platform}" --network none --entrypoint /usr/bin/sha256sum \
	"${image}" /usr/bin/tesseract /usr/share/tessdata/eng.traineddata)"
binary_sha="$(awk '$2=="/usr/bin/tesseract"{print $1}' <<<"${digests}")"
model_sha="$(awk '$2=="/usr/share/tessdata/eng.traineddata"{print $1}' <<<"${digests}")"
printf 'tesseract sha256:%s\neng.traineddata sha256:%s\n' "${binary_sha}" "${model_sha}"
if [[ "${model_sha}" != "${EXPECTED_MODEL_SHA256}" ]]; then
	printf 'FAIL - eng.traineddata digest %s != pinned %s\n' "${model_sha}" "${EXPECTED_MODEL_SHA256}" >&2
	exit 1
fi
if [[ "${binary_sha}" != "${EXPECTED_BINARY_SHA256[${arch}]}" ]]; then
	printf 'FAIL - tesseract %s digest %s != pinned %s\n' "${arch}" "${binary_sha}" "${EXPECTED_BINARY_SHA256[${arch}]}" >&2
	exit 1
fi

printf '== run proof under the sandbox profile\n'
set +e
docker run --rm --platform "${platform}" \
	--network none --read-only --cap-drop ALL --security-opt no-new-privileges \
	--user 65532:65532 --memory 512m --memory-swap 512m --pids-limit 64 --cpus 1 \
	--tmpfs /tmp:rw,noexec,nosuid,nodev,size=16m \
	-e ESHU_OCR_SANDBOX_PROOF=1 \
	-e ESHU_OCR_SANDBOX_EMULATED="${emulated}" \
	-e ESHU_OCR_TESSERACT_BIN=/usr/bin/tesseract \
	-e ESHU_OCR_TESSERACT_SHA256="${binary_sha}" \
	-e ESHU_OCR_TESSDATA_DIR=/usr/share/tessdata \
	-e ESHU_OCR_MODEL_SHA256="${model_sha}" \
	-e ESHU_OCR_ENGINE_VERSION="${ENGINE_VERSION}" \
	-e ESHU_OCR_SCRATCH_DIR=/tmp \
	-e ESHU_OCR_FIXTURE_DIR=/ocr/fixtures \
	"${image}" -test.run '^TestSandboxRealTesseract$' -test.v -test.count=1
status=$?
set -e
if [[ ${status} -ne 0 ]]; then
	printf 'FAIL - sandbox proof exited %d\n' "${status}" >&2
	exit 1
fi
printf 'PASS - OCR sandbox proof on %s\n' "${platform}"
