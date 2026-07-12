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
		# printf, not a heredoc: this 2199-byte body sits in the 512B-64KB
		# Homebrew-bash-5.1+ pipe-deadlock zone (#5074). It has no variable
		# expansion, so every line is single-quoted.
		printf '%s\n' \
			'apiVersion: apps/v1' \
			'kind: Deployment' \
			'metadata:' \
			'  name: eshu-api' \
			'spec:' \
			'  template:' \
			'    spec:' \
			'      containers:' \
			'        - name: api' \
			'          image: "ghcr.io/eshu-hq/eshu:v9.9.9"' \
			'          env:' \
			'            - name: ESHU_API_KEY' \
			'              valueFrom:' \
			'                secretKeyRef:' \
			'                  name: eshu-api-auth' \
			'                  key: api-key' \
			'            - name: ESHU_GOVERNANCE_MODE' \
			'              value: hosted_single_tenant' \
			'            - name: ESHU_GOVERNANCE_STATE' \
			'              value: enforcing' \
			'            - name: ESHU_GOVERNANCE_SOURCE_KIND' \
			'              value: kubernetes_secret' \
			'            - name: ESHU_GOVERNANCE_AUTH_MODE' \
			'              value: shared_token' \
			'            - name: ESHU_GOVERNANCE_EGRESS_MODE' \
			'              value: restricted' \
			'---' \
			'apiVersion: apps/v1' \
			'kind: Deployment' \
			'metadata:' \
			'  name: eshu-mcp-server' \
			'spec:' \
			'  template:' \
			'    spec:' \
			'      containers:' \
			'        - name: mcp-server' \
			'          env:' \
			'            - name: ESHU_API_KEY' \
			'              valueFrom:' \
			'                secretKeyRef:' \
			'                  name: eshu-api-auth' \
			'                  key: api-key' \
			'            - name: ESHU_GOVERNANCE_MODE' \
			'              value: hosted_single_tenant' \
			'            - name: ESHU_GOVERNANCE_STATE' \
			'              value: enforcing' \
			'            - name: ESHU_GOVERNANCE_SOURCE_KIND' \
			'              value: kubernetes_secret' \
			'            - name: ESHU_GOVERNANCE_AUTH_MODE' \
			'              value: shared_token' \
			'            - name: ESHU_GOVERNANCE_EGRESS_MODE' \
			'              value: restricted' \
			'---' \
			'apiVersion: apps/v1' \
			'kind: StatefulSet' \
			'metadata:' \
			'  name: eshu' \
			'spec:' \
			'  template:' \
			'    spec:' \
			'      containers:' \
			'        - name: ingester' \
			'          image: "ghcr.io/eshu-hq/eshu:v9.9.9"' \
			'---' \
			'apiVersion: apps/v1' \
			'kind: Deployment' \
			'metadata:' \
			'  name: eshu-resolution-engine' \
			'spec:' \
			'  template:' \
			'    spec:' \
			'      containers:' \
			'        - name: resolution-engine' \
			'          image: "ghcr.io/eshu-hq/eshu:v9.9.9"' \
			'---' \
			'apiVersion: batch/v1' \
			'kind: Job' \
			'metadata:' \
			'  name: eshu-schema-bootstrap' \
			'  annotations:' \
			'    "helm.sh/hook": pre-install,pre-upgrade' \
			'---' \
			'apiVersion: networking.k8s.io/v1' \
			'kind: NetworkPolicy' \
			'metadata:' \
			'  name: eshu-api' \
			'  labels:' \
			'    app.kubernetes.io/component: api' \
			'spec:' \
			'  podSelector: {}' \
			'  policyTypes: [Ingress, Egress]' \
			'  egress:'
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
