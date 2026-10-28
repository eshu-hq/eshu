#!/bin/sh
# Tier-2 tfstate drift component reconcile: the host-image half of the
# satellite worker shape (eshu-hq/eshu#9288).
#
# The terraform-state producer is extracted: the satellite image
# (ghcr.io/eshu-hq/eshu-collector-terraform-state) carries the only
# terraform-state collector binary, and it speaks SDK-stdio only. A
# *-seed service runs that image's `collector seed` into the shared
# component home; this step runs in the host product image (which has sh
# and the eshu CLI), verifies the seeded package, records the
# core-issued producer grants the seeded manifest declares, installs the
# allowlisted manifest, writes the activation file carrying the MinIO
# discovery the satellite collects, and enables the claims-capable
# instance the collector-terraform-state worker
# (eshu-collector-component-extension) runs.
#
# Inputs (all required unless noted):
#   ESHU_COMPONENT_HOME            shared component home (volume)
#   INSTANCE_ID                    collector instance id (tier2-terraform-state,
#                                  tier2-v25-terraform-state-gen1/-gen2)
#   TFSTATE_SEED_BUCKETS           space-separated MinIO buckets to collect
#                                  (v1: eshu-drift-a/b/d/e; v25: eshu-drift-c/f)
#   TFSTATE_SCOPE_PREFIX           target_scope_id prefix (v1: tier2; v25: tier2-v25);
#                                  scope per bucket is <prefix>-<bucket suffix>
#   TFSTATE_STATE_KEY              object key in each bucket (default prod/terraform.tfstate)
#   TFSTATE_HOST_SCOPE_ID          claim scope the activation asks the worker to use
#                                  (required: state_snapshot:<backend>:<locator hash>
#                                  per scope/tfstate.go so Phase 3.5 finds the scope)
#   TFSTATE_HOST_SCOPE_KIND        (default state_snapshot)
#   TFSTATE_HOST_SOURCE_SYSTEM     (default terraform_state; must match the
#                                  satellite's fact source_system or the SDK
#                                  validator rejects every result)
#   AWS_S3_ENDPOINT                satellite S3 endpoint (http://minio:9000)
#   AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY (default minioadmin)
#   AWS_REGION / AWS_DEFAULT_REGION (default us-east-1)
set -eu

component_home="${ESHU_COMPONENT_HOME:?ESHU_COMPONENT_HOME must be set}"
instance="${INSTANCE_ID:?INSTANCE_ID must be set}"
buckets="${TFSTATE_SEED_BUCKETS:?TFSTATE_SEED_BUCKETS must be set}"
scope_prefix="${TFSTATE_SCOPE_PREFIX:?TFSTATE_SCOPE_PREFIX must be set}"
state_key="${TFSTATE_STATE_KEY:-prod/terraform.tfstate}"
host_scope_id="${TFSTATE_HOST_SCOPE_ID:?TFSTATE_HOST_SCOPE_ID must be set}"
host_scope_kind="${TFSTATE_HOST_SCOPE_KIND:-state_snapshot}"
host_source_system="${TFSTATE_HOST_SOURCE_SYSTEM:-terraform_state}"
s3_endpoint="${AWS_S3_ENDPOINT:?AWS_S3_ENDPOINT must be set}"
aws_key="${AWS_ACCESS_KEY_ID:-minioadmin}"
aws_secret="${AWS_SECRET_ACCESS_KEY:-minioadmin}"
aws_region="${AWS_REGION:-us-east-1}"
component_id="dev.eshu.collector.terraform-state"
component_version="0.1.0"
component_publisher="eshu-hq"

seed_dir="${component_home}/runtime/${instance}"
seed_binary="${seed_dir}/collector"
seed_manifest="${seed_dir}/manifest.yaml"
activation_path="${seed_dir}/activation.yaml"

log() { printf '\n=== tfstate-reconcile[%s]: %s ===\n' "${instance}" "$*"; }

[ -x "${seed_binary}" ] || {
	echo "tfstate-reconcile failed: seeded collector ${seed_binary} is missing or not executable" >&2
	exit 1
}
[ -f "${seed_manifest}" ] || {
	echo "tfstate-reconcile failed: seeded manifest ${seed_manifest} is missing" >&2
	exit 1
}
# The seed container runs as root into a shared volume while the worker
# runs as the product image's eshu user, so the package must be
# world-readable before install and before the worker starts.
chmod -R a+rX "${seed_dir}"
# Fail closed when the seed did not carry the release this lane pins.
grep -qF "id: ${component_id}" "${seed_manifest}" || {
	echo "tfstate-reconcile failed: ${seed_manifest} does not carry component id ${component_id}" >&2
	exit 1
}
grep -qF "version: ${component_version}" "${seed_manifest}" || {
	echo "tfstate-reconcile failed: ${seed_manifest} does not carry version ${component_version}" >&2
	exit 1
}

