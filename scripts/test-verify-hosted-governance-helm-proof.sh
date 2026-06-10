#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-hosted-governance-helm-proof.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -f "${verifier}" ]]; then
	printf 'missing verifier: %s\n' "${verifier}" >&2
	exit 1
fi

bash -n "${verifier}"

install_fake_helm() {
	local bin_dir="$1"
	mkdir -p "${bin_dir}"
	cat >"${bin_dir}/helm" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
cmd="$1"
shift
values_mode="restricted"
while [[ $# -gt 0 ]]; do
	case "$1" in
		-f|--values)
			if rg --quiet 'mode:[[:space:]]*broad' "$2"; then
				values_mode="broad"
			fi
			shift 2
			;;
		*)
			shift
			;;
	esac
done
case "${cmd}" in
	lint)
		printf 'lint ok\n'
		;;
	upgrade)
		printf 'DRY RUN\n'
		;;
	template)
		cat <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: eshu-api
spec:
  template:
    spec:
      containers:
        - name: api
          image: "ghcr.io/eshu-hq/eshu:v9.9.9"
          env:
            - name: ESHU_API_KEY
              valueFrom:
                secretKeyRef:
                  name: eshu-api-auth
                  key: api-key
            - name: ESHU_GOVERNANCE_MODE
              value: hosted_single_tenant
            - name: ESHU_GOVERNANCE_STATE
              value: enforcing
            - name: ESHU_GOVERNANCE_SOURCE_KIND
              value: kubernetes_secret
            - name: ESHU_GOVERNANCE_AUTH_MODE
              value: shared_token
            - name: ESHU_GOVERNANCE_EGRESS_MODE
              value: restricted
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: eshu-mcp-server
spec:
  template:
    spec:
      containers:
        - name: mcp-server
          env:
            - name: ESHU_API_KEY
              valueFrom:
                secretKeyRef:
                  name: eshu-api-auth
                  key: api-key
            - name: ESHU_GOVERNANCE_MODE
              value: hosted_single_tenant
            - name: ESHU_GOVERNANCE_STATE
              value: enforcing
            - name: ESHU_GOVERNANCE_SOURCE_KIND
              value: kubernetes_secret
            - name: ESHU_GOVERNANCE_AUTH_MODE
              value: shared_token
            - name: ESHU_GOVERNANCE_EGRESS_MODE
              value: restricted
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: eshu
spec:
  template:
    spec:
      containers:
        - name: ingester
          image: "ghcr.io/eshu-hq/eshu:v9.9.9"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: eshu-resolution-engine
spec:
  template:
    spec:
      containers:
        - name: resolution-engine
          image: "ghcr.io/eshu-hq/eshu:v9.9.9"
---
apiVersion: batch/v1
kind: Job
metadata:
  name: eshu-schema-bootstrap
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: eshu-api
  labels:
    app.kubernetes.io/component: api
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
  egress:
YAML
		if [[ "${values_mode}" == "broad" ]]; then
			cat <<'YAML'
    - {}
YAML
		else
			cat <<'YAML'
    - to:
        - podSelector:
            matchLabels:
              egress.eshu.io/class: datastore
      ports:
        - protocol: TCP
          port: 5432
YAML
		fi
		;;
	*)
		printf 'unexpected helm command: %s\n' "${cmd}" >&2
		exit 1
		;;
esac
SH
	chmod +x "${bin_dir}/helm"
}

write_values() {
	local path="$1"
	local mode="$2"
	cat >"${path}" <<YAML
networkPolicy:
  egress:
    mode: ${mode}
    datastores:
      to:
        - podSelector:
            matchLabels:
              egress.eshu.io/class: datastore
api:
  env:
    ESHU_GOVERNANCE_MODE: hosted_single_tenant
    ESHU_GOVERNANCE_STATE: enforcing
    ESHU_GOVERNANCE_SOURCE_KIND: kubernetes_secret
    ESHU_GOVERNANCE_AUTH_MODE: shared_token
    ESHU_GOVERNANCE_EGRESS_MODE: restricted
mcpServer:
  env:
    ESHU_GOVERNANCE_MODE: hosted_single_tenant
    ESHU_GOVERNANCE_STATE: enforcing
    ESHU_GOVERNANCE_SOURCE_KIND: kubernetes_secret
    ESHU_GOVERNANCE_AUTH_MODE: shared_token
    ESHU_GOVERNANCE_EGRESS_MODE: restricted
YAML
}

bin_dir="${tmp_dir}/bin"
install_fake_helm "${bin_dir}"
valid_values="${tmp_dir}/governance-values.yaml"
write_values "${valid_values}" "restricted"

out_dir="${tmp_dir}/proof"
if ! PATH="${bin_dir}:${PATH}" "${verifier}" \
	--out-dir "${out_dir}" \
	--values "${valid_values}" \
	> "${tmp_dir}/valid.out" 2>"${tmp_dir}/valid.err"; then
	printf 'expected valid governance Helm proof to pass\n' >&2
	sed -n '1,120p' "${tmp_dir}/valid.out" >&2
	sed -n '1,120p' "${tmp_dir}/valid.err" >&2
	exit 1
fi

artifact="${out_dir}/hosted-governance-helm-proof.json"
summary="${out_dir}/hosted-governance-helm-proof.md"
[[ -f "${artifact}" && -f "${summary}" ]] || {
	printf 'expected hosted governance Helm proof artifacts\n' >&2
	sed -n '1,120p' "${tmp_dir}/valid.err" >&2
	exit 1
}

jq -e '
	.status == "pass" and
	.helm_rollout_status == "pass" and
	.security_posture_status == "pass" and
	.network_policy_status == "pass" and
	.governance_status_env.api == "pass" and
	.governance_status_env.mcp == "pass" and
	.public_artifact_review == "pass"
' "${artifact}" >/dev/null || {
	printf 'expected public-safe proof fields\n' >&2
	jq . "${artifact}" >&2
	exit 1
}
rg --fixed-strings --quiet 'Hosted governance Helm proof' "${summary}"

broad_values="${tmp_dir}/broad-values.yaml"
write_values "${broad_values}" "broad"
if PATH="${bin_dir}:${PATH}" "${verifier}" --out-dir "${tmp_dir}/broad" --values "${broad_values}" \
	>"${tmp_dir}/broad.out" 2>"${tmp_dir}/broad.err"; then
	printf 'expected broad egress values to fail\n' >&2
	exit 1
fi
rg --fixed-strings --quiet 'networkPolicy.egress.mode must be restricted' "${tmp_dir}/broad.err"

missing_values_out="${tmp_dir}/missing-values"
if PATH="${bin_dir}:${PATH}" "${verifier}" --out-dir "${missing_values_out}" \
	>"${tmp_dir}/missing.out" 2>"${tmp_dir}/missing.err"; then
	printf 'expected missing values to fail\n' >&2
	exit 1
fi
rg --fixed-strings --quiet 'at least one --values file is required' "${tmp_dir}/missing.err"

printf 'hosted governance Helm proof verifier tests passed\n'
