{{- define "eshu.pagerDutyCollectorFullname" -}}
{{- printf "%s-pagerduty-collector" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.pagerDutyCollectorMetricsServiceName" -}}
{{- printf "%s-pagerduty-collector-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.pagerDutyCollectorSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: pagerduty-collector
{{- end -}}