log "record producer grants for the manifest's emitted kinds"
for kind in terraform_state_snapshot terraform_state_resource terraform_state_output terraform_state_module terraform_state_provider_binding terraform_state_tag_observation terraform_state_warning incident_routing.applied_pagerduty_resource incident_routing.applied_alert_route; do
	/usr/local/bin/eshu component grant "${component_id}" \
		--component-home "${component_home}" \
		--version "${component_version}" \
		--kind "${kind}" \
		--schema-version 1.0.0 \
		--scope terraform_state \
		--expires-in 720h
done

log "install allowlisted manifest"
/usr/local/bin/eshu component install "${seed_manifest}" \
	--component-home "${component_home}" \
	--trust-mode allowlist \
	--allow-id "${component_id}" \
	--allow-publisher "${component_publisher}"

log "write activation config"
# One exact seed per bucket: the satellite opens base/bucket/key path-style
# against AWS_S3_ENDPOINT. The target_scope_ids are operator labels binding
# seeds to scopes. The host block fixes the claim identity the worker
# mints: scope and source system must reproduce the in-tree wire
# (state_snapshot:<backend>:<locator hash> per scope/tfstate.go,
# source_system terraform_state per the satellite's fact source_refs) or
# the SDK validator rejects every result and Phase 3.5 finds no scopes.
{
	printf 'host:\n'
	printf '  sourceSystem: %s\n' "${host_source_system}"
	printf '  scope:\n'
	printf '    id: %s\n' "${host_scope_id}"
	printf '    kind: %s\n' "${host_scope_kind}"
	printf 'config:\n'
	printf '  limits:\n'
	printf '    claimTimeoutSeconds: 60\n'
	printf '    maxPayloadBytes: 65536\n'
	printf '    maxRecordsPerClaim: 5000\n'
	printf '  discovery:\n'
	printf '    seeds:\n'
	for bucket in ${buckets}; do
		suffix="${bucket##eshu-drift-}"
		printf '      - kind: s3\n'
		printf '        target_scope_id: "%s-%s"\n' "${scope_prefix}" "${suffix}"
		printf '        bucket: "%s"\n' "${bucket}"
		printf '        key: "%s"\n' "${state_key}"
		printf '        region: "%s"\n' "${aws_region}"
	done
	printf '  target_scopes:\n'
	for bucket in ${buckets}; do
		suffix="${bucket##eshu-drift-}"
		printf '    - target_scope_id: "%s-%s"\n' "${scope_prefix}" "${suffix}"
		printf '      provider: aws\n'
		printf '      deployment_mode: account_local\n'
		printf '      allowed_backends: [s3]\n'
		printf '      allowed_regions: ["%s"]\n' "${aws_region}"
	done
	printf 'process:\n'
	printf '  command: %s\n' "${seed_binary}"
	printf '  args:\n'
	printf '    - --sdk-stdio\n'
	printf '  env:\n'
	printf '    - AWS_S3_ENDPOINT=%s\n' "${s3_endpoint}"
	printf '    - AWS_ACCESS_KEY_ID=%s\n' "${aws_key}"
	printf '    - AWS_SECRET_ACCESS_KEY=%s\n' "${aws_secret}"
	printf '    - AWS_REGION=%s\n' "${aws_region}"
	printf '    - AWS_DEFAULT_REGION=%s\n' "${aws_region}"
} >"${activation_path}"
chmod a+r "${activation_path}"

log "enable claims-capable instance ${instance}"
out="$("/usr/local/bin/eshu" component enable "${component_id}" \
	--component-home "${component_home}" \
	--instance "${instance}" \
	--mode scheduled \
	--claims \
	--config "${activation_path}" 2>&1)" || status=$?
status="${status:-0}"
printf '%s\n' "${out}"
if [ "${status}" -ne 0 ]; then
	case "${out}" in
		*"already enabled for component"*)
			echo "activation ${instance} already enabled; continuing"
			;;
		*) exit "${status}" ;;
	esac
fi

# The registry (registry.json, grants, installed packages) is written by
# this root step with owner-only modes; the worker reads it as the product
# image's eshu user, so the whole home must be world-readable before the
# worker starts.
chmod -R a+rX "${component_home}"

log "terraform-state component reconcile PASS"
