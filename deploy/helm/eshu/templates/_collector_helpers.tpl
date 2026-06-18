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

{{- define "eshu.renderGCPCloudCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "gcpCloudCollector.collectorInstances must contain at least one instance when gcpCloudCollector.enabled=true" .Values.gcpCloudCollector.collectorInstances | toJson | quote }}
- name: ESHU_GCP_COLLECTOR_INSTANCE_ID
  value: {{ .Values.gcpCloudCollector.instanceId | quote }}
- name: ESHU_GCP_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_GCP_COLLECTOR_POLL_INTERVAL
  value: {{ .Values.gcpCloudCollector.pollInterval | quote }}
{{- with .Values.gcpCloudCollector.claimLeaseTTL }}
- name: ESHU_GCP_COLLECTOR_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.gcpCloudCollector.heartbeatInterval }}
- name: ESHU_GCP_COLLECTOR_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.gcpCloudCollector.extraEnv }}
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

{{- define "eshu.renderSBOMAttestationCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "sbomAttestationCollector.collectorInstances must contain at least one instance when sbomAttestationCollector.enabled=true" .Values.sbomAttestationCollector.collectorInstances | toJson | quote }}
- name: ESHU_SBOM_ATTESTATION_COLLECTOR_INSTANCE_ID
  value: {{ .Values.sbomAttestationCollector.instanceId | quote }}
- name: ESHU_SBOM_ATTESTATION_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_SBOM_ATTESTATION_POLL_INTERVAL
  value: {{ .Values.sbomAttestationCollector.pollInterval | quote }}
{{- with .Values.sbomAttestationCollector.claimLeaseTTL }}
- name: ESHU_SBOM_ATTESTATION_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.sbomAttestationCollector.heartbeatInterval }}
- name: ESHU_SBOM_ATTESTATION_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.sbomAttestationCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "eshu.renderSecurityAlertCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "securityAlertCollector.collectorInstances must contain at least one instance when securityAlertCollector.enabled=true" .Values.securityAlertCollector.collectorInstances | toJson | quote }}
- name: ESHU_SECURITY_ALERT_COLLECTOR_INSTANCE_ID
  value: {{ .Values.securityAlertCollector.instanceId | quote }}
- name: ESHU_SECURITY_ALERT_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_SECURITY_ALERT_POLL_INTERVAL
  value: {{ .Values.securityAlertCollector.pollInterval | quote }}
{{- with .Values.securityAlertCollector.claimLeaseTTL }}
- name: ESHU_SECURITY_ALERT_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.securityAlertCollector.heartbeatInterval }}
- name: ESHU_SECURITY_ALERT_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.securityAlertCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "eshu.renderCICDRunCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "cicdRunCollector.collectorInstances must contain at least one instance when cicdRunCollector.enabled=true" .Values.cicdRunCollector.collectorInstances | toJson | quote }}
- name: ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID
  value: {{ .Values.cicdRunCollector.instanceId | quote }}
- name: ESHU_CICD_RUN_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_CICD_RUN_POLL_INTERVAL
  value: {{ .Values.cicdRunCollector.pollInterval | quote }}
{{- with .Values.cicdRunCollector.claimLeaseTTL }}
- name: ESHU_CICD_RUN_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.cicdRunCollector.heartbeatInterval }}
- name: ESHU_CICD_RUN_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.cicdRunCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "eshu.renderPagerDutyCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "pagerDutyCollector.collectorInstances must contain at least one instance when pagerDutyCollector.enabled=true" .Values.pagerDutyCollector.collectorInstances | toJson | quote }}
- name: ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID
  value: {{ .Values.pagerDutyCollector.instanceId | quote }}
- name: ESHU_PAGERDUTY_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_PAGERDUTY_POLL_INTERVAL
  value: {{ .Values.pagerDutyCollector.pollInterval | quote }}
{{- with .Values.pagerDutyCollector.claimLeaseTTL }}
- name: ESHU_PAGERDUTY_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.pagerDutyCollector.heartbeatInterval }}
- name: ESHU_PAGERDUTY_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.pagerDutyCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "eshu.renderGrafanaCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "grafanaCollector.collectorInstances must contain at least one instance when grafanaCollector.enabled=true" .Values.grafanaCollector.collectorInstances | toJson | quote }}
- name: ESHU_GRAFANA_COLLECTOR_INSTANCE_ID
  value: {{ .Values.grafanaCollector.instanceId | quote }}
- name: ESHU_GRAFANA_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_GRAFANA_COLLECTOR_POLL_INTERVAL
  value: {{ .Values.grafanaCollector.pollInterval | quote }}
{{- with .Values.grafanaCollector.claimLeaseTTL }}
- name: ESHU_GRAFANA_COLLECTOR_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.grafanaCollector.heartbeatInterval }}
- name: ESHU_GRAFANA_COLLECTOR_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.grafanaCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "eshu.renderPrometheusMimirCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "prometheusMimirCollector.collectorInstances must contain at least one instance when prometheusMimirCollector.enabled=true" .Values.prometheusMimirCollector.collectorInstances | toJson | quote }}
- name: ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID
  value: {{ .Values.prometheusMimirCollector.instanceId | quote }}
