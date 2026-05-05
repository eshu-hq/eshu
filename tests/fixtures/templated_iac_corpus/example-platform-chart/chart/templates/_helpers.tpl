{{- define "eshu.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "eshu.labels" -}}
helm.sh/chart: {{ include "eshu.name" . }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "eshu.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "eshu.selectorLabels" -}}
app.kubernetes.io/name: {{ include "eshu.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "eshu.apiFullname" -}}
{{- printf "%s-api" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.ingesterHeadlessServiceName" -}}
{{- printf "%s-ingester" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.apiSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: api
{{- end -}}

{{- define "eshu.ingesterSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: ingester
{{- end -}}

{{- define "eshu.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "eshu.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "eshu.dataClaimName" -}}
{{- if .Values.ingester.persistence.existingClaim -}}
{{- .Values.ingester.persistence.existingClaim -}}
{{- else -}}
{{- printf "%s-data" (include "eshu.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "eshu.renderEnvMap" -}}
{{- range $key, $value := . }}
- name: {{ $key }}
  value: {{ $value | quote }}
{{- end }}
{{- end -}}

{{- define "eshu.renderEnvMaps" -}}
{{- range $envMap := . }}
{{- if $envMap }}
{{- include "eshu.renderEnvMap" $envMap }}
{{- end }}
{{- end }}
{{- end -}}

{{- define "eshu.joinStringMap" -}}
{{- $map := . -}}
{{- $items := list -}}
{{- range $key := keys $map | sortAlpha -}}
{{- $items = append $items (printf "%s=%v" $key (index $map $key)) -}}
{{- end -}}
{{- join "," $items -}}
{{- end -}}

{{- define "eshu.renderOtelEnv" -}}
{{- if .Values.observability.otel.enabled }}
- name: ESHU_DEPLOYMENT_ENVIRONMENT
  value: {{ .Values.observability.environment | quote }}
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: {{ .Values.observability.otel.endpoint | quote }}
- name: OTEL_EXPORTER_OTLP_PROTOCOL
  value: {{ .Values.observability.otel.protocol | quote }}
- name: OTEL_EXPORTER_OTLP_INSECURE
  value: {{ ternary "true" "false" .Values.observability.otel.insecure | quote }}
- name: OTEL_EXPORTER_OTLP_HEADERS
  value: {{ include "eshu.joinStringMap" .Values.observability.otel.headers | quote }}
- name: OTEL_TRACES_EXPORTER
  value: "otlp"
- name: OTEL_METRICS_EXPORTER
  value: "otlp"
- name: OTEL_LOGS_EXPORTER
  value: "none"
- name: OTEL_METRIC_EXPORT_INTERVAL
  value: {{ mul (int .Values.observability.otel.metricExportIntervalSeconds) 1000 | quote }}
- name: OTEL_PYTHON_FASTAPI_EXCLUDED_URLS
  value: {{ join "," .Values.observability.otel.excludedUrls | quote }}
- name: OTEL_RESOURCE_ATTRIBUTES
  value: {{ include "eshu.joinStringMap" .Values.observability.otel.resourceAttributes | quote }}
{{- end }}
{{- end -}}

{{- define "eshu.renderContentStoreEnv" -}}
{{- if and .Values.contentStore.secretName .Values.contentStore.dsnKey }}
- name: ESHU_CONTENT_STORE_DSN
  valueFrom:
    secretKeyRef:
      name: {{ .Values.contentStore.secretName }}
      key: {{ .Values.contentStore.dsnKey }}
- name: ESHU_POSTGRES_DSN
  valueFrom:
    secretKeyRef:
      name: {{ .Values.contentStore.secretName }}
      key: {{ .Values.contentStore.dsnKey }}
{{- else if .Values.contentStore.dsn }}
- name: ESHU_CONTENT_STORE_DSN
  value: {{ .Values.contentStore.dsn | quote }}
- name: ESHU_POSTGRES_DSN
  value: {{ .Values.contentStore.dsn | quote }}
{{- end }}
{{- end -}}

{{- define "eshu.argocdAnnotations" -}}
argocd.argoproj.io/sync-wave: {{ default "1" .Values.argocd.syncWave | quote }}
{{- end -}}
