#!/bin/sh
# Install and enable the reference PagerDuty component into the shared
# component home for the Compose proof. The workflow coordinator and
# component-extension collector both read this registry state.
set -eu

component_home="${ESHU_COMPONENT_HOME:?ESHU_COMPONENT_HOME is required}"
package_dir="/opt/pagerduty"
manifest="${package_dir}/manifest.yaml"
instance_id="${ESHU_COMPONENT_COLLECTOR_INSTANCE_ID:-pagerduty-reference}"
config_path="${component_home}/pagerduty-activation.yaml"

mkdir -p "${component_home}"
cat >"${config_path}" <<CFG
host:
  sourceSystem: pagerduty
  scope:
    id: pagerduty:account:synthetic-reference
    kind: pagerduty_account
process:
  command: /usr/local/bin/pagerduty-reference
  args:
    - --sdk-stdio
config:
  source:
    mode: local-file
    input: ${package_dir}/testdata/complete.json
CFG

if eshu component list --component-home "${component_home}" --json 2>/dev/null \
	| rg --quiet '"id": "dev.eshu.examples.pagerduty"'; then
	echo "pagerduty component already installed; skipping install"
else
	eshu component install "${manifest}" \
		--component-home "${component_home}" \
		--trust-mode allowlist \
		--allow-id dev.eshu.examples.pagerduty \
		--allow-publisher eshu-hq
fi

enable_out="$(eshu component enable dev.eshu.examples.pagerduty \
	--component-home "${component_home}" \
	--instance "${instance_id}" \
	--mode scheduled \
	--claims \
	--config "${config_path}" 2>&1)" || true
printf '%s\n' "${enable_out}"
case "${enable_out}" in
	*"already enabled"*)
		echo "pagerduty activation ${instance_id} already enabled; continuing"
		;;
	*"enabled dev.eshu.examples.pagerduty"*)
		;;
	*)
		echo "unexpected enable result; failing" >&2
		exit 1
		;;
esac

if [ "$(id -u)" -eq 0 ] && id eshu >/dev/null 2>&1; then
	chown -R eshu:eshu "${component_home}"
fi
chmod -R a+rX "${component_home}"
echo "pagerduty component installed and enabled (instance=${instance_id})"
