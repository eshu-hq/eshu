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
