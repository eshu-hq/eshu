{{/*
Helpers for componentExtensionCollector.activations.

One activation renders one worker Deployment with its own instance ID and its
own component registry (an emptyDir seeded at pod start). The layout inside the
registry volume and the activation config path are shared by the worker and the
workflow coordinator, because ActivationConfigHandle hashes the config path and
both processes must derive the same handle.
*/}}

{{/*
"true" when componentExtensionCollector is enabled and the activations list
drives rendering, empty string otherwise.
*/}}
{{- define "eshu.componentActivationsMode" -}}
{{- if and .Values.componentExtensionCollector.enabled (gt (len (default (list) .Values.componentExtensionCollector.activations)) 0) -}}true{{- end -}}
{{- end -}}

{{- define "eshu.componentActivationFullname" -}}
{{- printf "%s-cx-%s" (include "eshu.fullname" .root) .activation.instanceId -}}
{{- end -}}

{{- define "eshu.componentActivationSelectorLabels" -}}
{{- include "eshu.componentExtensionCollectorSelectorLabels" .root }}
eshu.dev/component-instance: {{ .activation.instanceId | quote }}
{{- end -}}

{{- define "eshu.componentActivationConfigMapName" -}}
{{- printf "%s-component-activations" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Directory in the pod where the activation ConfigMap is mounted. */}}
{{- define "eshu.componentActivationConfigDir" -}}
/etc/eshu/component-activations
{{- end -}}

{{- define "eshu.componentActivationConfigPath" -}}
{{- printf "%s/%s.yaml" (include "eshu.componentActivationConfigDir" .root) .activation.instanceId -}}
{{- end -}}

{{/* Directory under componentHome that the seed init container fills. */}}
{{- define "eshu.componentActivationRuntimeDir" -}}
{{- printf "%s/runtime/%s" (trimSuffix "/" .root.Values.componentExtensionCollector.componentHome) .activation.instanceId -}}
{{- end -}}

{{- define "eshu.componentActivationProcessCommand" -}}
{{- printf "%s/collector" (include "eshu.componentActivationRuntimeDir" .) -}}
{{- end -}}

{{/*
Activation runtime config the worker and the coordinator read (componentRuntimeFile
in go/cmd/collector-component-extension/config.go). It holds only non-secret
claim metadata and collector config; credentials arrive as environment variables.
*/}}
{{- define "eshu.componentActivationConfigYAML" -}}
{{- $activation := .activation -}}
{{- $process := default (dict) $activation.process -}}
{{- $runner := dict "command" (include "eshu.componentActivationProcessCommand" .) -}}
{{- with $process.args }}{{ $_ := set $runner "args" . }}{{ end -}}
{{- with $process.env }}{{ $_ := set $runner "env" . }}{{ end -}}
{{- with $process.stdoutLimitBytes }}{{ $_ := set $runner "stdout_limit_bytes" . }}{{ end -}}
{{- with $process.stderrLimitBytes }}{{ $_ := set $runner "stderr_limit_bytes" . }}{{ end -}}
{{- $host := dict "sourceSystem" $activation.host.sourceSystem "scope" (dict "id" $activation.host.scope.id "kind" $activation.host.scope.kind) -}}
{{- $file := dict "host" $host "process" $runner -}}
{{- with $activation.config }}{{ $_ := set $file "config" . }}{{ end -}}
{{- toYaml $file -}}
{{- end -}}

{{/* ConfigMap data: one <instanceId>.yaml key per activation. */}}
{{- define "eshu.componentActivationsConfigData" -}}
{{- range $activation := .Values.componentExtensionCollector.activations }}
{{ $activation.instanceId }}.yaml: |
  {{- include "eshu.componentActivationConfigYAML" (dict "root" $ "activation" $activation) | nindent 2 }}
{{- end }}
{{- end -}}

{{- define "eshu.componentActivationsChecksum" -}}
{{- include "eshu.componentActivationsConfigData" . | sha256sum -}}
{{- end -}}

{{/*
Per-activation environment for the worker container. Credentials come only
from the activation's own extraEnv, so one collector never inherits another's
secret.
*/}}
{{- define "eshu.renderComponentActivationEnv" -}}
{{- $root := .root -}}
{{- $activation := .activation -}}
{{- $values := $root.Values.componentExtensionCollector -}}
{{- include "eshu.renderComponentExtensionPolicyEnv" $root }}
- name: ESHU_COMPONENT_COLLECTOR_INSTANCE_ID
  value: {{ $activation.instanceId | quote }}
