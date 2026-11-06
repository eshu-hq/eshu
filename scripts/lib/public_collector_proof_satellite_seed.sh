#!/usr/bin/env bash
# public_collector_proof_satellite_seed.sh
#
# Satellite-seeding sidecar for verify_local_public_collector_proof.sh.
#
# Seeds the digest-pinned vulnerability-intelligence satellite image into an
# isolated component-home volume and enables the public-proof activation, so
# the component-extension host serves the same three public targets the
# coordinator claims (KEV, EPSS, OSV/npm). It mirrors the remote-e2e
# seed/reconcile one-shots: verify the seed, write the activation file, grant
# the fact kinds, install and enable the component, then hand ownership to the
# runtime uid the worker runs as.
#
# Sourced by the proof script's main(), never executed directly. Consumes the
# proof script's globals: COMPOSE_PROJECT, COMPOSE_CMD, VULN_* below, and the
# PROOF_* bounded knobs.

# Digest-pinned vulnerability-intelligence satellite (eshu-hq/eshu#9999): the
# in-tree collector binary is deleted, so the proof seeds this image into a
# component home and runs the component-extension host against it — the same
# satellite-through-host shape the remote-e2e stack proves. Keep the digest,
# component id, and version in lockstep with the seed one-shot in
# eshu-hq/eshu-deploy compose/docker-compose.remote-e2e.seed.yaml.
VULN_SATELLITE_IMAGE="ghcr.io/eshu-hq/eshu-collector-vulnerability-intelligence@sha256:d0d0f289721e87e941e0fcb6cf4091c96a62b571ce533d956a5ef982490bce5d"
VULN_COMPONENT_ID="dev.eshu.collector.vulnerability_intelligence"
VULN_COMPONENT_VERSION="0.1.3"
VULN_INSTANCE_ID="public-proof-vulnerability-intelligence"

# seed_vulnerability_satellite seeds the digest-pinned satellite into an
# isolated component-home volume and enables the public-proof activation, so
# the component-extension host serves the same three public targets the
# coordinator claims (KEV, EPSS, OSV/npm). It mirrors the remote-e2e
# seed/reconcile one-shots: verify the seed, write the activation file, grant
# the fact kinds, install and enable the component, then hand ownership to the
# runtime uid the worker runs as.
seed_vulnerability_satellite() {
	local volume="${COMPOSE_PROJECT}_vuln_component_home"
	docker volume create "${volume}" >/dev/null
	docker run --rm --user 0:0 -v "${volume}:/components" "${VULN_SATELLITE_IMAGE}" \
		/usr/local/bin/collector seed --dest "/components/runtime/${VULN_INSTANCE_ID}" \
		--image-ref "${VULN_SATELLITE_IMAGE}" >/dev/null
	"${COMPOSE_CMD[@]}" run --rm --no-deps --user 0:0 \
		-v "${volume}:/components" \
		-e ESHU_COMPONENT_HOME=/components \
		-e VULN_COMPONENT_ID="${VULN_COMPONENT_ID}" \
		-e VULN_COMPONENT_VERSION="${VULN_COMPONENT_VERSION}" \
		-e VULN_INSTANCE_ID="${VULN_INSTANCE_ID}" \
		-e PROOF_EPSS_CVE_ID="${PROOF_EPSS_CVE_ID}" \
		-e PROOF_NPM_PACKAGE="${PROOF_NPM_PACKAGE}" \
		workflow-coordinator /bin/sh -c '
			set -eu
			home="${ESHU_COMPONENT_HOME:?ESHU_COMPONENT_HOME must be set}"
			runtime="${home}/runtime/${VULN_INSTANCE_ID:?VULN_INSTANCE_ID must be set}"
			if [ ! -x "${runtime}/collector" ] || [ ! -s "${runtime}/manifest.yaml" ]; then
				echo "seed image did not write ${runtime}/collector and ${runtime}/manifest.yaml" >&2
				exit 1
			fi
			config="${home}/activations/${VULN_INSTANCE_ID}.yaml"
			mkdir -p "$(dirname "${config}")"
			cat > "${config}" <<ACTIVATION
config:
  collector_instance_id: '"${VULN_INSTANCE_ID}"'
  targets:
    - source: cisa_kev
      scope_id: "vuln-intel://cisa/kev"
    - source: first_epss
      scope_id: "vuln-intel://first/epss"
      cve_ids: ["'"${PROOF_EPSS_CVE_ID}"'"]
    - source: osv
      scope_id: "vuln-intel://osv/npm/public-proof"
      ecosystem: npm
      queries:
        - package:
            name: "'"${PROOF_NPM_PACKAGE}"'"
            ecosystem: npm
host:
  scope:
    id: "vuln-intel://cisa/kev"
    kind: vulnerability_intelligence
  sourceSystem: vulnerability_intelligence
process:
  args:
    - --sdk-stdio
  command: /components/runtime/'"${VULN_INSTANCE_ID}"'/collector
  stdout_limit_bytes: 67108864
ACTIVATION
			eshu component grant "${VULN_COMPONENT_ID}" --component-home "${home}" --version "${VULN_COMPONENT_VERSION}" --kind vulnerability.source_snapshot --schema-version 1.0.0 --scope vulnerability_intelligence --expires-in 8760h
			eshu component grant "${VULN_COMPONENT_ID}" --component-home "${home}" --version "${VULN_COMPONENT_VERSION}" --kind vulnerability.cve --schema-version 1.0.0 --scope vulnerability_intelligence --expires-in 8760h
			eshu component grant "${VULN_COMPONENT_ID}" --component-home "${home}" --version "${VULN_COMPONENT_VERSION}" --kind vulnerability.affected_package --schema-version 1.0.0 --scope vulnerability_intelligence --expires-in 8760h
			eshu component grant "${VULN_COMPONENT_ID}" --component-home "${home}" --version "${VULN_COMPONENT_VERSION}" --kind vulnerability.epss_score --schema-version 1.0.0 --scope vulnerability_intelligence --expires-in 8760h
			eshu component grant "${VULN_COMPONENT_ID}" --component-home "${home}" --version "${VULN_COMPONENT_VERSION}" --kind vulnerability.known_exploited --schema-version 1.0.0 --scope vulnerability_intelligence --expires-in 8760h
			eshu component grant "${VULN_COMPONENT_ID}" --component-home "${home}" --version "${VULN_COMPONENT_VERSION}" --kind vulnerability.reference --schema-version 1.0.0 --scope vulnerability_intelligence --expires-in 8760h
			eshu component grant "${VULN_COMPONENT_ID}" --component-home "${home}" --version "${VULN_COMPONENT_VERSION}" --kind vulnerability.warning --schema-version 1.0.0 --scope vulnerability_intelligence --expires-in 8760h
			eshu component install "${runtime}/manifest.yaml" --component-home "${home}" --trust-mode allowlist --allow-id "${VULN_COMPONENT_ID}" --allow-publisher eshu-hq
			eshu component enable "${VULN_COMPONENT_ID}" --component-home "${home}" --instance "${VULN_INSTANCE_ID}" --mode scheduled --claims --config "${config}" || {
				status=$?
				echo "component enable failed with status ${status}" >&2
				exit "${status}"
			}
			chown -R 10001:10001 "${home}"
		'
}
