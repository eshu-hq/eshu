{{- define "eshu.cicdRunCollectorFullname" -}}
{{- printf "%s-cicd-run-collector" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.cicdRunCollectorMetricsServiceName" -}}
{{- printf "%s-cicd-run-collector-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.cicdRunCollectorSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: cicd-run-collector
{{- end -}}
