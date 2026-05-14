{{- define "eshu.renderTerraformStateCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "terraformStateCollector.collectorInstances must contain at least one instance when terraformStateCollector.enabled=true" .Values.terraformStateCollector.collectorInstances | toJson | quote }}
- name: ESHU_TFSTATE_COLLECTOR_INSTANCE_ID
  value: {{ .Values.terraformStateCollector.instanceId | quote }}
- name: ESHU_TFSTATE_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_TFSTATE_COLLECTOR_POLL_INTERVAL
  value: {{ .Values.terraformStateCollector.pollInterval | quote }}
{{- with .Values.terraformStateCollector.claimLeaseTTL }}
- name: ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.terraformStateCollector.heartbeatInterval }}
- name: ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
- name: ESHU_TFSTATE_REDACTION_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.terraformStateCollector.redaction.secretName }}
      key: {{ .Values.terraformStateCollector.redaction.keyKey }}
- name: ESHU_TFSTATE_REDACTION_RULESET_VERSION
  value: {{ .Values.terraformStateCollector.redaction.rulesetVersion | quote }}
{{- with .Values.terraformStateCollector.redaction.sensitiveKeys }}
- name: ESHU_TFSTATE_REDACTION_SENSITIVE_KEYS
  value: {{ . | quote }}
{{- end }}
{{- with .Values.terraformStateCollector.sourceMaxBytes }}
- name: ESHU_TFSTATE_SOURCE_MAX_BYTES
  value: {{ . | quote }}
{{- end }}
{{- with .Values.terraformStateCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "eshu.renderAWSCloudCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "awsCloudCollector.collectorInstances must contain at least one instance when awsCloudCollector.enabled=true" .Values.awsCloudCollector.collectorInstances | toJson | quote }}
- name: ESHU_AWS_COLLECTOR_INSTANCE_ID
  value: {{ .Values.awsCloudCollector.instanceId | quote }}
- name: ESHU_AWS_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_AWS_COLLECTOR_POLL_INTERVAL
  value: {{ .Values.awsCloudCollector.pollInterval | quote }}
{{- with .Values.awsCloudCollector.claimLeaseTTL }}
- name: ESHU_AWS_COLLECTOR_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.awsCloudCollector.heartbeatInterval }}
- name: ESHU_AWS_COLLECTOR_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- if .Values.awsCloudCollector.redaction.secretName }}
- name: ESHU_AWS_REDACTION_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.awsCloudCollector.redaction.secretName }}
      key: {{ .Values.awsCloudCollector.redaction.keyKey }}
{{- end }}
{{- with .Values.awsCloudCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "eshu.renderPackageRegistryCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "packageRegistryCollector.collectorInstances must contain at least one instance when packageRegistryCollector.enabled=true" .Values.packageRegistryCollector.collectorInstances | toJson | quote }}
- name: ESHU_PACKAGE_REGISTRY_COLLECTOR_INSTANCE_ID
  value: {{ .Values.packageRegistryCollector.instanceId | quote }}
- name: ESHU_PACKAGE_REGISTRY_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_PACKAGE_REGISTRY_POLL_INTERVAL
  value: {{ .Values.packageRegistryCollector.pollInterval | quote }}
{{- with .Values.packageRegistryCollector.claimLeaseTTL }}
- name: ESHU_PACKAGE_REGISTRY_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.packageRegistryCollector.heartbeatInterval }}
- name: ESHU_PACKAGE_REGISTRY_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.packageRegistryCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}
