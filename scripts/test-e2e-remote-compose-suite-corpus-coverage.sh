#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
HARNESS="${REPO_ROOT}/scripts/e2e_remote_compose_suite.sh"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

fake_bin="${TMP_DIR}/bin"
mkdir -p "${fake_bin}"
printf '#!/usr/bin/env bash\nexit 0\n' >"${fake_bin}/docker"
printf '#!/usr/bin/env bash\nexit 0\n' >"${fake_bin}/curl"
chmod +x "${fake_bin}/docker" "${fake_bin}/curl"

volume_proof="${TMP_DIR}/volume.json"
coverage="${TMP_DIR}/corpus-coverage.json"
readback="${TMP_DIR}/readback.json"
manifest="${TMP_DIR}/manifest.json"

jq -n '{
	schema_version: 1,
	run_kind: "clean",
	clean_volume_state: "reset_before_run",
	backing_stores: {
		nornicdb_data: {status: "pass", before: "absent", after: "present"},
		postgres_data: {status: "pass", before: "absent", after: "present"},
		eshu_data: {status: "pass", before: "absent", after: "present"}
	}
}' >"${volume_proof}"

jq -n '{
	schema_version: 1,
	proof_id: "corpus-coverage-suite-test",
	surfaces: {
		api: {status: "pass", checked: 1, failed: 0, truncated: 0},
		mcp: {status: "pass", checked: 1, failed: 0, truncated: 0},
		cli: {status: "pass", checked: 1, failed: 0, truncated: 0}
	}
}' >"${readback}"

jq -n '{
	schema_version: 1,
	mode: "representative",
	repository_count: 29,
	ecosystems: {npm: {status: "pass", count: 1}},
	evidence_families: {terraform_iac: {status: "pass", count: 1}}
}' >"${coverage}"

missing_count_coverage="${TMP_DIR}/corpus-coverage-missing-count.json"
jq 'del(.repository_count)' "${coverage}" >"${missing_count_coverage}"
if PATH="${fake_bin}:${PATH}" "${HARNESS}" \
	--run-kind clean \
	--manifest "${manifest}" \
	--api-base-url "http://127.0.0.1:18080/api/v0" \
	--pprof-base-url "http://127.0.0.1:16060" \
	--runtime-volume-proof "${volume_proof}" \
	--out-dir "${TMP_DIR}/evidence-missing-count" \
	--corpus-mode representative \
	--repository-count 24 \
	--corpus-coverage "${missing_count_coverage}" \
	--readback-proof "${readback}" \
	--image-tag-candidate v0.0.3-pre-release-test \
	>"${TMP_DIR}/suite-missing-count.out" 2>"${TMP_DIR}/suite-missing-count.err"; then
	printf 'expected missing corpus coverage repository_count to fail\n' >&2
	exit 1
fi

rg --fixed-strings --quiet -- \
	"corpus-coverage must contain schema_version, repository_count, ecosystems, and evidence_families" \
	"${TMP_DIR}/suite-missing-count.err" || {
	printf 'expected corpus coverage shape failure to name repository_count\n' >&2
	sed -n '1,120p' "${TMP_DIR}/suite-missing-count.err" >&2
	exit 1
}

if PATH="${fake_bin}:${PATH}" "${HARNESS}" \
	--run-kind clean \
	--manifest "${manifest}" \
	--api-base-url "http://127.0.0.1:18080/api/v0" \
	--pprof-base-url "http://127.0.0.1:16060" \
	--runtime-volume-proof "${volume_proof}" \
	--out-dir "${TMP_DIR}/evidence" \
	--corpus-mode representative \
	--repository-count 24 \
	--corpus-coverage "${coverage}" \
	--readback-proof "${readback}" \
	--image-tag-candidate v0.0.3-pre-release-test \
	>"${TMP_DIR}/suite.out" 2>"${TMP_DIR}/suite.err"; then
	printf 'expected mismatched corpus coverage repository_count to fail\n' >&2
	exit 1
fi

rg --fixed-strings --quiet -- \
	"corpus-coverage repository_count must match --repository-count" \
	"${TMP_DIR}/suite.err" || {
	printf 'expected corpus coverage repository_count mismatch failure\n' >&2
	sed -n '1,120p' "${TMP_DIR}/suite.err" >&2
	exit 1
}

printf 'e2e remote compose suite corpus coverage tests passed\n'
