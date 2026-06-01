{{- define "eshu.grafanaCollectorFullname" -}}
{{- printf "%s-grafana-collector" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.grafanaCollectorMetricsServiceName" -}}
{{- printf "%s-grafana-collector-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.grafanaCollectorSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: grafana-collector
{{- end -}}

{{- define "eshu.prometheusMimirCollectorFullname" -}}
{{- printf "%s-prometheus-mimir-collector" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.prometheusMimirCollectorMetricsServiceName" -}}
{{- printf "%s-prometheus-mimir-collector-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.prometheusMimirCollectorSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: prometheus-mimir-collector
{{- end -}}

{{- define "eshu.lokiCollectorFullname" -}}
{{- printf "%s-loki-collector" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.lokiCollectorMetricsServiceName" -}}
{{- printf "%s-loki-collector-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.lokiCollectorSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: loki-collector
{{- end -}}

{{- define "eshu.tempoCollectorFullname" -}}
{{- printf "%s-tempo-collector" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.tempoCollectorMetricsServiceName" -}}
{{- printf "%s-tempo-collector-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.tempoCollectorSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: tempo-collector
{{- end -}}
