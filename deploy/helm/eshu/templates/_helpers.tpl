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

{{- define "eshu.mcpServerFullname" -}}
{{- printf "%s-mcp-server" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.ingesterFullname" -}}
{{- printf "%s-ingester" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.resolutionEngineFullname" -}}
{{- printf "%s-resolution-engine" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.workflowCoordinatorFullname" -}}
{{- printf "%s-workflow-coordinator" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.apiMetricsServiceName" -}}
{{- printf "%s-api-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.mcpServerMetricsServiceName" -}}
{{- printf "%s-mcp-server-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.ingesterMetricsServiceName" -}}
{{- printf "%s-ingester-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.resolutionEngineMetricsServiceName" -}}
{{- printf "%s-resolution-engine-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.workflowCoordinatorMetricsServiceName" -}}
{{- printf "%s-workflow-coordinator-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.apiSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: api
{{- end -}}

{{- define "eshu.mcpServerSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: mcp-server
{{- end -}}

{{- define "eshu.ingesterSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: ingester
{{- end -}}

{{- define "eshu.resolutionEngineSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: resolution-engine
{{- end -}}

{{- define "eshu.workflowCoordinatorSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: workflow-coordinator
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
{{- $component := default "api" .component -}}
- name: ESHU_DEPLOYMENT_ENVIRONMENT
  value: {{ .Values.observability.environment | quote }}
- name: OTEL_SERVICE_NAME
  value: {{ printf "eshu-%s" $component | quote }}
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: {{ .Values.observability.otel.endpoint | quote }}
- name: OTEL_EXPORTER_OTLP_PROTOCOL
  value: {{ .Values.observability.otel.protocol | quote }}
- name: OTEL_EXPORTER_OTLP_INSECURE
  value: {{ ternary "true" "false" .Values.observability.otel.insecure | quote }}
- name: OTEL_EXPORTER_OTLP_HEADERS
  value: {{ include "eshu.joinStringMap" .Values.observability.otel.headers | quote }}
- name: OTEL_TRACES_EXPORTER
  value: {{ .Values.observability.otel.tracesExporter | quote }}
- name: OTEL_METRICS_EXPORTER
  value: {{ .Values.observability.otel.metricsExporter | quote }}
- name: OTEL_LOGS_EXPORTER
  value: {{ .Values.observability.otel.logsExporter | quote }}
- name: OTEL_METRIC_EXPORT_INTERVAL
  value: {{ mul (int .Values.observability.otel.metricExportIntervalSeconds) 1000 | quote }}
- name: OTEL_PYTHON_FASTAPI_EXCLUDED_URLS
  value: {{ join "," .Values.observability.otel.excludedUrls | quote }}
- name: OTEL_RESOURCE_ATTRIBUTES
  value: {{ include "eshu.joinStringMap" .Values.observability.otel.resourceAttributes | quote }}
{{- end }}
{{- end -}}

{{- define "eshu.renderPrometheusEnv" -}}
{{- if .Values.observability.prometheus.enabled }}
- name: ESHU_PROMETHEUS_METRICS_ENABLED
  value: "true"
- name: ESHU_PROMETHEUS_METRICS_HOST
  value: {{ .Values.observability.prometheus.host | quote }}
- name: ESHU_PROMETHEUS_METRICS_PORT
  value: {{ .Values.observability.prometheus.port | quote }}
{{- end }}
{{- end -}}

{{- define "eshu.renderContentStoreEnv" -}}
{{- if .Values.contentStore.dsn }}
- name: ESHU_CONTENT_STORE_DSN
  value: {{ .Values.contentStore.dsn | quote }}
- name: ESHU_POSTGRES_DSN
  value: {{ .Values.contentStore.dsn | quote }}
{{- end }}
{{- end -}}

{{- define "eshu.exposureBackendServiceName" -}}
{{- if eq .backend "mcp" -}}
{{- include "eshu.mcpServerFullname" .root -}}
{{- else -}}
{{- include "eshu.fullname" .root -}}
{{- end -}}
{{- end -}}

{{- define "eshu.renderDataPlaneBootstrapInitContainer" -}}
- name: db-migrate
  image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
  imagePullPolicy: {{ .Values.image.pullPolicy }}
  command: ["/usr/local/bin/eshu-bootstrap-data-plane"]
  securityContext:
    {{- toYaml .Values.initContainerSecurityContext | nindent 4 }}
  env:
    - name: ESHU_HOME
      value: /tmp/.eshu
    - name: HOME
      value: /tmp
    - name: NEO4J_URI
      value: {{ .Values.neo4j.uri | quote }}
    {{- include "eshu.renderContentStoreEnv" . | nindent 4 }}
    {{- include "eshu.renderNeo4jAuthEnv" . | nindent 4 }}
    {{- include "eshu.renderEnvMap" .Values.env | nindent 4 }}
  volumeMounts:
    - name: tmp
      mountPath: /tmp
{{- end -}}

{{/* Bolt credentials are always rendered because the shared client config rejects empty auth fields. */}}
{{- define "eshu.renderNeo4jAuthEnv" -}}
{{- if .Values.neo4j.auth.secretName }}
- name: NEO4J_USERNAME
  valueFrom:
    secretKeyRef:
      name: {{ .Values.neo4j.auth.secretName }}
      key: {{ .Values.neo4j.auth.usernameKey }}
- name: NEO4J_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ .Values.neo4j.auth.secretName }}
      key: {{ .Values.neo4j.auth.passwordKey }}
{{- else }}
- name: NEO4J_USERNAME
  value: {{ required "neo4j.auth.username is required when neo4j.auth.secretName is empty" .Values.neo4j.auth.username | quote }}
- name: NEO4J_PASSWORD
  value: {{ required "neo4j.auth.password is required when neo4j.auth.secretName is empty" .Values.neo4j.auth.password | quote }}
{{- end }}
{{- end -}}

{{- define "eshu.nornicdbFullname" -}}
{{- printf "%s-nornicdb" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.nornicdbSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: nornicdb
{{- end -}}

{{- define "eshu.renderConnectionTuningEnv" -}}
{{- with . }}
{{- with .postgres }}
{{- with .maxOpenConns }}
- name: ESHU_POSTGRES_MAX_OPEN_CONNS
  value: {{ . | quote }}
{{- end }}
{{- with .maxIdleConns }}
- name: ESHU_POSTGRES_MAX_IDLE_CONNS
  value: {{ . | quote }}
{{- end }}
{{- with .connMaxLifetime }}
- name: ESHU_POSTGRES_CONN_MAX_LIFETIME
  value: {{ . | quote }}
{{- end }}
{{- with .connMaxIdleTime }}
- name: ESHU_POSTGRES_CONN_MAX_IDLE_TIME
  value: {{ . | quote }}
{{- end }}
{{- with .pingTimeout }}
- name: ESHU_POSTGRES_PING_TIMEOUT
  value: {{ . | quote }}
{{- end }}
{{- end }}
{{- with .neo4j }}
{{- with .maxConnectionPoolSize }}
- name: ESHU_NEO4J_MAX_CONNECTION_POOL_SIZE
  value: {{ . | quote }}
{{- end }}
{{- with .maxConnectionLifetime }}
- name: ESHU_NEO4J_MAX_CONNECTION_LIFETIME
  value: {{ . | quote }}
{{- end }}
{{- with .connectionAcquisitionTimeout }}
- name: ESHU_NEO4J_CONNECTION_ACQUISITION_TIMEOUT
  value: {{ . | quote }}
{{- end }}
{{- with .socketConnectTimeout }}
- name: ESHU_NEO4J_SOCKET_CONNECT_TIMEOUT
  value: {{ . | quote }}
{{- end }}
{{- with .verifyTimeout }}
- name: ESHU_NEO4J_VERIFY_TIMEOUT
  value: {{ . | quote }}
{{- end }}
{{- end }}
{{- end }}
{{- end -}}
