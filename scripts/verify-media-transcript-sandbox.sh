#!/usr/bin/env bash
set -euo pipefail

# Sandbox proof for the whisper.cpp media transcript engine (#9729).
#
# Builds scripts/lib/media-transcript-sandbox/Dockerfile (pinned whisper.cpp
# v1.9.5, pinned ggml-base.en model, espeak-ng speech fixture, and the Go
# whispercpp test binary compiled with -tags whispercpp_sandbox), verifies
# the model digest on the host and records the built binary digest, then runs
# the proof tests under the gate-2 sandbox profile, where New verifies both
# digests inside the container:
#
#   --network none --read-only --cap-drop ALL --security-opt no-new-privileges
#   --user 65532:65532 --memory 2g --memory-swap 2g --pids-limit 64 --cpus 4
#
# It asserts every proof test passed and that the password canary never
# appears in test output or engine run logs, and prints the EVIDENCE lines.
#
# Requires docker with BuildKit, and network access for the image build only.
# The proof run itself has no network.
#
# Usage (from repo root): scripts/verify-media-transcript-sandbox.sh
# ESHU_SANDBOX_WORKDIR_BASE (default ~/.cache) holds one mktemp directory per
# run; the proof log stays there after the run.

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
runtime="${ESHU_OCI_RUNTIME:-docker}"
image="eshu-media-transcript-sandbox:local"
model_sha256="a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002"
work_base="${ESHU_SANDBOX_WORKDIR_BASE:-${HOME}/.cache}"

die() {
	printf 'verify-media-transcript-sandbox: %s\n' "$*" >&2
	exit 1
}

command -v "${runtime}" >/dev/null || die "${runtime} is not installed"
command -v go >/dev/null || die "go is not installed"
command -v rg >/dev/null || die "rg is not installed"

# Each run gets its own mktemp directory. The script never deletes a path
# it did not create: cleanup removes only the build inputs inside this run's
# directory and keeps proof.log for the evidence record.
[[ "${work_base}" == /* ]] || die "work base ${work_base} must be an absolute path"
mkdir -p "${work_base}"
work="$(mktemp -d "${work_base}/eshu-media-transcript-sandbox.XXXXXX")"
[[ -d "${work}" && "${work}" == "${work_base}"/eshu-media-transcript-sandbox.?????? ]] ||
	die "mktemp returned an unexpected path: ${work}"
mkdir "${work}/ctx" "${work}/extract"
container=""
cleanup() {
	if [[ -n "${container}" ]]; then
		"${runtime}" rm -f "${container}" >/dev/null 2>&1 || true
	fi
	rm -rf -- "${work:?}/ctx" "${work:?}/extract"
}
trap cleanup EXIT

arch="$("${runtime}" version --format '{{.Server.Arch}}')"
(
	cd "${repo_root}/go"
	CGO_ENABLED=0 GOOS=linux GOARCH="${arch}" go test -c -tags whispercpp_sandbox \
		-o "${work}/ctx/whispercpp.test" ./internal/collector/document/media/whispercpp
)

"${runtime}" build -f "${repo_root}/scripts/lib/media-transcript-sandbox/Dockerfile" \
	-t "${image}" "${work}/ctx"

container="$("${runtime}" create "${image}")"
"${runtime}" cp "${container}:/opt/whisper/bin/whisper-cli" "${work}/extract/whisper-cli"
"${runtime}" cp "${container}:/opt/whisper/models/ggml-base.en.bin" "${work}/extract/model.bin"
"${runtime}" cp "${container}:/opt/whisper/fixtures/speech.wav" "${work}/extract/speech.wav"
binary_sha256="$(sha256sum "${work}/extract/whisper-cli" | cut -d' ' -f1)"
got_model="$(sha256sum "${work}/extract/model.bin" | cut -d' ' -f1)"
[[ "${got_model}" == "${model_sha256}" ]] || die "model digest ${got_model} != pinned ${model_sha256}"
printf 'EVIDENCE whisper-cli sha256=%s\n' "${binary_sha256}"
printf 'EVIDENCE model ggml-base.en.bin sha256=%s\n' "${got_model}"
printf 'EVIDENCE fixture speech.wav sha256=%s bytes=%s\n' \
	"$(sha256sum "${work}/extract/speech.wav" | cut -d' ' -f1)" "$(wc -c <"${work}/extract/speech.wav")"

log="${work}/proof.log"
set +e
"${runtime}" run --rm --network none --read-only --cap-drop ALL \
	--security-opt no-new-privileges:true --user 65532:65532 \
	--memory 2g --memory-swap 2g --pids-limit 64 --cpus 4 \
	-e ESHU_WHISPERCPP_BINARY=/opt/whisper/bin/whisper-cli \
	-e ESHU_WHISPERCPP_BINARY_SHA256="${binary_sha256}" \
	-e ESHU_WHISPERCPP_MODEL=/opt/whisper/models/ggml-base.en.bin \
	-e ESHU_WHISPERCPP_MODEL_SHA256="${model_sha256}" \
	-e ESHU_WHISPERCPP_VERSION=v1.9.5 \
	-e ESHU_WHISPERCPP_SPEECH_FIXTURE=/opt/whisper/fixtures/speech.wav \
	"${image}" -test.v -test.count=1 -test.run '^TestSandbox' >"${log}" 2>&1
status=$?
set -e

rg --no-filename 'EVIDENCE|^--- |^(ok|PASS|FAIL)' "${log}" || true
rg --no-filename -o '"outcome":"[a-z_]+".*"engine_peak_rss_bytes":[0-9]+' "${log}" || true
[[ "${status}" -eq 0 ]] || die "proof tests failed (exit ${status}); log at ${log}"
if rg -qi 'swordfish' "${log}"; then
	die "password canary appeared in proof output or engine logs"
fi
for test_name in TestSandboxProfileIsEnforced TestSandboxRealSpeechRedactedAndDeterministic \
	TestSandboxSilenceIsNoSpeech TestSandboxBoundsFailClosed; do
	rg -q -- "--- PASS: ${test_name} " "${log}" || die "${test_name} did not pass"
done
printf 'verify-media-transcript-sandbox: PASS (log %s)\n' "${log}"
