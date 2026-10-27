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

{{/*
The single activation the dedicated tempo or loki worker renders. It carries the same
keys as a componentExtensionCollector.activations entry, so the shared seed
and reconcile helpers serve it unchanged: the seed init container copies the
collector binary and manifest out of the digest-pinned satellite image, and
the reconcile init container grants, installs, and enables the component.
*/}}
{{- define "eshu.tempoCollectorActivation" -}}
{{- $tempo := .Values.tempoCollector -}}
{{- $activation := dict "instanceId" $tempo.instanceId -}}
{{- $_ := set $activation "componentId" $tempo.componentId -}}
{{- $_ := set $activation "componentVersion" $tempo.componentVersion -}}
{{- $_ := set $activation "mode" (default "scheduled" $tempo.mode) -}}
{{- $_ := set $activation "seed" (default (dict) $tempo.seed) -}}
{{- $_ := set $activation "host" (default (dict) $tempo.host) -}}
{{- $_ := set $activation "process" (default (dict) $tempo.process) -}}
{{- with $tempo.config }}{{ $_ := set $activation "config" . }}{{ end -}}
{{- with $tempo.grants }}{{ $_ := set $activation "grants" . }}{{ end -}}
{{- with $tempo.pollInterval }}{{ $_ := set $activation "pollInterval" . }}{{ end -}}
{{- with $tempo.claimLeaseTTL }}{{ $_ := set $activation "claimLeaseTTL" . }}{{ end -}}
{{- with $tempo.heartbeatInterval }}{{ $_ := set $activation "heartbeatInterval" . }}{{ end -}}
{{- $activation | toJson -}}
{{- end -}}
{{- define "eshu.lokiCollectorActivation" -}}
{{- $loki := .Values.lokiCollector -}}
{{- $activation := dict "instanceId" $loki.instanceId -}}
{{- $_ := set $activation "componentId" $loki.componentId -}}
{{- $_ := set $activation "componentVersion" $loki.componentVersion -}}
{{- $_ := set $activation "mode" (default "scheduled" $loki.mode) -}}
{{- $_ := set $activation "seed" (default (dict) $loki.seed) -}}
{{- $_ := set $activation "host" (default (dict) $loki.host) -}}
{{- $_ := set $activation "process" (default (dict) $loki.process) -}}
{{- with $loki.config }}{{ $_ := set $activation "config" . }}{{ end -}}
{{- with $loki.grants }}{{ $_ := set $activation "grants" . }}{{ end -}}
{{- with $loki.pollInterval }}{{ $_ := set $activation "pollInterval" . }}{{ end -}}
{{- with $loki.claimLeaseTTL }}{{ $_ := set $activation "claimLeaseTTL" . }}{{ end -}}
{{- with $loki.heartbeatInterval }}{{ $_ := set $activation "heartbeatInterval" . }}{{ end -}}
{{- $activation | toJson -}}
{{- end -}}