- name: ESHU_COMPONENT_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_COMPONENT_COLLECTOR_SCOPE_KIND
  value: {{ $activation.host.scope.kind | quote }}
- name: ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL
  value: {{ default $values.pollInterval $activation.pollInterval | quote }}
{{- with default $values.claimLeaseTTL $activation.claimLeaseTTL }}
- name: ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with default $values.heartbeatInterval $activation.heartbeatInterval }}
- name: ESHU_COMPONENT_COLLECTOR_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with $values.env }}
{{- include "eshu.renderEnvMap" . }}
{{- end }}
{{- with $activation.env }}
{{- include "eshu.renderEnvMap" . }}
{{- end }}
{{- with $activation.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/* Pod volumes for the pod-local registry and the activation config. */}}
{{- define "eshu.componentActivationVolumes" -}}
- name: component-home
  emptyDir: {}
- name: component-activations
  configMap:
    name: {{ include "eshu.componentActivationConfigMapName" . }}
{{- end -}}

{{/* Main-container mounts. readOnlyHome mounts the registry read-only (coordinator). */}}
{{- define "eshu.componentActivationVolumeMounts" -}}
- name: component-home
  mountPath: {{ .root.Values.componentExtensionCollector.componentHome | quote }}
  {{- if .readOnlyHome }}
  readOnly: true
  {{- end }}
- name: component-activations
  mountPath: {{ include "eshu.componentActivationConfigDir" .root | quote }}
  readOnly: true
{{- end -}}

{{/*
Shell script for the reconcile init container. Values arrive as environment
variables; only grant arguments are written into the text, and validation
restricts them to characters that need no escaping inside single quotes.
Order matters: grants first, because a core-owned fact kind passes install
admission only when its grant is already recorded.
*/}}
{{- define "eshu.componentActivationReconcileScript" -}}
set -eu
runtime="${RUNTIME_DIR}"
if [ ! -x "${runtime}/collector" ] || [ ! -s "${runtime}/manifest.yaml" ]; then
  echo "seed image did not write ${runtime}/collector and ${runtime}/manifest.yaml" >&2
  exit 1
fi
{{- range $grant := default (list) .activation.grants }}
eshu component grant "${COMPONENT_ID}" --component-home "${ESHU_COMPONENT_HOME}" --version "${COMPONENT_VERSION}" --kind {{ $grant.kind | squote }}{{ range $version := $grant.schemaVersions }} --schema-version {{ $version | squote }}{{ end }} --scope {{ $grant.scope | squote }} --expires-in {{ default "720h" $grant.expiresIn | squote }}
{{- end }}
eshu component install "${runtime}/manifest.yaml" --component-home "${ESHU_COMPONENT_HOME}" --trust-mode allowlist --allow-id "${ALLOW_IDS}" --allow-publisher "${ALLOW_PUBLISHERS}" ${REVOKE_IDS:+--revoke-id "${REVOKE_IDS}"} ${REVOKE_PUBLISHERS:+--revoke-publisher "${REVOKE_PUBLISHERS}"}
raw="$(eshu component list --component-home "${ESHU_COMPONENT_HOME}" --json)"
listed="$(printf '%s' "${raw}" | tr -d ' \n')"
# The CLI prints one object per component, each opening with {"id":" and holding
# its own "version" later (name and publisher sit between the two). Walk the
# objects one at a time, so a component at the wrong version cannot pass on the
# version of another component in the same registry.
marker='{"id":"'
rest=${listed}
found=""
while :; do
  case "${rest}" in
    *"${marker}"*) ;;
    *) break ;;
  esac
  rest=${rest#*"${marker}"}
  entry=${rest%%"${marker}"*}
  case "${entry}" in
    "${COMPONENT_ID}\",\""*"\"version\":\"${COMPONENT_VERSION}\""*) found=1 ;;
  esac
done
if [ -z "${found}" ]; then
  echo "seeded manifest is not ${COMPONENT_ID}@${COMPONENT_VERSION}; fix componentId/componentVersion or the seed image" >&2
  exit 1
