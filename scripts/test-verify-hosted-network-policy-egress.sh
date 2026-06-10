#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-hosted-network-policy-egress.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -f "${verifier}" ]]; then
	printf 'missing verifier: %s\n' "${verifier}" >&2
	exit 1
fi

default_log="${tmp_dir}/default.log"
bash "${verifier}" >"${default_log}"
rg --fixed-strings --quiet "broad egress mode is a hosted governance risk" "${default_log}"
rg --fixed-strings --quiet "verified restricted collector-provider egress" "${default_log}"
rg --fixed-strings --quiet "verified restricted semantic-provider egress" "${default_log}"
rg --fixed-strings --quiet "verified restricted extension egress" "${default_log}"
# Fail-closed negative cases: a configured destination must not render when its
# class is disabled (provider denied / extension revoked), and restricted mode
# with no provider classes must render no external egress at all.
rg --fixed-strings --quiet "verified restricted missing-policy fail-closed egress" "${default_log}"
rg --fixed-strings --quiet "verified denied collector-provider egress is fail-closed" "${default_log}"
rg --fixed-strings --quiet "verified revoked extension egress is fail-closed" "${default_log}"

invalid_values="${tmp_dir}/invalid-mode.yaml"
cat >"${invalid_values}" <<'YAML'
networkPolicy:
  egress:
    mode: unrestricted
YAML

invalid_log="${tmp_dir}/invalid.log"
if bash "${verifier}" -f "${invalid_values}" >"${invalid_log}" 2>&1; then
	printf 'expected invalid egress mode to fail\n' >&2
	exit 1
fi
rg --fixed-strings --quiet "networkPolicy.egress.mode must be broad or restricted" "${invalid_log}"

restricted_values="${tmp_dir}/restricted.yaml"
cat >"${restricted_values}" <<'YAML'
schemaBootstrap:
  useHelmHooks: false
nornicdb:
  enabled: true
networkPolicy:
  egress:
    mode: restricted
    datastores:
      to:
        - podSelector:
            matchLabels:
              egress.eshu.io/class: datastore
    classes:
      collectorProviders:
        to:
          - namespaceSelector:
              matchLabels:
                egress.eshu.io/class: collector-provider
YAML

restricted_log="${tmp_dir}/restricted.log"
bash "${verifier}" -f "${restricted_values}" >"${restricted_log}"
rg --fixed-strings --quiet "verified restricted NetworkPolicy egress" "${restricted_log}"
