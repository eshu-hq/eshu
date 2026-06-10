#!/bin/sh
# Install + enable the reference Scorecard component into the shared component
# home for the remote Compose proof (#2126/#1923), then exit. Runs as a
# one-shot init the workflow coordinator and component-extension collector both
# depend on, so the coordinator starts in active mode with an enabled
# claim-capable instance already present.
#
# The activation config is written under the shared component home so the
# coordinator (base eshu image) can read the host metadata; the process adapter
# command and fixture resolve inside the collector proof image.
set -eu

component_home="${ESHU_COMPONENT_HOME:?ESHU_COMPONENT_HOME is required}"
package_dir="/opt/scorecard"
manifest="${package_dir}/manifest.yaml"
instance_id="${ESHU_COMPONENT_COLLECTOR_INSTANCE_ID:-scorecard-remote}"
config_path="${component_home}/scorecard-activation.yaml"

mkdir -p "${component_home}"
cat >"${config_path}" <<CFG
host:
  sourceSystem: openssf-scorecard
  scope:
    id: github.com/example/widgets
    kind: repository
process:
  command: /usr/local/bin/scorecard-collector
  args:
    - --sdk-stdio
config:
  source:
    mode: local-file
    input: ${package_dir}/testdata/complete.json
CFG

# Install is not idempotent; skip if the component is already installed so a
# re-run of the init does not fail the proof.
if eshu component list --component-home "${component_home}" --json 2>/dev/null \
	| grep -q '"id": "dev.eshu.examples.scorecard"'; then
	echo "scorecard component already installed; skipping install"
else
	eshu component install "${manifest}" \
		--component-home "${component_home}" \
		--trust-mode allowlist \
		--allow-id dev.eshu.examples.scorecard \
		--allow-publisher eshu-hq
fi

# Enable is not idempotent either: it errors if the activation already exists.
# Tolerate that so a re-run of the init (e.g. when only the coordinator is
# recreated) does not fail the proof.
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

# The coordinator and collector (uid 10001) only read the component home; make
# the registry state world-readable since this init wrote it as root.
chmod -R a+rX "${component_home}"

echo "scorecard component installed and enabled (instance=${instance_id})"
