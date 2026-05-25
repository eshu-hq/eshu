{{- define "eshu.securityAlertCollectorFullname" -}}
{{- printf "%s-security-alert-collector" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.securityAlertCollectorMetricsServiceName" -}}
{{- printf "%s-security-alert-collector-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.securityAlertCollectorSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: security-alert-collector
{{- end -}}
