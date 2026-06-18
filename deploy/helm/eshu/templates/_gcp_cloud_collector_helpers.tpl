{{- define "eshu.gcpCloudCollectorFullname" -}}
{{- printf "%s-gcp-cloud-collector" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.gcpCloudCollectorMetricsServiceName" -}}
{{- printf "%s-gcp-cloud-collector-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.gcpCloudCollectorSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: gcp-cloud-collector
{{- end -}}

{{- define "eshu.gcpCloudCollectorServiceAccountName" -}}
{{- $serviceAccount := default dict .Values.gcpCloudCollector.serviceAccount -}}
{{- if $serviceAccount.name -}}
{{- $serviceAccount.name -}}
{{- else if $serviceAccount.create -}}
{{- include "eshu.gcpCloudCollectorFullname" . -}}
{{- else -}}
{{- include "eshu.serviceAccountName" . -}}
{{- end -}}
{{- end -}}
