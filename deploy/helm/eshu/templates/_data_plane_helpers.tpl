{{- define "eshu.renderContentStoreEnv" -}}
{{- if .Values.contentStore.secretName }}
- name: ESHU_CONTENT_STORE_DSN
  valueFrom:
    secretKeyRef:
      name: {{ .Values.contentStore.secretName }}
      key: {{ .Values.contentStore.dsnKey | default "dsn" }}
- name: ESHU_POSTGRES_DSN
  valueFrom:
    secretKeyRef:
      name: {{ .Values.contentStore.secretName }}
      key: {{ .Values.contentStore.dsnKey | default "dsn" }}
{{- else if .Values.contentStore.dsn }}
- name: ESHU_CONTENT_STORE_DSN
  value: {{ .Values.contentStore.dsn | quote }}
- name: ESHU_POSTGRES_DSN
  value: {{ .Values.contentStore.dsn | quote }}
{{- end }}
{{- end -}}

{{- define "eshu.renderDataPlaneBootstrapEnv" -}}
- name: ESHU_HOME
  value: /tmp/.eshu
- name: HOME
  value: /tmp
- name: NEO4J_URI
  value: {{ .Values.neo4j.uri | quote }}
{{- include "eshu.renderContentStoreEnv" . | nindent 0 }}
{{- include "eshu.renderNeo4jAuthEnv" . | nindent 0 }}
{{- include "eshu.renderEnvMap" .Values.env | nindent 0 }}
{{- end -}}

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
  value: {{ required "neo4j.auth.password is required when neo4j.auth.secretName is empty; set a strong password (min 12 chars, mixed case + digit) or reference a K8s Secret via neo4j.auth.secretName" .Values.neo4j.auth.password | quote }}
{{- end }}
{{- end -}}

{{- define "eshu.nornicdbFullname" -}}
{{- printf "%s-nornicdb" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.nornicdbLegacyClaimName" -}}
{{- printf "%s-data" (include "eshu.nornicdbFullname" .) -}}
{{- end -}}

{{- define "eshu.nornicdbManagedClaimName" -}}
{{- printf "%s-v132-data" (include "eshu.nornicdbFullname" .) -}}
{{- end -}}

{{- define "eshu.nornicdbClaimName" -}}
{{- if .Values.nornicdb.persistence.existingClaim -}}
{{- .Values.nornicdb.persistence.existingClaim -}}
{{- else -}}
{{- include "eshu.nornicdbManagedClaimName" . -}}
{{- end -}}
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
