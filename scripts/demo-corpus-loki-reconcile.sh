#!/bin/sh
# Demo-corpus loki component reconcile: the host-image half of the live-QA
# host-image-plus-seed shape (#9202).
#
# The loki producer is extracted: the satellite image
# (ghcr.io/eshu-hq/eshu-collector-loki) carries the only loki collector
# binary, and it speaks SDK-stdio only (no -mode=cassette, no status
# server, no shell). The collector-loki-seed service runs that image's
# `collector seed` into the shared component volume; this step runs in the
# host product image (which has sh and the eshu CLI), verifies the seeded
# package, records the core-issued producer grants the seeded manifest
# declares, installs the allowlisted manifest, and enables the
# claims-capable instance the collector-loki worker
# (eshu-collector-component-extension) runs.
#
# The credential-free demo carries no live Loki target, so the activation
# ships an empty target list: the worker starts healthy and idles without
# claims until live QA supplies targets through this same activation
# shape. The cassette bind on collector-loki is retained for the
# follow-up replay wiring.
set -eu

component_home="${ESHU_COMPONENT_HOME:?ESHU_COMPONENT_HOME must be set}"
instance="${ESHU_COMPONENT_COLLECTOR_INSTANCE_ID:?ESHU_COMPONENT_COLLECTOR_INSTANCE_ID must be set}"
component_id="dev.eshu.collector.loki"
component_version="0.1.2"
component_publisher="eshu-hq"

seed_dir="${component_home}/runtime/${instance}"
seed_binary="${seed_dir}/collector"
seed_manifest="${seed_dir}/manifest.yaml"
activation_path="${seed_dir}/activation.yaml"

log() { printf '\n=== loki-reconcile: %s ===\n' "$*"; }

[ -x "${seed_binary}" ] || {
	echo "loki-reconcile failed: seeded collector ${seed_binary} is missing or not executable" >&2
	exit 1
}
[ -f "${seed_manifest}" ] || {
	echo "loki-reconcile failed: seeded manifest ${seed_manifest} is missing" >&2
	exit 1
}
# The seed container runs as root into a shared volume while the worker
# runs as the product image's eshu user, so the package must be
# world-readable before install and before the worker starts.
chmod -R a+rX "${seed_dir}"
# Fail closed when the seed did not carry the release this lane pins.
# Install would refuse the mismatch later, but this names the expected
# pair up front, mirroring the chart's reconcile init container.
grep -qF "id: ${component_id}" "${seed_manifest}" || {
	echo "loki-reconcile failed: ${seed_manifest} does not carry component id ${component_id}" >&2
	exit 1
}
grep -qF "version: ${component_version}" "${seed_manifest}" || {
	echo "loki-reconcile failed: ${seed_manifest} does not carry version ${component_version}" >&2
	exit 1
}

log "record producer grants for the manifest's four emitted kinds"
for kind in observability.source_instance observability.observed_log_signal observability.observed_rule observability.coverage_warning; do
	/usr/local/bin/eshu component grant "${component_id}" \
		--component-home "${component_home}" \
		--version "${component_version}" \
		--kind "${kind}" \
		--schema-version 1.0.0 \
		--scope loki \
		--expires-in 720h
done

log "install allowlisted manifest"
/usr/local/bin/eshu component install "${seed_manifest}" \
	--component-home "${component_home}" \
	--trust-mode allowlist \
	--allow-id "${component_id}" \
	--allow-publisher "${component_publisher}"

log "write activation config"
# Credential-free demo: no live Loki target. The satellite requires at
# least one enabled target per collection, so with no targets the worker
# starts healthy and idles until live QA supplies targets here.
cat >"${activation_path}" <<CFG
host:
  sourceSystem: loki
  scope:
    id: loki:supply-chain-demo-logs:supply-chain-demo
    kind: log_source
process:
  command: ${seed_binary}
  args:
    - --sdk-stdio
config:
  targets: []
CFG
chmod a+r "${activation_path}"

log "enable claims-capable instance ${instance}"
/usr/local/bin/eshu component enable "${component_id}" \
	--component-home "${component_home}" \
	--instance "${instance}" \
	--mode scheduled \
	--claims \
	--config "${activation_path}"

# The registry (registry.json, grants, installed packages) is written by
# this root step with owner-only modes; the worker reads it as the product
# image's eshu user, so the whole home must be world-readable before the
# worker starts (same reason the component-extension proof init relaxes its
# shared home for uid 10001).
chmod -R a+rX "${component_home}"

log "loki component reconcile PASS"
