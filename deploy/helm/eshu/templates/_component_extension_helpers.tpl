{{- define "eshu.componentExtensionCollectorFullname" -}}
{{- printf "%s-component-extension-collector" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.componentExtensionCollectorMetricsServiceName" -}}
{{- printf "%s-component-extension-collector-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.componentExtensionCollectorSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: component-extension-collector
{{- end -}}

{{- define "eshu.renderComponentExtensionPolicyEnv" -}}
- name: ESHU_COMPONENT_HOME
  value: {{ .Values.componentExtensionCollector.componentHome | quote }}
{{- with .Values.componentExtensionCollector.trustMode }}
- name: ESHU_COMPONENT_TRUST_MODE
  value: {{ . | quote }}
{{- end }}
{{- with .Values.componentExtensionCollector.allowIds }}
- name: ESHU_COMPONENT_ALLOW_IDS
  value: {{ . | quote }}
{{- end }}
{{- with .Values.componentExtensionCollector.allowPublishers }}
- name: ESHU_COMPONENT_ALLOW_PUBLISHERS
  value: {{ . | quote }}
{{- end }}
{{- with .Values.componentExtensionCollector.revokeIds }}
- name: ESHU_COMPONENT_REVOKE_IDS
  value: {{ . | quote }}
{{- end }}
{{- with .Values.componentExtensionCollector.revokePublishers }}
- name: ESHU_COMPONENT_REVOKE_PUBLISHERS
  value: {{ . | quote }}
{{- end }}
{{- with .Values.componentExtensionCollector.coreVersion }}
- name: ESHU_COMPONENT_CORE_VERSION
  value: {{ . | quote }}
{{- end }}
{{- with .Values.componentExtensionCollector.extensionEgressPolicyJSON }}
- name: ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON
  value: {{ . | quote }}
{{- end }}
{{- end -}}

{{- define "eshu.renderComponentExtensionCollectorEnv" -}}
{{- include "eshu.renderComponentExtensionPolicyEnv" . }}
{{- with .Values.componentExtensionCollector.instanceId }}
- name: ESHU_COMPONENT_COLLECTOR_INSTANCE_ID
  value: {{ . | quote }}
{{- end }}
- name: ESHU_COMPONENT_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_COMPONENT_COLLECTOR_SCOPE_KIND
  value: {{ .Values.componentExtensionCollector.scopeKind | quote }}
- name: ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL
  value: {{ .Values.componentExtensionCollector.pollInterval | quote }}
{{- with .Values.componentExtensionCollector.claimLeaseTTL }}
- name: ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.componentExtensionCollector.heartbeatInterval }}
- name: ESHU_COMPONENT_COLLECTOR_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.componentExtensionCollector.env }}
{{- include "eshu.renderEnvMap" . }}
{{- end }}
{{- with .Values.componentExtensionCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}
