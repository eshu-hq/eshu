{{/*
Helpers for componentExtensionCollector.activations.

One activation renders one worker Deployment with its own instance ID and its
own component registry (an emptyDir seeded at pod start). The layout inside the
registry volume and the activation config path are shared by the worker and the
workflow coordinator, because ActivationConfigHandle hashes the config path and
both processes must derive the same handle.
*/}}

{{/* "true" when the activations list drives rendering, empty string otherwise. */}}
{{- define "eshu.componentActivationsMode" -}}
{{- if gt (len (default (list) .Values.componentExtensionCollector.activations)) 0 -}}true{{- end -}}
{{- end -}}

{{- define "eshu.componentActivationFullname" -}}
{{- printf "%s-cx-%s" (include "eshu.fullname" .root) .activation.instanceId -}}
{{- end -}}

{{- define "eshu.componentActivationSelectorLabels" -}}
{{- include "eshu.componentExtensionCollectorSelectorLabels" .root }}
eshu.dev/component-instance: {{ .activation.instanceId | quote }}
{{- end -}}

{{- define "eshu.componentActivationConfigMapName" -}}
{{- printf "%s-component-activations" (include "eshu.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Directory in the pod where the activation ConfigMap is mounted. */}}
{{- define "eshu.componentActivationConfigDir" -}}
/etc/eshu/component-activations
{{- end -}}

{{- define "eshu.componentActivationConfigPath" -}}
{{- printf "%s/%s.yaml" (include "eshu.componentActivationConfigDir" .root) .activation.instanceId -}}
{{- end -}}

{{/* Directory under componentHome that the seed init container fills. */}}
{{- define "eshu.componentActivationRuntimeDir" -}}
{{- printf "%s/runtime/%s" (trimSuffix "/" .root.Values.componentExtensionCollector.componentHome) .activation.instanceId -}}
{{- end -}}

{{- define "eshu.componentActivationProcessCommand" -}}
{{- printf "%s/collector" (include "eshu.componentActivationRuntimeDir" .) -}}
{{- end -}}
