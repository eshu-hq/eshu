{{- define "eshu.networkPolicy.ports" -}}
{{- range . }}
- protocol: {{ default "TCP" .protocol }}
  port: {{ .port }}
{{- end }}
{{- end -}}

{{- define "eshu.networkPolicy.peers" -}}
{{- range . }}
- {{- with .namespaceSelector }}
  namespaceSelector:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .podSelector }}
  podSelector:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .ipBlock }}
  ipBlock:
    {{- toYaml . | nindent 4 }}
  {{- end }}
{{- end }}
{{- end -}}

{{- define "eshu.networkPolicy.peerRule" -}}
{{- $peers := default list .peers -}}
{{- if gt (len $peers) 0 }}
- to:
    {{- include "eshu.networkPolicy.peers" $peers | nindent 4 }}
  ports:
    {{- include "eshu.networkPolicy.ports" .ports | nindent 4 }}
{{- end }}
{{- end -}}

{{- define "eshu.networkPolicy.configuredRule" -}}
{{- $cfg := default dict .cfg -}}
{{- $enabled := true -}}
{{- if hasKey $cfg "enabled" -}}
{{- $enabled = $cfg.enabled -}}
{{- end -}}
{{- if and $enabled (gt (len (default list $cfg.to)) 0) -}}
{{- include "eshu.networkPolicy.peerRule" (dict "peers" $cfg.to "ports" (default .defaultPorts $cfg.ports)) -}}
{{- end -}}
{{- end -}}

{{- define "eshu.networkPolicy.dnsRule" -}}
{{- $root := .root -}}
{{- $egress := default dict $root.Values.networkPolicy.egress -}}
{{- $dns := default dict $egress.dns -}}
{{- $enabled := true -}}
{{- if hasKey $dns "enabled" -}}
{{- $enabled = $dns.enabled -}}
{{- end -}}
{{- if $enabled -}}
{{- $defaultPeers := list (dict "namespaceSelector" (dict "matchLabels" (dict "kubernetes.io/metadata.name" "kube-system")) "podSelector" (dict "matchLabels" (dict "k8s-app" "kube-dns"))) -}}
{{- $defaultPorts := list (dict "protocol" "UDP" "port" 53) (dict "protocol" "TCP" "port" 53) -}}
{{- include "eshu.networkPolicy.peerRule" (dict "peers" (default $defaultPeers $dns.to) "ports" (default $defaultPorts $dns.ports)) -}}
{{- end -}}
{{- end -}}

{{- define "eshu.networkPolicy.graphRule" -}}
{{- $root := .root -}}
{{- $egress := default dict $root.Values.networkPolicy.egress -}}
{{- $graph := default dict $egress.graph -}}
{{- $enabled := true -}}
{{- if hasKey $graph "enabled" -}}
{{- $enabled = $graph.enabled -}}
{{- end -}}
{{- $ports := default (list (dict "protocol" "TCP" "port" 7687)) $graph.ports -}}
{{- if and $enabled (gt (len (default list $graph.to)) 0) -}}
{{- include "eshu.networkPolicy.peerRule" (dict "peers" $graph.to "ports" $ports) -}}
{{- else if and $enabled $root.Values.nornicdb.enabled -}}
{{- $peers := list (dict "podSelector" (dict "matchLabels" (dict "app.kubernetes.io/name" (include "eshu.name" $root) "app.kubernetes.io/instance" $root.Release.Name "app.kubernetes.io/component" "nornicdb"))) -}}
{{- $nornicPorts := list (dict "protocol" "TCP" "port" $root.Values.nornicdb.ports.bolt) -}}
{{- include "eshu.networkPolicy.peerRule" (dict "peers" $peers "ports" (default $nornicPorts $graph.ports)) -}}
{{- end -}}
{{- end -}}

{{- define "eshu.networkPolicy.internalRule" -}}
{{- $root := .root -}}
{{- $egress := default dict $root.Values.networkPolicy.egress -}}
{{- $internal := default dict $egress.internalServices -}}
{{- $enabled := true -}}
{{- if hasKey $internal "enabled" -}}
{{- $enabled = $internal.enabled -}}
{{- end -}}
{{- if $enabled -}}
{{- $defaultPeers := list (dict "podSelector" (dict "matchLabels" (dict "app.kubernetes.io/name" (include "eshu.name" $root) "app.kubernetes.io/instance" $root.Release.Name))) -}}
{{- $defaultPorts := list (dict "protocol" "TCP" "port" 8080) -}}
{{- include "eshu.networkPolicy.peerRule" (dict "peers" (default $defaultPeers $internal.to) "ports" (default $defaultPorts $internal.ports)) -}}
{{- end -}}
{{- end -}}

{{- define "eshu.networkPolicy.restrictedEgress" -}}
{{- $root := .root -}}
{{- $egress := default dict $root.Values.networkPolicy.egress -}}
{{- include "eshu.networkPolicy.dnsRule" (dict "root" $root) }}
{{ include "eshu.networkPolicy.configuredRule" (dict "cfg" $egress.datastores "defaultPorts" (list (dict "protocol" "TCP" "port" 5432))) }}
{{ include "eshu.networkPolicy.graphRule" (dict "root" $root) }}
{{ include "eshu.networkPolicy.internalRule" (dict "root" $root) }}
{{- $classes := default dict $egress.classes -}}
{{- range .classes }}
{{ include "eshu.networkPolicy.configuredRule" (dict "cfg" (index $classes .) "defaultPorts" (list (dict "protocol" "TCP" "port" 443))) }}
{{- end }}
{{- end -}}

{{- define "eshu.networkPolicy.egress" -}}
{{- $egress := default dict .root.Values.networkPolicy.egress -}}
{{- $mode := default "broad" $egress.mode -}}
{{- if not (or (eq $mode "broad") (eq $mode "restricted")) -}}
{{- fail "networkPolicy.egress.mode must be broad or restricted" -}}
{{- end -}}
{{- if eq $mode "broad" }}
- {}
{{- else }}
{{- $rules := include "eshu.networkPolicy.restrictedEgress" . | trim -}}
{{- if $rules }}
{{ $rules }}
{{- else }}
[]
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "eshu.networkPolicy.ingress" -}}
{{- $root := .root -}}
{{- $ports := list -}}
{{- with .port -}}
{{- $ports = append $ports (dict "protocol" "TCP" "port" .) -}}
{{- end -}}
{{- if and .metrics $root.Values.observability.prometheus.enabled -}}
{{- $ports = append $ports (dict "protocol" "TCP" "port" $root.Values.observability.prometheus.port) -}}
{{- end -}}
{{- if gt (len $ports) 0 }}
- ports:
    {{- include "eshu.networkPolicy.ports" $ports | nindent 4 }}
{{- else }}
[]
{{- end -}}
{{- end -}}

{{- define "eshu.networkPolicy.workload" -}}
{{- if .enabled }}
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ .name }}
  labels:
    {{- include "eshu.labels" .root | nindent 4 }}
    app.kubernetes.io/component: {{ .component }}
spec:
  podSelector:
    matchLabels:
      {{- .selector | nindent 6 }}
  policyTypes:
    - Ingress
    - Egress
  ingress:
    {{- include "eshu.networkPolicy.ingress" (dict "root" .root "port" .port "metrics" .metrics) | nindent 4 }}
  egress:
    {{- include "eshu.networkPolicy.egress" (dict "root" .root "classes" .classes) | nindent 4 }}
{{- end }}
{{- end -}}
