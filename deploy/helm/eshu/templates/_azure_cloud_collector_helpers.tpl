{{- define "eshu.azureCloudCollectorFullname" -}}
{{- printf "%s-azure-cloud-collector" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.azureCloudCollectorMetricsServiceName" -}}
{{- printf "%s-azure-cloud-collector-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.azureCloudCollectorSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: azure-cloud-collector
{{- end -}}

{{- define "eshu.azureCloudCollectorServiceAccountName" -}}
{{- $serviceAccount := default dict .Values.azureCloudCollector.serviceAccount -}}
{{- if $serviceAccount.name -}}
{{- $serviceAccount.name -}}
{{- else if $serviceAccount.create -}}
{{- include "eshu.azureCloudCollectorFullname" . -}}
{{- else -}}
{{- include "eshu.serviceAccountName" . -}}
{{- end -}}
{{- end -}}