- name: ESHU_PROMETHEUS_MIMIR_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_PROMETHEUS_MIMIR_COLLECTOR_POLL_INTERVAL
  value: {{ .Values.prometheusMimirCollector.pollInterval | quote }}
{{- with .Values.prometheusMimirCollector.claimLeaseTTL }}
- name: ESHU_PROMETHEUS_MIMIR_COLLECTOR_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.prometheusMimirCollector.heartbeatInterval }}
- name: ESHU_PROMETHEUS_MIMIR_COLLECTOR_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.prometheusMimirCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "eshu.renderPrometheusMimirTimeSeriesEnv" -}}
{{- if .Values.prometheusMimirCollector.enabled }}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "prometheusMimirCollector.collectorInstances must contain at least one instance when prometheusMimirCollector.enabled=true" .Values.prometheusMimirCollector.collectorInstances | toJson | quote }}
- name: ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID
  value: {{ .Values.prometheusMimirCollector.instanceId | quote }}
{{- with .Values.prometheusMimirCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}
{{- end -}}

{{- define "eshu.renderLokiCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "lokiCollector.collectorInstances must contain at least one instance when lokiCollector.enabled=true" .Values.lokiCollector.collectorInstances | toJson | quote }}
- name: ESHU_LOKI_COLLECTOR_INSTANCE_ID
  value: {{ .Values.lokiCollector.instanceId | quote }}
- name: ESHU_LOKI_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_LOKI_COLLECTOR_POLL_INTERVAL
  value: {{ .Values.lokiCollector.pollInterval | quote }}
{{- with .Values.lokiCollector.claimLeaseTTL }}
- name: ESHU_LOKI_COLLECTOR_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.lokiCollector.heartbeatInterval }}
- name: ESHU_LOKI_COLLECTOR_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.lokiCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "eshu.renderTempoCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "tempoCollector.collectorInstances must contain at least one instance when tempoCollector.enabled=true" .Values.tempoCollector.collectorInstances | toJson | quote }}
- name: ESHU_TEMPO_COLLECTOR_INSTANCE_ID
  value: {{ .Values.tempoCollector.instanceId | quote }}
- name: ESHU_TEMPO_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_TEMPO_COLLECTOR_POLL_INTERVAL
  value: {{ .Values.tempoCollector.pollInterval | quote }}
{{- with .Values.tempoCollector.claimLeaseTTL }}
- name: ESHU_TEMPO_COLLECTOR_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.tempoCollector.heartbeatInterval }}
- name: ESHU_TEMPO_COLLECTOR_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.tempoCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "eshu.renderJiraCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "jiraCollector.collectorInstances must contain at least one instance when jiraCollector.enabled=true" .Values.jiraCollector.collectorInstances | toJson | quote }}
- name: ESHU_JIRA_COLLECTOR_INSTANCE_ID
  value: {{ .Values.jiraCollector.instanceId | quote }}
- name: ESHU_JIRA_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_JIRA_POLL_INTERVAL
  value: {{ .Values.jiraCollector.pollInterval | quote }}
{{- with .Values.jiraCollector.claimLeaseTTL }}
- name: ESHU_JIRA_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.jiraCollector.heartbeatInterval }}
- name: ESHU_JIRA_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.jiraCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "eshu.renderVulnerabilityIntelligenceCollectorEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "vulnerabilityIntelligenceCollector.collectorInstances must contain at least one instance when vulnerabilityIntelligenceCollector.enabled=true" .Values.vulnerabilityIntelligenceCollector.collectorInstances | toJson | quote }}
- name: ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_INSTANCE_ID
  value: {{ .Values.vulnerabilityIntelligenceCollector.instanceId | quote }}
- name: ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_VULNERABILITY_INTELLIGENCE_POLL_INTERVAL
  value: {{ .Values.vulnerabilityIntelligenceCollector.pollInterval | quote }}
{{- with .Values.vulnerabilityIntelligenceCollector.claimLeaseTTL }}
- name: ESHU_VULNERABILITY_INTELLIGENCE_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.vulnerabilityIntelligenceCollector.heartbeatInterval }}
- name: ESHU_VULNERABILITY_INTELLIGENCE_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.vulnerabilityIntelligenceCollector.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "eshu.renderScannerWorkerEnv" -}}
- name: ESHU_COLLECTOR_INSTANCES_JSON
  value: {{ required "scannerWorker.collectorInstances must contain at least one instance when scannerWorker.enabled=true" .Values.scannerWorker.collectorInstances | toJson | quote }}
- name: ESHU_SCANNER_WORKER_INSTANCE_ID
  value: {{ .Values.scannerWorker.instanceId | quote }}
- name: ESHU_SCANNER_WORKER_OWNER_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: ESHU_SCANNER_WORKER_ANALYZER
  value: {{ .Values.scannerWorker.analyzer | quote }}
- name: ESHU_SCANNER_WORKER_POLL_INTERVAL
  value: {{ .Values.scannerWorker.pollInterval | quote }}
{{- with .Values.scannerWorker.claimLeaseTTL }}
- name: ESHU_SCANNER_WORKER_CLAIM_LEASE_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.scannerWorker.heartbeatInterval }}
- name: ESHU_SCANNER_WORKER_HEARTBEAT_INTERVAL
  value: {{ . | quote }}
{{- end }}
- name: ESHU_SCANNER_WORKER_CPU_MILLIS
  value: {{ .Values.scannerWorker.cpuMillis | quote }}
- name: ESHU_SCANNER_WORKER_MEMORY_BYTES
  value: {{ .Values.scannerWorker.memoryBytes | quote }}
- name: ESHU_SCANNER_WORKER_TIMEOUT
  value: {{ .Values.scannerWorker.timeout | quote }}
- name: ESHU_SCANNER_WORKER_MAX_INPUT_BYTES
  value: {{ .Values.scannerWorker.maxInputBytes | quote }}
- name: ESHU_SCANNER_WORKER_MAX_FILES
  value: {{ .Values.scannerWorker.maxFiles | quote }}
- name: ESHU_SCANNER_WORKER_MAX_FACTS
  value: {{ .Values.scannerWorker.maxFacts | quote }}
{{- with .Values.scannerWorker.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}