fi
status=0
out="$(eshu component enable "${COMPONENT_ID}" --component-home "${ESHU_COMPONENT_HOME}" --instance "${INSTANCE_ID}" --mode "${MODE}" --claims --config "${CONFIG_PATH}" 2>&1)" || status=$?
printf '%s\n' "${out}"
if [ "${status}" -ne 0 ]; then
  # Only the CLI's duplicate-activation message is a restart. Any other failure
  # keeps its exit code, so the pod does not start without an activation.
  case "${out}" in
    *"activation \"${INSTANCE_ID}\" is already enabled for component \"${COMPONENT_ID}\""*)
      echo "activation ${INSTANCE_ID} already enabled; continuing"
      ;;
    *) exit "${status}" ;;
  esac
fi
{{- end -}}

{{/*
Init containers for a list of activations: a seed container from the collector
image, then a reconcile container from the Eshu image, per activation.
*/}}
{{- define "eshu.componentActivationInitContainers" -}}
{{- $root := .root -}}
{{- range $activation := .activations }}
{{- $ctx := dict "root" $root "activation" $activation }}
{{- $seed := default (dict) $activation.seed }}
- name: seed-{{ $activation.instanceId }}
  image: {{ $seed.image | quote }}
  imagePullPolicy: {{ default "IfNotPresent" $seed.pullPolicy }}
  command:
    {{- range $arg := default (list "/collector" "seed") $seed.command }}
    - {{ $arg | quote }}
    {{- end }}
    - --dest
    - {{ include "eshu.componentActivationRuntimeDir" $ctx | quote }}
    - --image-ref
    - {{ $seed.image | quote }}
  securityContext:
    {{- toYaml $root.Values.initContainerSecurityContext | nindent 4 }}
  volumeMounts:
    - name: component-home
      mountPath: {{ $root.Values.componentExtensionCollector.componentHome | quote }}
- name: reconcile-{{ $activation.instanceId }}
  image: "{{ $root.Values.image.repository }}:{{ $root.Values.image.tag }}"
  imagePullPolicy: {{ $root.Values.image.pullPolicy }}
  command:
    - sh
    - -c
    - |
      {{- include "eshu.componentActivationReconcileScript" $ctx | nindent 6 }}
  env:
    - name: ESHU_HOME
      value: /tmp/.eshu
    - name: HOME
      value: /tmp
    - name: ESHU_COMPONENT_HOME
      value: {{ $root.Values.componentExtensionCollector.componentHome | quote }}
    - name: RUNTIME_DIR
      value: {{ include "eshu.componentActivationRuntimeDir" $ctx | quote }}
    - name: COMPONENT_ID
      value: {{ $activation.componentId | quote }}
    - name: COMPONENT_VERSION
      value: {{ $activation.componentVersion | quote }}
    - name: INSTANCE_ID
      value: {{ $activation.instanceId | quote }}
    - name: MODE
      value: {{ default "scheduled" $activation.mode | quote }}
    - name: CONFIG_PATH
      value: {{ include "eshu.componentActivationConfigPath" $ctx | quote }}
    - name: ALLOW_IDS
      value: {{ include "eshu.componentCSVList" $root.Values.componentExtensionCollector.allowIds | quote }}
    - name: ALLOW_PUBLISHERS
      value: {{ include "eshu.componentCSVList" $root.Values.componentExtensionCollector.allowPublishers | quote }}
    - name: REVOKE_IDS
      value: {{ include "eshu.componentCSVList" $root.Values.componentExtensionCollector.revokeIds | quote }}
    - name: REVOKE_PUBLISHERS
      value: {{ include "eshu.componentCSVList" $root.Values.componentExtensionCollector.revokePublishers | quote }}
    {{- with $root.Values.componentExtensionCollector.coreVersion }}
    {{- /* eshu component install checks compatibleCore against this; without it a
           main-branch image checks its commit SHA and refuses every install (#9090). */}}
    - name: ESHU_COMPONENT_CORE_VERSION
      value: {{ . | quote }}
    {{- end }}
  securityContext:
    {{- toYaml $root.Values.initContainerSecurityContext | nindent 4 }}
  volumeMounts:
    - name: tmp
      mountPath: /tmp
    - name: component-home
      mountPath: {{ $root.Values.componentExtensionCollector.componentHome | quote }}
    - name: component-activations
      mountPath: {{ include "eshu.componentActivationConfigDir" $root | quote }}
      readOnly: true
{{- end }}
{{- end -}}
