#!/bin/sh
# Install + enable the reference Scorecard component under the OCI adapter for
# the remote Compose proof (#1980/#1923), then exit. Runs as a one-shot init the
# workflow coordinator and component-extension collector both depend on.
#
# The committed OCI manifest carries a placeholder digest. This init rewrites the
# single artifact image to the real digest-pinned local-registry ref supplied by
# the harness (ESHU_SCORECARD_OCI_IMAGE) before install, so the worker only ever
# launches the exact artifact that was built and pushed. The real ref is written
# into the shared component home, never into the repository.
set -eu

component_home="${ESHU_COMPONENT_HOME:?ESHU_COMPONENT_HOME is required}"
image_ref="${ESHU_SCORECARD_OCI_IMAGE:?ESHU_SCORECARD_OCI_IMAGE is required (digest-pinned)}"
package_dir="/opt/scorecard"
src_manifest="${package_dir}/manifest.oci.yaml"
instance_id="${ESHU_COMPONENT_COLLECTOR_INSTANCE_ID:-scorecard-remote-oci}"
config_path="${component_home}/scorecard-activation-oci.yaml"
manifest_path="${component_home}/scorecard-manifest-oci.yaml"

case "${image_ref}" in
	*@sha256:*) ;;
	*) echo "ESHU_SCORECARD_OCI_IMAGE must be digest-pinned (repo@sha256:<64 hex>): ${image_ref}" >&2; exit 1 ;;
esac

mkdir -p "${component_home}"

# Rewrite the single artifact image line to the real digest-pinned ref.
sed "s#image: .*#image: ${image_ref}#" "${src_manifest}" >"${manifest_path}"

# Activation config: host metadata, explicit OCI isolation knobs (these mirror
# the adapter defaults), and the source fixture path baked into the artifact
# image. The image is NOT taken from this file; it comes from the verified
# manifest artifact.
cat >"${config_path}" <<CFG
host:
  sourceSystem: openssf-scorecard
  scope:
    id: github.com/example/widgets
    kind: repository
oci:
  network: none
  user: "65532:65532"
config:
  source:
    input: /var/lib/scorecard/complete.json
CFG

if eshu component list --component-home "${component_home}" --json 2>/dev/null \
	| grep -q '"id": "dev.eshu.examples.scorecard"'; then
	echo "scorecard component already installed; skipping install"
else
	eshu component install "${manifest_path}" \
		--component-home "${component_home}" \
		--trust-mode allowlist \
		--allow-id dev.eshu.examples.scorecard \
		--allow-publisher eshu-hq
fi

enable_out="$(eshu component enable dev.eshu.examples.scorecard \
	--component-home "${component_home}" \
	--instance "${instance_id}" \
	--mode scheduled \
	--claims \
	--config "${config_path}" 2>&1)" || true
printf '%s\n' "${enable_out}"
case "${enable_out}" in
	*"already enabled"*)
		echo "scorecard activation ${instance_id} already enabled; continuing"
		;;
	*"enabled dev.eshu.examples.scorecard"*)
		;;
	*)
		echo "unexpected enable result; failing" >&2
		exit 1
		;;
esac

chmod -R a+rX "${component_home}"
echo "scorecard OCI component installed and enabled (instance=${instance_id})"
