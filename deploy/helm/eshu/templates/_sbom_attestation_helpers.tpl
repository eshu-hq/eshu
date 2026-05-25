{{- define "eshu.sbomAttestationCollectorFullname" -}}
{{- printf "%s-sbom-attestation-collector" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.sbomAttestationCollectorMetricsServiceName" -}}
{{- printf "%s-sbom-attestation-collector-metrics" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "eshu.sbomAttestationCollectorSelectorLabels" -}}
{{- include "eshu.selectorLabels" . }}
app.kubernetes.io/component: sbom-attestation-collector
{{- end -}}
