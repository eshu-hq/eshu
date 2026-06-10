#!/bin/sh
# Entrypoint for the Scorecard component-extension proof image (#2126/#1923).
# Installs and enables the reference Scorecard component into the shared
# component home under an allowlist trust policy, points the process adapter at
# the bundled scorecard-collector binary and fixture, then runs the
# component-extension collector worker. The image is source-evidence only: it
# commits dev.eshu.examples.scorecard.* facts and writes no graph truth.
set -eu

component_home="${ESHU_COMPONENT_HOME:?ESHU_COMPONENT_HOME is required}"
package_dir="/opt/scorecard"
manifest="${package_dir}/manifest.yaml"
instance_id="${ESHU_COMPONENT_COLLECTOR_INSTANCE_ID:-scorecard-remote}"

# A process-adapter activation config that resolves the bundled collector
# binary and the deterministic fixture by absolute path inside the image.
config_path="/tmp/scorecard-activation.yaml"
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

eshu component install "${manifest}" \
  --component-home "${component_home}" \
  --trust-mode allowlist \
  --allow-id dev.eshu.examples.scorecard \
  --allow-publisher eshu-hq

eshu component enable dev.eshu.examples.scorecard \
  --component-home "${component_home}" \
  --instance "${instance_id}" \
  --mode scheduled \
  --claims \
  --config "${config_path}"

exec eshu-collector-component-extension
