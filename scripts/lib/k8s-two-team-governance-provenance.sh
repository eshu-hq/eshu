#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq

# capture_k8s_governance_provenance records the configured NornicDB index, the
# node platform, the raw image identity reported by the Pod runtime, and the
# binary-reported version. Kubernetes runtimes may report either the configured
# multi-architecture index or the selected platform child. The caller supplies
# kc() and die() in the current shell.
capture_k8s_governance_provenance() {
	local repo_root="$1"
	local artifacts_dir="$2"
	local nornicdb_image="$3"
	local eshu_commit k8s_version nornicdb_pod nornicdb_node nornicdb_arch
	local nornicdb_configured_image nornicdb_runtime_image_id nornicdb_version
	local provenance_file provenance_tmp

	eshu_commit="$(git -C "${repo_root}" rev-parse --short HEAD 2>/dev/null || echo unknown)"
	k8s_version="$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.kubeletVersion}' 2>/dev/null | rg -o '^v[0-9][^[:space:]]*' | head -1 || echo unknown)"
	[[ -n "${k8s_version}" ]] || k8s_version="unknown"
	nornicdb_pod="$(kc get pods -l app.kubernetes.io/component=nornicdb -o jsonpath='{.items[0].metadata.name}')"
	[[ -n "${nornicdb_pod}" ]] || die "cannot resolve the running NornicDB pod"
	nornicdb_node="$(kc get pod "${nornicdb_pod}" -o jsonpath='{.spec.nodeName}')"
	[[ -n "${nornicdb_node}" ]] || die "NornicDB pod has no assigned node"
	nornicdb_arch="$(kubectl get node "${nornicdb_node}" -o jsonpath='{.status.nodeInfo.architecture}')"
	[[ -n "${nornicdb_arch}" ]] || die "cannot resolve NornicDB node architecture"
	nornicdb_configured_image="$(kc get pod "${nornicdb_pod}" -o jsonpath='{.spec.containers[?(@.name=="nornicdb")].image}')"
	[[ -n "${nornicdb_configured_image}" ]] || die "NornicDB pod has no configured container image"
	[[ "${nornicdb_configured_image}" == "${nornicdb_image}" ]] \
		|| die "NornicDB pod image is ${nornicdb_configured_image}, want ${nornicdb_image}"
	nornicdb_runtime_image_id="$(kc get pod "${nornicdb_pod}" -o jsonpath='{.status.containerStatuses[?(@.name=="nornicdb")].imageID}')"
	[[ -n "${nornicdb_runtime_image_id}" ]] || die "NornicDB pod has no runtime imageID"
	nornicdb_version="$(kc exec "${nornicdb_pod}" -c nornicdb -- /app/nornicdb version 2>/dev/null | tr -d '\r' | tail -1)"
	[[ "${nornicdb_version}" == "NornicDB v1.3.1" ]] \
		|| die "NornicDB runtime version is ${nornicdb_version:-missing}, want NornicDB v1.3.1"

	provenance_file="${artifacts_dir}/provenance.json"
	provenance_tmp="$(mktemp "${artifacts_dir}/.provenance.XXXXXX")"
	if ! jq -n \
		--arg eshu_commit "${eshu_commit}" \
		--arg backend_image "${nornicdb_configured_image}" \
		--arg backend_platform "linux/${nornicdb_arch}" \
		--arg backend_runtime_image_id "${nornicdb_runtime_image_id}" \
		--arg backend_version "${nornicdb_version}" \
		--arg kubernetes_version "${k8s_version}" \
		'{
			eshu_commit: $eshu_commit,
			backend: "nornicdb",
			backend_image: $backend_image,
			backend_platform: $backend_platform,
			backend_runtime_image_id: $backend_runtime_image_id,
			backend_version: $backend_version,
			backend_source_revision: "unavailable",
			platform: "kubernetes",
			kubernetes_version: $kubernetes_version,
			registry_token_count: 3,
			metrics_handle: ":9464/metrics",
			counts_and_states_only: true
		}' >"${provenance_tmp}"; then
		rm -f "${provenance_tmp}"
		die "cannot serialize Kubernetes provenance"
	fi
	if ! mv "${provenance_tmp}" "${provenance_file}"; then
		rm -f "${provenance_tmp}"
		die "cannot publish Kubernetes provenance"
	fi
}
