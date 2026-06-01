{{- define "eshu.jiraCollectorFullname" -}}
{{- printf "%s-jira-collector" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.jiraCollectorMetricsServiceName" -}}
{{- printf "%s-jira-collector-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.jiraCollectorSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: jira-collector
{{- end -}}
