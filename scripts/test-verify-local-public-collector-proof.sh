#!/usr/bin/env bash
set -euo pipefail

# Deterministic, no-network unit test for verify_local_public_collector_proof.sh.
#
# Covers the cheap, side-effect-free paths: --help, --check (tooling +
# claim-instance JSON shape), and argument rejection. The live proof path is
# exercised by running the script itself against Docker Compose and public
# endpoints; that is intentionally out of scope for this unit test.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify_local_public_collector_proof.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
mkdir -p "${fake_bin}"

# Minimal stubs so --check can validate tooling without a real Docker engine.
for tool in docker curl nc; do
	cat >"${fake_bin}/${tool}" <<'SH'
#!/usr/bin/env bash
# docker compose version must succeed so resolve_compose_cmd picks "docker compose".
if [[ "${1:-}" == "compose" && "${2:-}" == "version" ]]; then
	exit 0
fi
exit 0
SH
	chmod +x "${fake_bin}/${tool}"
done

run_check() {
	PATH="${fake_bin}:${PATH}" "${script}" "$@"
}

# --help exits 0 and never requires Docker.
if ! "${script}" --help >/dev/null; then
	echo "expected --help to exit 0" >&2
	exit 1
fi

# --check passes with tooling present and validates the claim JSON shape.
check_out="${tmp_root}/check.out"
if ! run_check --check >"${check_out}" 2>&1; then
	echo "expected --check to pass with required tooling present" >&2
	cat "${check_out}" >&2
	exit 1
fi
if ! rg -q 'public-collector proof preflight passed' "${check_out}"; then
	echo "expected --check success banner" >&2
	cat "${check_out}" >&2
	exit 1
fi
# Public-safe contract: the proof must name the public lanes and exclude NVD.
for needle in 'CISA KEV' 'FIRST EPSS' 'OSV' 'npm' 'NVD is excluded'; do
	if ! rg -Fq "${needle}" "${check_out}"; then
		echo "expected --check output to mention ${needle}" >&2
		cat "${check_out}" >&2
		exit 1
	fi
done

# --dry-run is an alias for --check.
if ! run_check --dry-run >/dev/null 2>&1; then
	echo "expected --dry-run to behave like --check" >&2
	exit 1
fi

# Unknown arguments are rejected with a non-zero exit.
if run_check --bogus >/dev/null 2>&1; then
	echo "expected unknown argument to be rejected" >&2
	exit 1
fi

# Public-safety static check: the script must not embed credentials, tokens, or
# internal hostnames. Only public registries/sources are allowed.
if rg -nI 'password=|secret=|jfrog\.io|boatsgroup|10\.[0-9]+\.[0-9]+\.[0-9]+' "${script}"; then
	echo "verify_local_public_collector_proof.sh must not embed secrets or internal locators" >&2
	exit 1
fi

# The generated claim instances must reference only public sources. NVD is
# key-gated and must never appear as a configured source or env wiring.
# Extract and validate the claim JSON the script would emit by evaluating only
# its builder function in an isolated shell, never main().
claim_json="$(bash -c '
	set -euo pipefail
	# Stub the entrypoint so sourcing does not start the proof.
	PROOF_EPSS_CVE_ID="CVE-0000-0000"
	PROOF_NPM_PACKAGE="examplepkg"
	PROOF_NPM_VERSION_LIMIT=5
	# Pull just the builder function out of the script.
	eval "$(sed -n "/^build_public_claim_instances()/,/^}/p" "$1")"
	build_public_claim_instances
' _ "${script}")"

if ! printf '%s' "${claim_json}" | jq -e 'type == "array" and length == 2' >/dev/null; then
	echo "expected two public claim instances" >&2
	printf '%s\n' "${claim_json}" >&2
	exit 1
fi
if printf '%s' "${claim_json}" | jq -e '
	[.. | objects | select(has("source")) | .source]
	| any(. == "nvd")
' >/dev/null; then
	echo "claim instances must not configure the key-gated NVD source" >&2
	exit 1
fi
# Affirm the three required public vuln sources and the public npm registry.
for expected_source in cisa_kev first_epss osv; do
	if ! printf '%s' "${claim_json}" | jq -e --arg s "${expected_source}" '
		[.. | objects | select(has("source")) | .source] | any(. == $s)
	' >/dev/null; then
		echo "claim instances missing required public source ${expected_source}" >&2
		exit 1
	fi
done
if ! printf '%s' "${claim_json}" | jq -e '
	[.. | objects | select(has("registry")) | .registry]
	| any(. == "https://registry.npmjs.org")
' >/dev/null; then
	echo "claim instances missing the public npm registry" >&2
	exit 1
fi
if rg -nI 'NVD_API_KEY|api_key_env' "${script}"; then
	echo "verify_local_public_collector_proof.sh must not wire NVD credentials" >&2
	exit 1
fi

# Compose project isolation: the script must declare a COMPOSE_PROJECT variable
# and pass -p to every compose invocation via COMPOSE_CMD so this gate's
# `down -v` never removes a developer's default `eshu` project volumes.
if ! rg -q 'COMPOSE_PROJECT' "${script}"; then
	echo "verify_local_public_collector_proof.sh must declare COMPOSE_PROJECT for volume isolation" >&2
	exit 1
fi
if ! rg -q -- '-p.*COMPOSE_PROJECT' "${script}"; then
	echo "verify_local_public_collector_proof.sh must pass -p \"\${COMPOSE_PROJECT}\" to compose" >&2
	exit 1
fi

# Per-source assertions: the assertions sidecar lib must exist and export the
# three required assertion functions.
assertions_lib="${repo_root}/scripts/lib/public_collector_proof_assertions.sh"
if [[ ! -f "${assertions_lib}" ]]; then
	echo "expected scripts/lib/public_collector_proof_assertions.sh to exist" >&2
	exit 1
fi
for fn in assert_kev_evidence assert_epss_evidence assert_osv_evidence; do
	if ! rg -q "^${fn}()" "${assertions_lib}"; then
		echo "assertions lib missing required function ${fn}" >&2
		exit 1
	fi
done
# Public-safety check on the assertions lib: no credentials or internal locators.
if rg -nI 'password=|secret=|jfrog\.io|boatsgroup|10\.[0-9]+\.[0-9]+\.[0-9]+|NVD_API_KEY' "${assertions_lib}"; then
	echo "public_collector_proof_assertions.sh must not embed secrets or internal locators" >&2
	exit 1
fi

printf 'test-verify-local-public-collector-proof tests passed\n'
